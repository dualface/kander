// Shared fakes: a dsh subprocess service whose terminals are in-memory, a
// recording logger, and a form POST over the unix socket like kander's curl.
import { mkdtempSync, realpathSync, rmSync } from 'node:fs'
import { request } from 'node:http'
import { join } from 'node:path'
import { PassThrough } from 'node:stream'

export class FakeTerminal {
  constructor(spec) {
    this.spec = spec
    this.output = new PassThrough()
    this.writes = []
    this.size = [spec.cols, spec.rows]
    this.foregroundGroup = { processGroupId: 4242, inputWaiting: true }
    this.terminated = false
    this.done = new Promise((resolve) => { this.exit = resolve })
  }

  async write(data) { this.writes.push(data) }
  async resize(cols, rows) { this.size = [cols, rows] }
  async inspectForeground() { return this.foregroundGroup }
  async terminate() {
    this.terminated = true
    this.exit({ exitCode: 0 })
  }
}

export class FakeSubprocess {
  constructor() { this.terminals = [] }
  async spawnTerminal(spec) {
    const terminal = new FakeTerminal(spec)
    this.terminals.push(terminal)
    return terminal
  }
}

export class RecordingLog {
  constructor() { this.lines = [] }
  info(message) { this.lines.push(['info', message]) }
  warn(message) { this.lines.push(['warn', message]) }
}

/**
 * A fresh private directory, symlinks resolved. It lives under /tmp, not the
 * per-user temp directory of macOS, because a unix socket path is limited to
 * about 100 bytes.
 */
export function temporaryDirectory() {
  const directory = realpathSync(mkdtempSync(join('/tmp', 'kander-dsh-')))
  created.push(directory)
  return directory
}

const created = []
process.on('exit', () => {
  for (const directory of created) rmSync(directory, { recursive: true, force: true })
})

/** POSTs a form to /op/<op> on the socket; resolves {status, body}. */
export function call(socketPath, op, fields = {}) {
  const body = new URLSearchParams(fields).toString()
  return new Promise((resolve, reject) => {
    const outgoing = request({
      socketPath,
      path: `/op/${op}`,
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded', 'Content-Length': Buffer.byteLength(body) },
    }, (incoming) => {
      let text = ''
      incoming.setEncoding('utf8')
      incoming.on('data', (chunk) => { text += chunk })
      incoming.on('end', () => resolve({ status: incoming.statusCode, body: JSON.parse(text) }))
    })
    outgoing.on('error', reject)
    outgoing.end(body)
  })
}

/** Resolves once condition() holds, polling every 10ms for at most timeoutMs. */
export async function eventually(condition, timeoutMs = 3000) {
  const deadline = Date.now() + timeoutMs
  while (!(await condition())) {
    if (Date.now() > deadline) throw new Error('condition did not hold in time')
    await new Promise((resolve) => setTimeout(resolve, 10))
  }
}
