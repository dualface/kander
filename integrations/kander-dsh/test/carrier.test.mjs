import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { existsSync, lstatSync, mkdirSync, renameSync, writeFileSync } from 'node:fs'
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
  const newHost = (directory) => {
    const log = new RecordingLog()
    const host = new KanderHost({ subprocess: new FakeSubprocess(), log, directory, intervalMs: 20 })
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

    const { host } = newHost(directory)
    await host.start()
    assert.equal(host.isCarrier(), true)
    assert.equal((await call(socket, 'hello', { protocol: '1' })).body.pid, process.pid)
  })

  test('a carrier whose path was replaced ends its panes and competes again', async () => {
    const directory = join(temporaryDirectory(), 'kander')
    const socket = join(directory, 'mux.sock')
    const first = newHost(directory)
    await first.host.start()
    const { pane } = (await call(socket, 'create_container', { cwd: '/w' })).body
    await call(socket, 'run_command', { pane, cwd_quoted: "'/w'", command: 'agent', posix: 'true' })
    const [terminal] = first.host.mux.subprocess.terminals
    // Another host's live socket lands on the path, as when two hosts raced
    // over a stale socket and the restore of removeStale found the path taken.
    const other = createServer((request, response) => response.end('{"pid":999}'))
    const otherName = join(directory, '.other')
    await new Promise((resolve) => other.listen(otherName, resolve))
    renameSync(otherName, socket)
    try {
      await first.host.carrier.tick()
      assert.equal(first.host.isCarrier(), false)
      assert.deepEqual(first.host.carrierStatus(), { carrier: false, socket, reason: 'held', holderPid: 999 })
      assert.equal(terminal.terminated, true)
      assert.deepEqual(await first.host.listPanes(), [])
      assert.ok(first.log.lines.some(([, line]) => line.includes('no longer names')), first.log.lines)
      // The demoted host left the other host's socket in place.
      assert.equal((await call(socket, 'hello', { protocol: '1' })).body.pid, 999)
    } finally {
      await new Promise((resolve) => other.close(resolve))
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
