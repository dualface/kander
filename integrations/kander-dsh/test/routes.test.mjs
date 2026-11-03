import assert from 'node:assert/strict'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, test } from 'node:test'
import { BoardError } from '../lib/boards.js'
import { KanderHost } from '../lib/host.js'
import { ROUTE_PREFIX, createRoutes, sseEvent } from '../lib/routes.js'
import { FakeSubprocess, RecordingLog, eventually, temporaryDirectory } from './helpers.mjs'

/** A Boards stand-in that records its calls. */
class FakeBoards {
  constructor() {
    this.calls = []
    this.listResult = { kander: '/bin/kander', snapshot: { schema_version: 1, change_token: 't', tasks: [] } }
  }
  async projectOf(path) { return path.startsWith('/board') ? '/board' : null }
  async list(project) {
    this.calls.push(['list', project])
    if (this.listResult instanceof Error) throw this.listResult
    return this.listResult
  }
  async start(project, task) {
    this.calls.push(['start', project, task])
    return { ok: true, code: 0, stdout: 'started', stderr: '' }
  }
}

describe('view routes', () => {
  let host, boards, subprocess, routes

  beforeEach(async () => {
    subprocess = new FakeSubprocess()
    host = new KanderHost({ subprocess, log: new RecordingLog(), directory: join(temporaryDirectory(), 'kander'), foregroundName: async () => 'kfake' })
    await host.start()
    boards = new FakeBoards()
    routes = createRoutes(host, boards)
  })

  afterEach(() => host.stop())

  const route = (name) => routes.find((item) => item.path === `${ROUTE_PREFIX}/${name}`)
  const get = (name, query = '') => route(name).fetch(new Request(`http://dsh${ROUTE_PREFIX}/${name}${query}`))
  const post = (name, body) => route(name).fetch(new Request(`http://dsh${ROUTE_PREFIX}/${name}`, { method: 'POST', body: typeof body === 'string' ? body : JSON.stringify(body) }))
  const runningPane = async () => {
    const pane = host.mux.create({ cwd: '/work', label: 'card', project: '/board' })
    await host.mux.run(pane, ['/bin/agent'], '/')
    return { pane, terminal: subprocess.terminals.at(-1) }
  }

  test('every route is buffered and has one method', () => {
    for (const item of routes) {
      assert.equal(item.requestBody, 'buffered')
      assert.equal(item.methods.length, 1)
    }
    assert.deepEqual(routes.filter((item) => item.methods[0] === 'POST').map((item) => item.path.slice(ROUTE_PREFIX.length + 1)).sort(), ['input', 'start'])
  })

  test('state reports the carrier, the panes, and the focus request', async () => {
    const { pane } = await runningPane()
    host.recordFocus(pane)
    const body = await (await get('state')).json()
    assert.equal(body.carrier.carrier, true)
    assert.deepEqual(body.panes.map((item) => [item.pane, item.project, item.inputWaiting]), [[pane.id, '/board', true]])
    assert.equal(body.focus.pane, pane.id)
  })

  test('boards resolves each distinct path once', async () => {
    const body = await (await get('boards', '?path=/board/a&path=/plain&path=/board/a')).json()
    assert.deepEqual(body.boards, [{ path: '/board/a', project: '/board' }, { path: '/plain', project: null }])
  })

  test('board returns the snapshot, and errors carry their code', async () => {
    assert.equal((await (await get('board', '?project=/board')).json()).snapshot.change_token, 't')
    assert.deepEqual(boards.calls, [['list', '/board']])
    boards.listResult = new BoardError('kander-not-found', 'kander was not found', { searched: ['/a/kander'] })
    const missing = await get('board', '?project=/board')
    assert.equal(missing.status, 500)
    assert.deepEqual((await missing.json()).error, { code: 'kander-not-found', message: 'kander was not found', searched: ['/a/kander'] })
    boards.listResult = new BoardError('no-board', '/x has no kanban/ directory')
    assert.equal((await get('board', '?project=/x')).status, 404)
  })

  test('start runs on the carrier host', async () => {
    const response = await post('start', { project: '/board', task: '20261103-alpha-task' })
    assert.equal(response.status, 200)
    assert.equal((await response.json()).ok, true)
    assert.deepEqual(boards.calls, [['start', '/board', '20261103-alpha-task']])
    assert.equal((await post('start', 'not json')).status, 400)
  })

  test('a non-carrier host refuses start without running kander', async () => {
    const other = createRoutes({ isCarrier: () => false }, boards)
    const start = other.find((item) => item.path.endsWith('/start'))
    const response = await start.fetch(new Request('http://dsh/x', { method: 'POST', body: JSON.stringify({ project: '/board', task: '20261103-alpha-task' }) }))
    assert.equal(response.status, 409)
    assert.equal((await response.json()).error.code, 'not-carrier')
    assert.deepEqual(boards.calls, [])
  })

  test('input reaches the pane in arrival order and resizes only on a size change', async () => {
    const { pane, terminal } = await runningPane()
    const order = []
    terminal.write = async (data) => {
      // The first write is slow: later requests must still wait for it.
      if (data === 'a') await new Promise((resolve) => setTimeout(resolve, 50))
      order.push(data)
    }
    const sizes = []
    terminal.resize = async (cols, rows) => { sizes.push(`${cols}x${rows}`) }
    const responses = await Promise.all([
      post('input', { pane: pane.id, data: 'a', cols: 100, rows: 30 }),
      post('input', { pane: pane.id, data: 'b', cols: 100, rows: 30 }),
      post('input', { pane: pane.id, data: 'c', cols: 90, rows: 20 }),
      post('input', { pane: pane.id, data: '', cols: 90, rows: 20 }),
    ])
    assert.deepEqual(responses.map((response) => response.status), [200, 200, 200, 200])
    assert.deepEqual(order, ['a', 'b', 'c'])
    assert.deepEqual(sizes, ['100x30', '90x20'])
    assert.deepEqual([pane.cols, pane.rows], [90, 20])
  })

  test('input to an unknown pane is gone; malformed input is refused', async () => {
    assert.equal((await post('input', { pane: 'p9-1', data: 'x' })).status, 404)
    assert.equal((await post('input', { data: 'x' })).status, 400)
    const { pane } = await runningPane()
    assert.equal((await post('input', { pane: pane.id, data: 7 })).status, 400)
  })

  test('the output stream is an event stream: replay, new output, then exit', async () => {
    const { pane, terminal } = await runningPane()
    terminal.output.write('before\n')
    await eventually(() => pane.ring.length === 1)
    const response = await get('stream', `?pane=${pane.id}`)
    assert.equal(response.headers.get('content-type'), 'text/event-stream; charset=utf-8')
    const reader = response.body.getReader()
    const decoder = new TextDecoder()
    let text = ''
    const readUntil = async (marker) => {
      while (!text.includes(marker)) {
        const { value, done } = await reader.read()
        if (done) break
        text += decoder.decode(value, { stream: true })
      }
    }
    await readUntil('before')
    terminal.output.write('line one\nline two')
    await readUntil('line two')
    terminal.exit({ exitCode: 0 })
    await readUntil('event: exit')
    assert.equal(text, sseEvent('output', 'before\n') + sseEvent('output', 'line one\nline two') + sseEvent('exit', {}))
    assert.equal((await reader.read()).done, true)
    assert.equal(pane.listeners.size, 0)
  })

  test('a stream of an exited pane replays and ends; an unknown pane is 404', async () => {
    const { pane, terminal } = await runningPane()
    terminal.output.write('last words')
    terminal.exit({ exitCode: 0 })
    await eventually(() => pane.exited)
    const text = await (await get('stream', `?pane=${pane.id}`)).text()
    assert.equal(text, sseEvent('output', 'last words') + sseEvent('exit', {}))
    assert.equal((await get('stream', '?pane=p9-1')).status, 404)
  })

  test('cancelling a stream unsubscribes from the pane', async () => {
    const { pane } = await runningPane()
    const controller = new AbortController()
    const response = await route('stream').fetch(new Request(`http://dsh/x?pane=${pane.id}`, { signal: controller.signal }))
    assert.equal(pane.listeners.size, 1)
    await response.body.cancel()
    assert.equal(pane.listeners.size, 0)
  })

  test('the vendored xterm files are served with their types', async () => {
    assert.equal((await get('xterm.js')).headers.get('content-type'), 'text/javascript; charset=utf-8')
    assert.equal((await get('xterm.css')).headers.get('content-type'), 'text/css; charset=utf-8')
    const fit = await get('addon-fit.js')
    assert.match(await fit.text(), /FitAddon/)
  })
})
