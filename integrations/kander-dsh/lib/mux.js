// The pane multiplexer: one container holds exactly one pane, and a pane is one
// PTY that the dsh host opens with ctx.subprocess.spawnTerminal. A headless
// screen per pane answers read_output, and a bounded output ring replays the
// recent output to a new subscriber.
import { createRequire } from 'node:module'
import { processName } from './foreground.js'

const require = createRequire(import.meta.url)
const { Terminal: HeadlessTerminal } = require('../vendor/xterm-headless.cjs')

const DEFAULT_COLS = 160
const DEFAULT_ROWS = 48
const RING_LIMIT = 2 << 20

// Variables that make a program believe it runs inside another terminal
// multiplexer. A kander started by the agent would otherwise pick that
// launcher; an empty value reads as unset there.
const FOREIGN_TERMINAL_VARIABLES = ['TMUX', 'TMUX_PANE', 'HERDR_ENV', 'HERDR_PANE_ID', 'LUVUS_ENV', 'LUVUS_PANE_ID', 'HC_TERMINAL_ID']

export class Mux {
  /**
   * @param options.subprocess - the dsh subprocess service (ctx.subprocess).
   * @param options.sequence - a PaneSequence that issues pane and container IDs.
   * @param options.env - variables added to every pane on top of dsh's scrubbed environment.
   * @param options.foregroundName - resolves a process group id to a command name.
   */
  constructor({ subprocess, sequence, env = {}, foregroundName = processName }) {
    this.subprocess = subprocess
    this.sequence = sequence
    this.env = { ...Object.fromEntries(FOREIGN_TERMINAL_VARIABLES.map((name) => [name, ''])), ...env }
    this.foregroundName = foregroundName
    this.panes = new Map()
  }

  create({ cwd, label = '', project = '' }) {
    const { pane: id, container } = this.sequence.next()
    const pane = {
      id,
      container,
      cwd,
      label,
      project,
      marker: '',
      handle: undefined,
      screen: new HeadlessTerminal({ cols: DEFAULT_COLS, rows: DEFAULT_ROWS, scrollback: 2000, allowProposedApi: true }),
      ring: [],
      ringBytes: 0,
      listeners: new Set(),
      writes: Promise.resolve(),
      started: false,
      exited: false,
      cols: DEFAULT_COLS,
      rows: DEFAULT_ROWS,
    }
    this.panes.set(id, pane)
    return pane
  }

  get(id) {
    return this.panes.get(id)
  }

  byContainer(container) {
    for (const pane of this.panes.values()) if (pane.container === container) return pane
    return undefined
  }

  list() {
    return [...this.panes.values()]
  }

  /** Starts argv in the pane; a pane runs one program for its whole life. */
  async run(pane, argv, cwd) {
    if (pane.started) throw new Error(`pane ${pane.id} already runs a program`)
    pane.started = true
    const handle = await this.subprocess.spawnTerminal({
      argv,
      cwd,
      env: this.env,
      cols: pane.cols,
      rows: pane.rows,
      terminalType: 'xterm-256color',
      graceMs: 1000,
    })
    pane.handle = handle
    handle.output.setEncoding?.('utf8')
    handle.output.on('data', (data) => this.output(pane, typeof data === 'string' ? data : data.toString('utf8')))
    const settle = () => {
      if (pane.exited) return
      pane.exited = true
      for (const listener of pane.listeners) listener(null)
    }
    handle.done.then(settle, settle)
  }

  output(pane, text) {
    pane.screen.write(text)
    pane.ring.push(text)
    pane.ringBytes += text.length
    while (pane.ringBytes > RING_LIMIT && pane.ring.length > 1) pane.ringBytes -= pane.ring.shift().length
    for (const listener of pane.listeners) listener(text)
  }

  /**
   * Calls listener with the buffered output, then with each new chunk, and
   * with null once the program exited.
   * @returns a function that ends the subscription.
   */
  subscribe(pane, listener) {
    for (const chunk of pane.ring) listener(chunk)
    if (pane.exited) {
      listener(null)
      return () => {}
    }
    pane.listeners.add(listener)
    return () => pane.listeners.delete(listener)
  }

  /** The visible screen text, like `tmux capture-pane -p`. */
  capture(pane) {
    const buffer = pane.screen.buffer.active
    const lines = []
    for (let row = buffer.viewportY; row < buffer.viewportY + pane.screen.rows; row++) {
      lines.push(buffer.getLine(row)?.translateToString(true) ?? '')
    }
    while (lines.length && lines[lines.length - 1] === '') lines.pop()
    return lines.join('\n')
  }

  /**
   * The foreground process group of the pane: its command name ('' when
   * unknown) and whether it reads terminal input (null when dsh cannot tell).
   */
  async foreground(pane) {
    if (!pane.handle || pane.exited) return { command: '', inputWaiting: null }
    let group
    try {
      group = await pane.handle.inspectForeground()
    } catch {
      return { command: '', inputWaiting: null }
    }
    if (!group) return { command: '', inputWaiting: null }
    return { command: await this.foregroundName(group.processGroupId), inputWaiting: group.inputWaiting === true }
  }

  /** Writes reach the program in call order: kander's text and Enter are two requests. */
  write(pane, data) {
    const current = pane.writes.then(async () => {
      if (!pane.handle || pane.exited) throw new Error(`pane ${pane.id} is not running`)
      await pane.handle.write(data)
    })
    pane.writes = current.catch(() => {})
    return current
  }

  async resize(pane, cols, rows) {
    if (!Number.isInteger(cols) || !Number.isInteger(rows) || cols <= 0 || rows <= 0) {
      throw new Error('cols and rows must be positive integers')
    }
    pane.cols = cols
    pane.rows = rows
    pane.screen.resize(cols, rows)
    if (pane.handle && !pane.exited) await pane.handle.resize(cols, rows)
  }

  /** Ends the program and forgets the pane. */
  async close(pane) {
    this.panes.delete(pane.id)
    if (pane.handle) await pane.handle.terminate().catch(() => {})
    if (!pane.exited) {
      pane.exited = true
      for (const listener of pane.listeners) listener(null)
    }
    pane.listeners.clear()
    pane.screen.dispose()
  }

  async closeAll() {
    await Promise.all(this.list().map((pane) => this.close(pane)))
  }
}
