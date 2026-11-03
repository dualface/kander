import assert from 'node:assert/strict'
import { execFile } from 'node:child_process'
import { chmodSync, mkdirSync, statSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, test } from 'node:test'
import { processName } from '../lib/foreground.js'
import { PaneSequence } from '../lib/ids.js'
import { paneEnvironment } from '../lib/index.js'
import { ensurePrivateDirectory, stateDirectory } from '../lib/paths.js'
import { upgradeSide } from '../lib/protocol.js'
import { temporaryDirectory } from './helpers.mjs'

describe('pane sequence', () => {
  const take = (seen, sequence) => {
    const { pane, container } = sequence.next()
    assert.ok(!seen.has(pane) && !seen.has(container), pane)
    seen.add(pane)
    seen.add(container)
    return pane
  }

  test('IDs never repeat across restarts, between hosts, or after a regressed record', () => {
    const directory = temporaryDirectory()
    const seen = new Set()
    const first = new PaneSequence(directory, 100)
    assert.equal(take(seen, first), 'p1-100')
    take(seen, first)
    // A restarted host reusing the same pid continues the persisted number.
    assert.equal(take(seen, new PaneSequence(directory, 100)), 'p3-100')
    // A record written late by a slower host points back; taken numbers are skipped.
    writeFileSync(join(directory, 'sequence'), '1\n')
    assert.equal(take(seen, new PaneSequence(directory, 100)), 'p4-100')
    assert.equal(statSync(join(directory, 'sequence')).mode & 0o777, 0o600)
  })

  test('processes taking numbers at the same time never share one', async () => {
    const directory = temporaryDirectory()
    const script = `import { PaneSequence } from ${JSON.stringify(new URL('../lib/ids.js', import.meta.url).href)}
      const sequence = new PaneSequence(process.argv[1], 7)
      const out = []
      for (let i = 0; i < 40; i++) out.push(sequence.next().pane)
      console.log(JSON.stringify(out))`
    const runs = await Promise.all(Array.from({ length: 4 }, () => new Promise((resolve, reject) => {
      execFile(process.execPath, ['--input-type=module', '-e', script, directory], (error, stdout) => (error ? reject(error) : resolve(JSON.parse(stdout))))
    })))
    const panes = runs.flat()
    assert.equal(panes.length, 160)
    // All four children use the pid 7, so only the number keeps them apart.
    assert.equal(new Set(panes).size, 160)
  })

  test('a damaged sequence record is refused instead of restarting at 1', () => {
    const directory = temporaryDirectory()
    writeFileSync(join(directory, 'sequence'), 'garbage')
    assert.throws(() => new PaneSequence(directory).next(), /does not hold a pane sequence number/)
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
