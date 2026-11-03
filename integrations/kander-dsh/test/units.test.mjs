import assert from 'node:assert/strict'
import { execFile } from 'node:child_process'
import { chmodSync, mkdirSync, readFileSync, statSync, utimesSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, test } from 'node:test'
import { processName } from '../lib/foreground.js'
import { PaneSequence } from '../lib/ids.js'
import { paneEnvironment } from '../lib/index.js'
import { DirectoryLock } from '../lib/lock.js'
import { ensurePrivateDirectory, stateDirectory } from '../lib/paths.js'
import { upgradeSide } from '../lib/protocol.js'
import { temporaryDirectory } from './helpers.mjs'

describe('pane sequence', () => {
  const sequenceIn = (directory, pid) => new PaneSequence(directory, new DirectoryLock(join(directory, 'lock')), pid)

  test('IDs continue across restarts', async () => {
    const directory = temporaryDirectory()
    assert.equal((await sequenceIn(directory, 100).next()).pane, 'p1-100')
    assert.deepEqual(await sequenceIn(directory, 100).next(), { pane: 'p2-100', container: 'c2-100' })
    assert.equal(statSync(join(directory, 'sequence')).mode & 0o777, 0o600)
  })

  test('two hosts taking numbers together never share one or move the record back', async () => {
    const directory = temporaryDirectory()
    const a = sequenceIn(directory, 100)
    const b = sequenceIn(directory, 100)
    // More than the 64 numbers an earlier design reclaimed, interleaved.
    const taken = await Promise.all(Array.from({ length: 150 }, (_, index) => (index % 2 ? a : b).next()))
    assert.equal(new Set(taken.map((ids) => ids.pane)).size, 150)
    assert.equal(a.read(), 150)
    assert.equal((await b.next()).pane, 'p151-100')
  })

  test('processes taking numbers at the same time never share one', async () => {
    const directory = temporaryDirectory()
    const lib = (name) => JSON.stringify(new URL(`../lib/${name}`, import.meta.url).href)
    const script = `import { PaneSequence } from ${lib('ids.js')}
      import { DirectoryLock } from ${lib('lock.js')}
      const sequence = new PaneSequence(process.argv[1], new DirectoryLock(process.argv[1] + '/lock'), 7)
      const out = []
      for (let i = 0; i < 40; i++) out.push((await sequence.next()).pane)
      console.log(JSON.stringify(out))`
    const runs = await Promise.all(Array.from({ length: 4 }, () => new Promise((resolve, reject) => {
      execFile(process.execPath, ['--input-type=module', '-e', script, directory], (error, stdout) => (error ? reject(error) : resolve(JSON.parse(stdout))))
    })))
    const panes = runs.flat()
    // All four children use the pid 7, so only the number keeps them apart.
    assert.equal(new Set(panes).size, 160)
    assert.equal(sequenceIn(directory, 7).read(), 160)
  })

  test('a damaged sequence record is refused instead of restarting at 1', async () => {
    const directory = temporaryDirectory()
    writeFileSync(join(directory, 'sequence'), 'garbage')
    await assert.rejects(sequenceIn(directory).next(), /does not hold a pane sequence number/)
  })
})

describe('directory lock', () => {
  test('a second holder waits for the first', async () => {
    const lock = new DirectoryLock(join(temporaryDirectory(), 'lock'))
    const order = []
    let release
    const first = lock.run(() => new Promise((resolve) => { order.push('first'); release = resolve }))
    await new Promise((resolve) => setTimeout(resolve, 20))
    const second = lock.run(() => { order.push('second') })
    await new Promise((resolve) => setTimeout(resolve, 50))
    assert.deepEqual(order, ['first'])
    release()
    await Promise.all([first, second])
    assert.deepEqual(order, ['first', 'second'])
  })

  test('a lock left by a dead process or held too long is broken', async () => {
    const path = join(temporaryDirectory(), 'lock')
    mkdirSync(path)
    // pid 2^22 + 1 is above the Linux and macOS pid limits, so it is not running.
    writeFileSync(join(path, 'owner'), '4194305 dead')
    assert.equal(await new DirectoryLock(path, { timeoutMs: 200 }).run(() => 'ran'), 'ran')
    mkdirSync(path)
    writeFileSync(join(path, 'owner'), `${process.pid} old`)
    const old = new Date(Date.now() - 60_000)
    utimesSync(join(path, 'owner'), old, old)
    assert.equal(await new DirectoryLock(path, { timeoutMs: 200 }).run(() => 'ran'), 'ran')
  })

  test('a live lock is not broken, and a holder never removes a lock it lost', async () => {
    const path = join(temporaryDirectory(), 'lock')
    mkdirSync(path)
    writeFileSync(join(path, 'owner'), `${process.pid} live`)
    await assert.rejects(new DirectoryLock(path, { timeoutMs: 100 }).run(() => {}), /stayed locked/)
    const lock = new DirectoryLock(path)
    lock.release('someone else')
    assert.equal(readFileSync(join(path, 'owner'), 'utf8'), `${process.pid} live`)
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
