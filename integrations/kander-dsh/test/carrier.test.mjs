import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { existsSync, lstatSync, mkdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { afterEach, describe, test } from 'node:test'
import { KanderHost } from '../lib/host.js'
import { FakeSubprocess, RecordingLog, call, eventually, temporaryDirectory } from './helpers.mjs'

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
    const child = spawn(process.execPath, ['-e', `require('node:net').createServer().listen(${JSON.stringify(socket)}, () => console.log('up'))`], { stdio: ['ignore', 'pipe', 'inherit'] })
    await new Promise((resolve) => child.stdout.once('data', resolve))
    child.kill('SIGKILL')
    await new Promise((resolve) => child.once('exit', resolve))
    assert.ok(lstatSync(socket).isSocket())

    const { host } = newHost(directory)
    await host.start()
    assert.equal(host.isCarrier(), true)
    assert.equal((await call(socket, 'hello', { protocol: '1' })).body.pid, process.pid)
  })

  test('a carrier whose path was replaced steps down and competes again', async () => {
    const directory = join(temporaryDirectory(), 'kander')
    const first = newHost(directory)
    await first.host.start()
    const second = newHost(directory)
    // Simulate a host that judged the socket stale and renamed its own over it.
    second.host.carrier.claimFree({ code: 'ENOENT' })
    await eventually(() => second.host.isCarrier())
    await eventually(() => !first.host.isCarrier())
    assert.ok(first.log.lines.some(([, line]) => line.includes('no longer names')), first.log.lines)
    await eventually(() => first.host.carrierStatus().reason === 'held')
    // The demoted server closed without removing the new carrier's socket.
    assert.ok(existsSync(join(directory, 'mux.sock')))
    assert.equal((await call(join(directory, 'mux.sock'), 'hello', { protocol: '1' })).status, 200)
  })

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
