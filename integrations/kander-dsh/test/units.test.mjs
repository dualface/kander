import assert from 'node:assert/strict'
import { statSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, test } from 'node:test'
import { processName } from '../lib/foreground.js'
import { PaneSequence } from '../lib/ids.js'
import { paneEnvironment } from '../lib/index.js'
import { ensurePrivateDirectory, stateDirectory } from '../lib/paths.js'
import { upgradeSide } from '../lib/protocol.js'
import { temporaryDirectory } from './helpers.mjs'

describe('pane sequence', () => {
  test('IDs never repeat across restarts and between hosts', () => {
    const path = join(temporaryDirectory(), 'sequence')
    const seen = new Set()
    const take = (sequence) => {
      const { pane, container } = sequence.next()
      assert.ok(!seen.has(pane) && !seen.has(container), pane)
      seen.add(pane)
      seen.add(container)
    }
    const first = new PaneSequence(path, 100)
    take(first)
    take(first)
    // A restarted host reusing the same pid continues the persisted number.
    take(new PaneSequence(path, 100))
    // Two hosts that read the same number during a handover differ by pid.
    const a = new PaneSequence(path, 200)
    const b = new PaneSequence(path, 300)
    const number = a.read()
    writeFileSync(path, `${number}\n`)
    take(a)
    writeFileSync(path, `${number}\n`)
    take(b)
    assert.equal(statSync(path).mode & 0o777, 0o600)
  })

  test('a damaged sequence file is refused instead of restarting at 1', () => {
    const path = join(temporaryDirectory(), 'sequence')
    writeFileSync(path, 'garbage')
    assert.throws(() => new PaneSequence(path).next(), /does not hold a pane sequence number/)
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

  test('an existing directory is tightened to 0700', () => {
    const directory = join(temporaryDirectory(), 'kander')
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
