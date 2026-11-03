import assert from 'node:assert/strict'
import { statSync } from 'node:fs'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, test } from 'node:test'
import { KanderHost } from '../lib/host.js'
import { Gone, PROTOCOL_VERSION } from '../lib/protocol.js'
import { FakeSubprocess, RecordingLog, call, eventually, temporaryDirectory } from './helpers.mjs'

describe('socket operations', () => {
  let directory, socket, subprocess, host

  beforeEach(async () => {
    directory = join(temporaryDirectory(), 'kander')
    socket = join(directory, 'mux.sock')
    subprocess = new FakeSubprocess()
    host = new KanderHost({ subprocess, log: new RecordingLog(), directory, foregroundName: async (pid) => (pid === 4242 ? 'claude' : '') })
    await host.start()
  })

  afterEach(() => host.stop())

  const createPane = async () => {
    const created = await call(socket, 'create_container', { cwd: '/work', label: 'card', project: '/work' })
    assert.equal(created.status, 200)
    return created.body
  }

  test('hello reports the protocol and the side to upgrade', async () => {
    const same = await call(socket, 'hello', { protocol: String(PROTOCOL_VERSION) })
    assert.deepEqual(same, { status: 200, body: { protocol: String(PROTOCOL_VERSION), upgrade: '', pid: process.pid } })
    assert.equal((await call(socket, 'hello', { protocol: String(PROTOCOL_VERSION - 1) })).body.upgrade, 'kander')
    assert.equal((await call(socket, 'hello', { protocol: String(PROTOCOL_VERSION + 1) })).body.upgrade, 'plugin')
    assert.equal((await call(socket, 'hello', {})).status, 400)
  })

  test('a pane runs its command in the startup directory and reports facts', async () => {
    const { container, pane } = await createPane()
    assert.match(pane, /^p1-\d+$/)
    assert.match(container, /^c1-\d+$/)
    assert.equal((await call(socket, 'run_command', { pane, cwd_quoted: "'/work'", command: '/bin/agent task', posix: 'true' })).status, 200)
    const [terminal] = subprocess.terminals
    assert.deepEqual(terminal.spec.argv, ['/bin/sh', '-c', "cd -- '/work' && exec /bin/agent task"])
    assert.equal(terminal.spec.cwd, '/')
    assert.equal(terminal.spec.env.TMUX, '')

    assert.equal((await call(socket, 'set_session_marker', { pane, value: 'session-1' })).status, 200)
    assert.deepEqual((await call(socket, 'pane_facts', { pane })).body, { command: 'claude', in_mode: '0', dead: '0', session_marker: 'session-1' })
    assert.deepEqual((await call(socket, 'topology', { pane })).body, { container, pane_count: '1' })
    assert.deepEqual((await call(socket, 'container_exists', { container })).body, { container })
    assert.deepEqual((await call(socket, 'list')).body, { panes: [{ pane, container, command: 'claude', dead: '0', marker: 'session-1' }] })

    terminal.output.write('first line\r\nready> ')
    await eventually(async () => (await call(socket, 'read_output', { pane })).body.text === 'first line\nready> ')

    await call(socket, 'deliver_text', { pane, text: 'hello {world}' })
    await call(socket, 'enter', { pane })
    assert.deepEqual(terminal.writes, ['hello {world}', '\r'])

    terminal.exit({ exitCode: 0 })
    await eventually(async () => (await call(socket, 'pane_facts', { pane })).body.dead === '1')
    assert.equal((await call(socket, 'deliver_text', { pane, text: 'late' })).status, 409)
  })

  test('a non-POSIX command is refused', async () => {
    const { pane } = await createPane()
    const answer = await call(socket, 'run_command', { pane, cwd_quoted: '', command: 'agent', posix: 'false' })
    assert.equal(answer.status, 400)
    assert.equal(subprocess.terminals.length, 0)
  })

  test('a closed or unknown pane and container are gone', async () => {
    const { container, pane } = await createPane()
    await call(socket, 'run_command', { pane, cwd_quoted: "'/work'", command: 'agent', posix: 'true' })
    assert.equal((await call(socket, 'close_container', { container })).status, 200)
    assert.equal(subprocess.terminals[0].terminated, true)
    for (const [op, fields] of [['pane_facts', { pane }], ['read_output', { pane }], ['topology', { pane }], ['focus', { pane }], ['container_exists', { container }], ['pane_facts', { pane: 'p9-1' }]]) {
      assert.deepEqual(await call(socket, op, fields), { status: 404, body: { error: 'gone' } }, op)
    }
    assert.equal((await call(socket, 'close_container', { container })).status, 200)
    assert.deepEqual(await call(socket, 'nope'), { status: 404, body: { error: 'unknown op' } })
  })

  test('a pane whose program does not end stays and the close fails', async () => {
    const { container, pane } = await createPane()
    await call(socket, 'run_command', { pane, cwd_quoted: "'/work'", command: 'agent', posix: 'true' })
    const [terminal] = subprocess.terminals
    const terminate = terminal.terminate.bind(terminal)
    terminal.terminate = async () => { throw new Error('session members survived') }
    const failed = await call(socket, 'close_container', { container })
    assert.deepEqual(failed, { status: 409, body: { error: 'session members survived' } })
    assert.equal((await call(socket, 'pane_facts', { pane })).body.dead, '0')
    assert.deepEqual((await call(socket, 'container_exists', { container })).body, { container })
    terminal.terminate = terminate
    assert.equal((await call(socket, 'close_container', { container })).status, 200)
    assert.deepEqual(await call(socket, 'pane_facts', { pane }), { status: 404, body: { error: 'gone' } })
  })

  test('the screen answers terminal queries through the pane input', async () => {
    const { pane } = await createPane()
    await call(socket, 'run_command', { pane, cwd_quoted: "'/work'", command: 'agent', posix: 'true' })
    const [terminal] = subprocess.terminals
    terminal.output.write('ab\x1b[6n')
    await eventually(() => terminal.writes.includes('\x1b[1;3R'))
    // Replies and kander's input share one ordered queue.
    await call(socket, 'deliver_text', { pane, text: 'next' })
    assert.deepEqual(terminal.writes, ['\x1b[1;3R', 'next'])
  })

  test('focus records the latest request for the page', async () => {
    const { container, pane } = await createPane()
    assert.equal(host.lastFocus(), null)
    const before = Date.now()
    assert.equal((await call(socket, 'focus', { pane })).status, 200)
    const focus = host.lastFocus()
    assert.deepEqual({ ...focus, at: 0 }, { pane, container, cwd: '/work', at: 0 })
    assert.ok(focus.at >= before && focus.at <= Date.now())
  })

  test('the JavaScript interface lists, streams, writes, and resizes panes', async () => {
    const { container, pane } = await createPane()
    await call(socket, 'run_command', { pane, cwd_quoted: "'/work'", command: 'agent', posix: 'true' })
    const [terminal] = subprocess.terminals
    terminal.output.write('early ')
    await eventually(() => host.mux.get(pane).ring.length === 1)

    const chunks = []
    const unsubscribe = host.subscribe(pane, (chunk) => chunks.push(chunk))
    terminal.output.write('later')
    await eventually(() => chunks.length === 2)
    assert.deepEqual(chunks, ['early ', 'later'])

    await host.write(pane, 'typed')
    await host.resize(pane, 100, 30)
    assert.deepEqual(terminal.writes, ['typed'])
    assert.deepEqual(terminal.size, [100, 30])
    await assert.rejects(host.resize(pane, 0, 30))

    assert.deepEqual(await host.listPanes(), [{ pane, container, label: 'card', project: '/work', cwd: '/work', exited: false, command: 'claude', inputWaiting: true }])
    terminal.foregroundGroup = { processGroupId: 4242, inputWaiting: false }
    assert.equal((await host.listPanes())[0].inputWaiting, false)
    terminal.foregroundGroup = undefined
    assert.deepEqual(await host.listPanes(), [{ pane, container, label: 'card', project: '/work', cwd: '/work', exited: false, command: '', inputWaiting: null }])
    terminal.inspectForeground = async () => { throw new Error('no terminal driver') }
    assert.equal((await host.listPanes())[0].inputWaiting, null)

    terminal.exit({ exitCode: 1 })
    await eventually(() => chunks.at(-1) === null)
    unsubscribe()
    assert.throws(() => host.write('p404-1', 'x'), Gone)
  })

  test('the directory is 0700 and the socket 0600', () => {
    assert.equal(statSync(directory).mode & 0o777, 0o700)
    assert.equal(statSync(socket).mode & 0o777, 0o600)
    assert.equal(statSync(join(directory, 'carrier.lock')).mode & 0o777, 0o600)
    assert.ok(statSync(socket).isSocket())
  })
})
