import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { existsSync, linkSync, lstatSync, mkdirSync, renameSync, writeFileSync } from 'node:fs'
import { createServer } from 'node:http'
import { join } from 'node:path'
import { afterEach, describe, test } from 'node:test'
import { KanderHost } from '../lib/host.js'
import { FakeSubprocess, RecordingLog, call, eventually, temporaryDirectory } from './helpers.mjs'

// A socket file nobody serves, as a dsh host killed with SIGKILL leaves it.
async function leaveStaleSocket(path) {
  const child = spawn(process.execPath, ['-e', `require('node:net').createServer().listen(${JSON.stringify(path)}, () => console.log('up'))`], { stdio: ['ignore', 'pipe', 'inherit'] })
  await new Promise((resolve) => child.stdout.once('data', resolve))
  child.kill('SIGKILL')
  await new Promise((resolve) => child.once('exit', resolve))
}

describe('carrier host', () => {
  const hosts = []
  const newHost = (directory, options = {}) => {
    const log = new RecordingLog()
    const host = new KanderHost({ subprocess: new FakeSubprocess(), log, directory, intervalMs: 20, ...options })
    host.log = log
    hosts.push(host)
    return { host, log }
  }

  afterEach(async () => {
    await Promise.all(hosts.splice(0).map((host) => host.stop()))
  })

  test('the second host of one directory does not take the socket and logs why', async () => {
    const directory = join(temporaryDirectory(), 'kander')
    const first = newHost(directory)
    const second = newHost(directory)
    await first.host.start()
    await second.host.start()
    assert.equal(first.host.isCarrier(), true)
    assert.equal(second.host.isCarrier(), false)
    assert.deepEqual(second.host.carrierStatus(), { carrier: false, socket: join(directory, 'mux.sock'), reason: 'held', holderPid: process.pid })
    assert.ok(second.log.lines.some(([level, line]) => level === 'warn' && line.includes('held by another dsh host')), second.log.lines)
    assert.deepEqual(await second.host.listPanes(), [])
  })

  test('another host takes the socket over once the carrier exits', async () => {
    const directory = join(temporaryDirectory(), 'kander')
    const first = newHost(directory)
    const second = newHost(directory)
    await first.host.start()
    await second.host.start()
    const socket = join(directory, 'mux.sock')
    const old = (await call(socket, 'create_container', { cwd: '/w' })).body
    await first.host.stop()
    await eventually(() => second.host.isCarrier())
    const created = (await call(socket, 'create_container', { cwd: '/w' })).body
    assert.notEqual(created.pane, old.pane)
    assert.notEqual(created.container, old.container)
    assert.deepEqual(await call(socket, 'pane_facts', { pane: old.pane }), { status: 404, body: { error: 'gone' } })
  })

  test('a stale socket left by a killed host is replaced', async () => {
    const directory = join(temporaryDirectory(), 'kander')
    mkdirSync(directory, { mode: 0o700 })
    const socket = join(directory, 'mux.sock')
    await leaveStaleSocket(socket)
    assert.ok(lstatSync(socket).isSocket())

    // The killed host's temporary name stays behind too.
    linkSync(socket, join(directory, '.mux-4194305-0'))
    const { host } = newHost(directory)
    await host.start()
    assert.equal(host.isCarrier(), true)
    assert.equal((await call(socket, 'hello', { protocol: '1' })).body.pid, process.pid)
    assert.equal(existsSync(join(directory, '.mux-4194305-0')), false)
  })

  test('a path changed from outside is linked again and the carrier keeps its panes', async () => {
    const directory = join(temporaryDirectory(), 'kander')
    const socket = join(directory, 'mux.sock')
    const { host, log } = newHost(directory)
    await host.start()
    const { pane } = (await call(socket, 'create_container', { cwd: '/w' })).body
    const other = createServer((request, response) => response.end('{"pid":999}'))
    const otherName = join(directory, '.other')
    await new Promise((resolve) => other.listen(otherName, resolve))
    renameSync(otherName, socket)
    try {
      await host.carrier.tick()
      assert.equal(host.isCarrier(), true)
      assert.equal((await call(socket, 'hello', { protocol: '1' })).body.pid, process.pid)
      assert.ok(host.mux.get(pane))
      assert.ok(log.lines.some(([, line]) => line.includes('changed from outside')), log.lines)
    } finally {
      await new Promise((resolve) => other.close(resolve))
    }
  })

  test('a paused carrier process keeps the carrier until it dies, and its socket stays untouched', async () => {
    const directory = join(temporaryDirectory(), 'kander')
    const socket = join(directory, 'mux.sock')
    const host = new URL('../lib/host.js', import.meta.url).href
    const script = `import { KanderHost } from ${JSON.stringify(host)}
      const log = { info: () => {}, warn: () => {} }
      const subprocess = { spawnTerminal: async () => { throw new Error('no terminals here') } }
      const kander = new KanderHost({ subprocess, log, directory: process.argv[1], intervalMs: 20 })
      await kander.start()
      console.log(kander.isCarrier() ? 'carrier' : 'not carrier')
      setInterval(() => {}, 1000)`
    const child = spawn(process.execPath, ['--input-type=module', '-e', script, directory], { stdio: ['ignore', 'pipe', 'inherit'] })
    try {
      assert.equal(String(await new Promise((resolve) => child.stdout.once('data', resolve))).trim(), 'carrier')
      const waiting = newHost(directory).host
      await waiting.start()
      assert.equal(waiting.carrierStatus().holderPid, child.pid)
      const published = lstatSync(socket).ino
      // A stopped process still holds the lock: nobody takes over while it could resume.
      child.kill('SIGSTOP')
      for (let round = 0; round < 10; round++) await waiting.carrier.tick()
      assert.equal(waiting.isCarrier(), false)
      assert.equal(lstatSync(socket).ino, published)
      child.kill('SIGCONT')
      await new Promise((resolve) => setTimeout(resolve, 100))
      assert.equal(lstatSync(socket).ino, published)
      assert.equal((await call(socket, 'hello', { protocol: '1' })).body.pid, child.pid)
      child.kill('SIGKILL')
      await new Promise((resolve) => child.once('exit', resolve))
      await eventually(async () => { await waiting.carrier.tick(); return waiting.isCarrier() })
      assert.equal((await call(socket, 'hello', { protocol: '1' })).body.pid, process.pid)
    } finally {
      child.kill('SIGKILL')
    }
  })

  test('a host that is not the carrier stops at once while the carrier keeps serving', async () => {
    const directory = join(temporaryDirectory(), 'kander')
    const socket = join(directory, 'mux.sock')
    const first = newHost(directory).host
    const second = newHost(directory).host
    await first.start()
    await second.start()
    const stopped = await Promise.race([second.stop().then(() => 'stopped'), new Promise((resolve) => setTimeout(() => resolve('hung'), 1000))])
    assert.equal(stopped, 'stopped')
    assert.equal(second.carrierStatus().reason, 'stopped')
    assert.equal(first.isCarrier(), true)
    assert.equal((await call(socket, 'hello', { protocol: '1' })).status, 200)
  })

  test('a stopping carrier ends its panes before another host can take over', async () => {
    const directory = join(temporaryDirectory(), 'kander')
    const socket = join(directory, 'mux.sock')
    const first = newHost(directory).host
    const second = newHost(directory).host
    await first.start()
    await second.start()
    const { pane } = (await call(socket, 'create_container', { cwd: '/w' })).body
    await call(socket, 'run_command', { pane, cwd_quoted: "'/w'", command: 'agent', posix: 'true' })
    const [terminal] = first.mux.subprocess.terminals
    let ended
    const slow = new Promise((resolve) => { ended = resolve })
    terminal.terminate = async () => { await slow; terminal.terminated = true }
    const stopping = first.stop()
    for (let round = 0; round < 5; round++) await second.carrier.tick()
    assert.equal(second.isCarrier(), false)
    ended()
    await stopping
    await eventually(async () => { await second.carrier.tick(); return second.isCarrier() })
    assert.equal(terminal.terminated, true)
  })

  test('one host at a time holds the carrier, through repeated handovers', async () => {
    const directory = join(temporaryDirectory(), 'kander')
    const socket = join(directory, 'mux.sock')
    const contenders = Array.from({ length: 4 }, () => newHost(directory).host)
    await Promise.all(contenders.map((host) => host.start()))
    for (let round = 0; round < 3; round++) {
      const running = contenders.filter((host) => host.carrierStatus().reason !== 'stopped')
      await eventually(async () => {
        await Promise.all(running.map((host) => host.carrier.tick()))
        return running.filter((host) => host.isCarrier()).length === 1
      })
      const carrier = running.find((host) => host.isCarrier())
      assert.equal(running.filter((host) => host.carrier.lock.held).length, 1)
      const { pane } = (await call(socket, 'create_container', { cwd: '/w' })).body
      for (let tick = 0; tick < 3; tick++) await Promise.all(running.map((host) => host.carrier.tick()))
      assert.equal(carrier.isCarrier(), true)
      assert.ok(carrier.mux.get(pane))
      await carrier.stop()
    }
  })

  for (const stale of [false, true]) {
    test(`hosts that start together on a ${stale ? 'stale' : 'missing'} socket elect one carrier`, async () => {
      const directory = join(temporaryDirectory(), 'kander')
      mkdirSync(directory, { mode: 0o700 })
      if (stale) await leaveStaleSocket(join(directory, 'mux.sock'))
      const contenders = Array.from({ length: 6 }, () => newHost(directory).host)
      await Promise.all(contenders.map((host) => host.start()))
      for (let round = 0; round < 5; round++) await Promise.all(contenders.map((host) => host.carrier.tick()))
      const carriers = contenders.filter((host) => host.isCarrier())
      assert.equal(carriers.length, 1)
      assert.ok(carriers[0].carrier.ownsPath())
      for (const host of contenders) if (host !== carriers[0]) assert.equal(host.carrierStatus().reason, 'held')
      // Panes created through the socket meanwhile all land on the carrier.
      const created = await Promise.all(Array.from({ length: 8 }, () => call(join(directory, 'mux.sock'), 'create_container', { cwd: '/w' })))
      assert.equal(new Set(created.map((answer) => answer.body.pane)).size, 8)
      assert.equal(carriers[0].mux.list().length, 8)
    })
  }

  test('a path that is not a socket is never replaced', async () => {
    const directory = join(temporaryDirectory(), 'kander')
    mkdirSync(directory, { mode: 0o700 })
    writeFileSync(join(directory, 'mux.sock'), 'not a socket')
    const { host, log } = newHost(directory)
    await host.start()
    assert.equal(host.isCarrier(), false)
    assert.equal(host.carrierStatus().reason, 'blocked')
    assert.ok(log.lines.some(([level]) => level === 'warn'))
  })

  test('stopping the carrier removes its socket', async () => {
    const directory = join(temporaryDirectory(), 'kander')
    const { host } = newHost(directory)
    await host.start()
    await host.stop()
    assert.equal(existsSync(join(directory, 'mux.sock')), false)
    assert.equal(host.carrierStatus().reason, 'stopped')
  })
})
