import assert from 'node:assert/strict'
import { execFile, spawn } from 'node:child_process'
import { chmodSync, mkdirSync, readFileSync, statSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, test } from 'node:test'
import { processName } from '../lib/foreground.js'
import { PaneSequence } from '../lib/ids.js'
import { paneEnvironment } from '../lib/index.js'
import { CarrierLock } from '../lib/lock.js'
import { ensurePrivateDirectory, stateDirectory } from '../lib/paths.js'
import { upgradeSide } from '../lib/protocol.js'
import { temporaryDirectory } from './helpers.mjs'

describe('pane sequence', () => {
  test('IDs continue across restarts and never go below the last one a host took', () => {
    const directory = temporaryDirectory()
    const first = new PaneSequence(directory, 100)
    assert.equal(first.next().pane, 'p1-100')
    assert.deepEqual(new PaneSequence(directory, 100).next(), { pane: 'p2-100', container: 'c2-100' })
    assert.equal(statSync(join(directory, 'sequence')).mode & 0o777, 0o600)
    // A record moved back from outside does not make this host repeat a number.
    writeFileSync(join(directory, 'sequence'), '1\n')
    assert.equal(first.next().pane, 'p2-100')
    const again = new PaneSequence(directory, 100)
    again.next()
    again.next()
    writeFileSync(join(directory, 'sequence'), '0\n')
    assert.equal(again.next().pane, 'p5-100')
  })

  test('a damaged sequence record is refused instead of restarting at 1', () => {
    const directory = temporaryDirectory()
    writeFileSync(join(directory, 'sequence'), 'garbage')
    assert.throws(() => new PaneSequence(directory).next(), /does not hold a pane sequence number/)
  })
})

describe('carrier lock', () => {
  test('one holder at a time, and a release lets the next one in', async () => {
    const file = join(temporaryDirectory(), 'carrier.lock')
    const a = new CarrierLock(file)
    const b = new CarrierLock(file)
    assert.equal(await a.tryAcquire(), true)
    assert.equal(await b.tryAcquire(), false)
    assert.equal(await a.tryAcquire(), true)
    a.release()
    assert.equal(await b.tryAcquire(), true)
    assert.equal(await a.tryAcquire(), false)
    b.release()
    assert.equal(statSync(file).mode & 0o777, 0o600)
  })

  // Each process takes the lock ten times and writes enter and exit lines
  // around a short pause; the lines never interleave between processes.
  test('processes never hold the lock at the same time', async () => {
    const directory = temporaryDirectory()
    const file = join(directory, 'carrier.lock')
    const record = join(directory, 'record')
    const script = `import { CarrierLock } from ${JSON.stringify(new URL('../lib/lock.js', import.meta.url).href)}
      import { appendFileSync } from 'node:fs'
      const lock = new CarrierLock(process.argv[1])
      for (let i = 0; i < 10; i++) {
        while (!(await lock.tryAcquire())) await new Promise((resolve) => setTimeout(resolve, 2))
        appendFileSync(process.argv[2], 'enter ' + process.pid + '\\n')
        await new Promise((resolve) => setTimeout(resolve, 5))
        appendFileSync(process.argv[2], 'exit ' + process.pid + '\\n')
        lock.release()
      }`
    await Promise.all(Array.from({ length: 3 }, () => new Promise((resolve, reject) => {
      execFile(process.execPath, ['--input-type=module', '-e', script, file, record], (error) => (error ? reject(error) : resolve()))
    })))
    const lines = readFileSync(record, 'utf8').trim().split('\n')
    assert.equal(lines.length, 60)
    for (let i = 0; i < lines.length; i += 2) {
      const [enter, pid] = lines[i].split(' ')
      assert.equal(enter, 'enter')
      assert.equal(lines[i + 1], `exit ${pid}`)
    }
  })

  test('the lock stays with a stopped holder process and ends when it is killed', async () => {
    const file = join(temporaryDirectory(), 'carrier.lock')
    const script = `import { CarrierLock } from ${JSON.stringify(new URL('../lib/lock.js', import.meta.url).href)}
      const lock = new CarrierLock(process.argv[1])
      console.log(await lock.tryAcquire() ? 'held' : 'busy')
      setInterval(() => {}, 1000)`
    const child = spawn(process.execPath, ['--input-type=module', '-e', script, file], { stdio: ['ignore', 'pipe', 'inherit'] })
    try {
      assert.equal(String(await new Promise((resolve) => child.stdout.once('data', resolve))).trim(), 'held')
      const lock = new CarrierLock(file)
      child.kill('SIGSTOP')
      assert.equal(await lock.tryAcquire(), false)
      child.kill('SIGKILL')
      await new Promise((resolve) => child.once('exit', resolve))
      assert.equal(await lock.tryAcquire(), true)
      lock.release()
    } finally {
      child.kill('SIGKILL')
    }
  })

  test('a missing flock fails the attempt with the reason', { skip: process.platform !== 'linux' }, async () => {
    const lock = new CarrierLock(join(temporaryDirectory(), 'carrier.lock'), {
      spawn: (command, args, options) => spawn('/nonexistent/flock', args, options),
    })
    await assert.rejects(lock.tryAcquire(), /flock: .*ENOENT/)
    assert.equal(lock.held, false)
  })
})

describe('foreground process name', () => {
  test('Linux reads /proc/<pid>/comm', async () => {
    const readFile = async (path) => {
      assert.equal(path, '/proc/77/comm')
      return 'claude\n'
    }
    assert.equal(await processName(77, { platform: 'linux', readFile }), 'claude')
  })

  test('macOS reads ps output and keeps the basename', async () => {
    const execFile = (file, args, options, callback) => {
      assert.equal(file, 'ps')
      assert.deepEqual(args, ['-o', 'comm=', '-p', '77'])
      callback(null, '/opt/homebrew/bin/codex\n')
    }
    assert.equal(await processName(77, { platform: 'darwin', execFile }), 'codex')
  })

  test('an unreadable process has no name', async () => {
    const execFile = (file, args, options, callback) => callback(Object.assign(new Error('exit 1'), { code: 1 }), '')
    assert.equal(await processName(77, { platform: 'darwin', execFile }), '')
    assert.equal(await processName(77, { platform: 'linux', readFile: async () => { throw new Error('ENOENT') } }), '')
    assert.equal(await processName(0, { platform: 'linux' }), '')
  })

  test('this Linux process is found in the real /proc', { skip: process.platform !== 'linux' }, async () => {
    assert.notEqual(await processName(process.pid), '')
  })
})

describe('paths and environment', () => {
  test('the directory follows DSH_HOME, else ~/.dsh', () => {
    assert.equal(stateDirectory({ DSH_HOME: '/custom' }, '/home/u'), '/custom/kander')
    assert.equal(stateDirectory({ DSH_HOME: '' }, '/home/u'), '/home/u/.dsh/kander')
    assert.equal(stateDirectory({}, '/home/u'), '/home/u/.dsh/kander')
  })

  test('a new directory is created 0700', () => {
    const directory = join(temporaryDirectory(), 'kander')
    ensurePrivateDirectory(directory)
    assert.equal(statSync(directory).mode & 0o777, 0o700)
  })

  test('an existing directory is tightened to 0700', () => {
    const directory = join(temporaryDirectory(), 'kander')
    mkdirSync(directory)
    chmodSync(directory, 0o755)
    assert.equal(statSync(directory).mode & 0o777, 0o755)
    ensurePrivateDirectory(directory)
    assert.equal(statSync(directory).mode & 0o777, 0o700)
  })

  test('panes get DSH_HOME and the configured variables', () => {
    assert.deepEqual(paneEnvironment({ env: ['API_TOKEN', 'MISSING'] }, { DSH_HOME: '/d', API_TOKEN: 't', OTHER: 'o' }), { DSH_HOME: '/d', API_TOKEN: 't' })
    assert.deepEqual(paneEnvironment({}, {}), {})
  })

  test('the upgrade side follows the older protocol', () => {
    assert.equal(upgradeSide(1), '')
    assert.equal(upgradeSide(0), 'kander')
    assert.equal(upgradeSide(2), 'plugin')
  })
})
