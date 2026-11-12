import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { chmodSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, test } from 'node:test'
import { BoardError, Boards } from '../lib/boards.js'
import { temporaryDirectory } from './helpers.mjs'

const git = (cwd, ...args) => execFileSync('git', ['-C', cwd, ...args], { stdio: 'ignore' })

/** A Git repository with a kanban/ directory and a second worktree. */
function repository(withLocalBoard = true) {
  const root = temporaryDirectory()
  const main = join(root, 'main')
  mkdirSync(main, { recursive: true })
  if (withLocalBoard) mkdirSync(join(main, 'kanban'))
  git(root, 'init', '-q', '-b', 'main', main)
  git(main, '-c', 'user.name=t', '-c', 'user.email=t@t', 'commit', '-q', '--allow-empty', '-m', 'init')
  const worktree = join(root, 'wt')
  git(main, 'worktree', 'add', '-q', worktree, '-b', 'feature')
  mkdirSync(join(worktree, 'sub'))
  return { root, main, worktree }
}

/**
 * A fake kander: records its directory, argv, and KANBAN_DIR in <dir>/calls and answers by
 * FAKE_MODE (list, fail, garbage, slow).
 */
function fakeKander(directory, name = 'kander') {
  const path = join(directory, name)
  writeFileSync(path, `#!/bin/sh
printf '%s|%s|%s\\n' "$PWD" "$*" "$KANBAN_DIR" >> "${directory}/calls"
case "$FAKE_MODE" in
  fail) echo "kander: boom" >&2; exit 1 ;;
  garbage) echo "not json" ;;
  slow) sleep 0.3; echo started ;;
  *) echo '{"schema_version":1,"change_token":"t","tasks":[]}' ;;
esac
`)
  chmodSync(path, 0o755)
  return path
}

describe('board resolution', () => {
  test('a remote-bound clone and worktree need no local kanban directory', async () => {
    const { main, worktree } = repository(false)
    const boards = new Boards()
    assert.equal(await boards.projectOf(worktree), main)
    assert.equal(await boards.validProject(main), main)
  })
  test('a worktree and a subdirectory resolve to the main worktree', async () => {
    const { main, worktree } = repository()
    const boards = new Boards()
    assert.equal(await boards.projectOf(worktree), main)
    assert.equal(await boards.projectOf(join(worktree, 'sub')), main)
    assert.equal(await boards.projectOf(main), main)
  })

  test('outside Git the nearest ancestor with project config wins', async () => {
    const root = temporaryDirectory()
    writeFileSync(join(root, '.kander-config.json'), '{}')
    mkdirSync(join(root, 'a', 'b'), { recursive: true })
    assert.equal(await new Boards().projectOf(join(root, 'a', 'b')), root)
  })

  test('no board, a relative path, or a missing directory resolve to null', async () => {
    const root = temporaryDirectory()
    const boards = new Boards()
    assert.equal(await boards.projectOf(root), null)
    assert.equal(await boards.projectOf('relative/path'), null)
    assert.equal(await boards.projectOf(join(root, 'missing')), null)
    assert.equal(await boards.projectOf(undefined), null)
  })
})

describe('kander lookup', () => {
  test('the configured absolute path comes first', async () => {
    const root = temporaryDirectory()
    const configured = fakeKander(root, 'configured')
    mkdirSync(join(root, '.kander', 'bin'), { recursive: true })
    fakeKander(join(root, '.kander', 'bin'))
    assert.equal(await new Boards({ kander: configured, env: { PATH: root } }).executable(root), configured)
  })

  test('a configured path that is not executable is an error, not a fallback', async () => {
    const root = temporaryDirectory()
    fakeKander(root)
    await assert.rejects(new Boards({ kander: join(root, 'missing'), env: { PATH: root } }).executable(root),
      (error) => error instanceof BoardError && error.code === 'kander-not-found' && error.details.searched[0] === join(root, 'missing'))
    await assert.rejects(new Boards({ kander: 'kander', env: { PATH: root } }).executable(root), { code: 'kander-not-found' })
  })

  test('the project install comes before PATH', async () => {
    const root = temporaryDirectory()
    const onPath = join(root, 'path')
    mkdirSync(onPath)
    fakeKander(onPath)
    const project = join(root, 'project')
    mkdirSync(join(project, '.kander', 'bin'), { recursive: true })
    const installed = fakeKander(join(project, '.kander', 'bin'))
    const boards = new Boards({ env: { PATH: onPath } })
    assert.equal(await boards.executable(project), installed)
    assert.equal(await boards.executable(root), join(onPath, 'kander'))
  })

  test('without any kander the error lists every place searched', async () => {
    const root = temporaryDirectory()
    const error = await new Boards({ env: { PATH: `relative:${root}` } }).executable(root).catch((failure) => failure)
    assert.equal(error.code, 'kander-not-found')
    assert.deepEqual(error.details.searched, [join(root, '.kander', 'bin', 'kander'), join(root, 'kander')])
  })
})

describe('kander commands', () => {
  const setup = (mode, env = {}) => {
    const root = temporaryDirectory()
    mkdirSync(join(root, 'kanban'))
    fakeKander(root)
    const calls = () => readFileSync(join(root, 'calls'), 'utf8').trim().split('\n')
    // The fake needs sleep from the system directories.
    return { root, calls, boards: new Boards({ env: { PATH: `${root}:/usr/bin:/bin`, FAKE_MODE: mode, ...env } }) }
  }

  test('list runs kander list --json --agent-status in the project', async () => {
    const { root, calls, boards } = setup('list')
    const { kander, snapshot } = await boards.list(root)
    assert.equal(kander, join(root, 'kander'))
    assert.equal(snapshot.change_token, 't')
    assert.deepEqual(calls(), [`${root}|list --json --agent-status|`])
  })

  test('a KANBAN_DIR of the dsh host never redirects list or start to another board', async () => {
    const other = join(temporaryDirectory(), 'kanban')
    const { root, calls, boards } = setup('list', { KANBAN_DIR: other })
    await boards.list(root)
    await boards.start(root, '20261103-alpha-task')
    assert.deepEqual(calls(), [`${root}|list --json --agent-status|`, `${root}|start --launcher dsh 20261103-alpha-task|`])
    assert.equal(boards.env.KANBAN_DIR, other)
  })

  test('a failed or unreadable list is an error with the kander output', async () => {
    const failed = setup('fail')
    await assert.rejects(failed.boards.list(failed.root), (error) => error.code === 'kander-failed' && error.details.stderr.includes('kander: boom'))
    const garbage = setup('garbage')
    await assert.rejects(garbage.boards.list(garbage.root), { code: 'bad-output' })
  })

  test('missing and relative project directories are refused before kander runs', async () => {
    const root = join(temporaryDirectory(), 'missing')
    await assert.rejects(new Boards({ env: { PATH: root } }).list(root), { code: 'no-board' })
    await assert.rejects(new Boards().list('relative'), { code: 'no-board' })
  })

  test('start runs kander start --launcher dsh with a validated task ID', async () => {
    const { root, calls, boards } = setup('list')
    const result = await boards.start(root, '20261103-alpha-task')
    assert.equal(result.ok, true)
    assert.deepEqual(calls(), [`${root}|start --launcher dsh 20261103-alpha-task|`])
    for (const task of ['--help', '20261103-a--b', 'x; rm -rf /', '', 7]) {
      await assert.rejects(boards.start(root, task), { code: 'bad-request' })
    }
  })

  test('a failed start is a result with the kander output', async () => {
    const { root, boards } = setup('fail')
    const result = await boards.start(root, '20261103-alpha-task')
    assert.equal(result.ok, false)
    assert.equal(result.code, 1)
    assert.match(result.stderr, /kander: boom/)
  })

  test('kander runs asynchronously: the event loop keeps running', async () => {
    // kander start --launcher dsh calls back into this host's socket.
    const { root, boards } = setup('slow')
    let ticks = 0
    const timer = setInterval(() => { ticks++ }, 20)
    const result = await boards.start(root, '20261103-alpha-task')
    clearInterval(timer)
    assert.equal(result.ok, true)
    assert.ok(ticks >= 5, `only ${ticks} ticks while kander ran`)
  })
})
