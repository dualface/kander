// The boards behind the Kander view: which main worktree a dsh workspace
// belongs to, which kander executable serves it, and the two kander commands
// the view runs. Every command runs asynchronously: `kander start --launcher
// dsh` calls back into this host's socket, so a synchronous child process
// would deadlock the host.
import { execFile as nodeExecFile } from 'node:child_process'
import { constants } from 'node:fs'
import { access, lstat, stat } from 'node:fs/promises'
import { delimiter, dirname, isAbsolute, join, resolve } from 'node:path'

const GIT_TIMEOUT_MS = 5000
const LIST_TIMEOUT_MS = 30_000
const START_TIMEOUT_MS = 180_000
const OUTPUT_LIMIT = 8 << 20

/** A task ID: YYYYMMDD-slug, lowercase letters, digits and single hyphens. */
export const TASK_ID = /^[0-9]{8}-[a-z0-9]+(?:-[a-z0-9]+)*$/

/** A failure the view shows with its code; kander output stays verbatim. */
export class BoardError extends Error {
  constructor(code, message, details = {}) {
    super(message)
    this.code = code
    this.details = details
  }
}

const isDirectory = async (path) => {
  try {
    return (await lstat(path)).isDirectory()
  } catch {
    return false
  }
}

const isRegularFile = async (path) => {
  try { return (await lstat(path)).isFile() } catch { return false }
}

const isExecutable = async (path) => {
  try {
    if (!(await stat(path)).isFile()) return false
    await access(path, constants.X_OK)
    return true
  } catch {
    return false
  }
}

/**
 * Runs a command without a shell; resolves {code, stdout, stderr}. A missing
 * executable or a timeout resolves with code null and the error in stderr.
 */
export function run(file, args, { cwd, timeout, env = process.env, execFile = nodeExecFile }) {
  return new Promise((resolvePromise) => {
    execFile(file, args, { cwd, timeout, env, maxBuffer: OUTPUT_LIMIT, encoding: 'utf8' }, (error, stdout, stderr) => {
      if (!error) return resolvePromise({ code: 0, stdout, stderr })
      const code = typeof error.code === 'number' ? error.code : null
      const reason = code === null ? `${error.killed ? 'timed out' : error.message}\n` : ''
      resolvePromise({ code, stdout: stdout ?? '', stderr: `${stderr ?? ''}${reason}` })
    })
  })
}

export class Boards {
  /**
   * @param options.kander - the configured absolute kander path, or ''.
   * @param options.env - the environment of the kander and git processes.
   * @param options.execFile - node's execFile, replaced in tests.
   */
  constructor({ kander = '', env = process.env, execFile = nodeExecFile } = {}) {
    this.kander = kander
    this.env = env
    this.execFile = execFile
  }

  /**
   * The local repository bound to a server project. Git worktrees resolve to
   * their main worktree; a non-Git project uses its nearest config directory.
   * No local kanban directory is required.
   */
  async projectOf(path) {
    if (typeof path !== 'string' || !isAbsolute(path) || !(await isDirectory(path))) return null
    const main = await this.mainWorktree(path)
    if (main && (await isDirectory(main))) return main
    for (let current = resolve(path); ; current = dirname(current)) {
      if (await isRegularFile(join(current, '.kander-config.json')) || await isRegularFile(join(current, '.kander', 'config.json'))) return current
      if (dirname(current) === current) return null
    }
  }

  async mainWorktree(path) {
    const { code, stdout } = await run('git', ['-C', path, 'worktree', 'list', '--porcelain'], { timeout: GIT_TIMEOUT_MS, env: this.env, execFile: this.execFile })
    if (code !== 0) return ''
    const line = stdout.split('\n').find((entry) => entry.startsWith('worktree '))
    return line ? line.slice('worktree '.length) : ''
  }

  /**
   * The kander of a project: the configured absolute path, then the project
   * install <project>/.kander/bin/kander, then PATH.
   * @throws BoardError kander-not-found with the places it looked.
   */
  async executable(project) {
    const searched = []
    if (this.kander) {
      searched.push(this.kander)
      if (isAbsolute(this.kander) && (await isExecutable(this.kander))) return this.kander
      throw new BoardError('kander-not-found', `the configured kander ${this.kander} is not an executable absolute path`, { searched })
    }
    const projectInstall = join(project, '.kander', 'bin', 'kander')
    searched.push(projectInstall)
    if (await isExecutable(projectInstall)) return projectInstall
    for (const directory of (this.env.PATH ?? '').split(delimiter)) {
      if (!isAbsolute(directory)) continue
      const candidate = join(directory, 'kander')
      searched.push(candidate)
      if (await isExecutable(candidate)) return candidate
    }
    throw new BoardError('kander-not-found', 'kander was not found', { searched })
  }

  /** Host-wide v1 board overrides must never select a different project. */
  envOf() {
    const env = { ...this.env }
    delete env.KANBAN_DIR
    return env
  }

  async validProject(project) {
    if (typeof project !== 'string' || !isAbsolute(project) || !(await isDirectory(project))) {
      throw new BoardError('no-board', `${project} is not a project directory`)
    }
    return resolve(project)
  }

  /**
   * The board snapshot of `kander list --json --agent-status`.
   * @returns {kander, snapshot}.
   * @throws BoardError kander-not-found, kander-failed or bad-output.
   */
  async list(project) {
    const directory = await this.validProject(project)
    const kander = await this.executable(directory)
    const result = await run(kander, ['list', '--json', '--agent-status'], { cwd: directory, timeout: LIST_TIMEOUT_MS, env: this.envOf(directory), execFile: this.execFile })
    if (result.code !== 0) {
      throw new BoardError('kander-failed', result.stderr.trim() || `kander list exited with ${result.code}`, { kander, stderr: result.stderr, stdout: result.stdout })
    }
    let snapshot
    try {
      snapshot = JSON.parse(result.stdout)
    } catch (error) {
      throw new BoardError('bad-output', `kander list printed no JSON: ${error.message}`, { kander })
    }
    if (snapshot?.schema_version !== 1 || !Array.isArray(snapshot.tasks)) {
      throw new BoardError('bad-output', `unsupported board snapshot schema ${snapshot?.schema_version}`, { kander })
    }
    return { kander, snapshot }
  }

  /**
   * Runs `kander start --launcher dsh <task>` in the project.
   * @returns {ok, code, stdout, stderr}; a failed start is a result, not a throw.
   */
  async start(project, task) {
    if (typeof task !== 'string' || !TASK_ID.test(task)) throw new BoardError('bad-request', `invalid task ID ${task}`)
    const directory = await this.validProject(project)
    const kander = await this.executable(directory)
    const result = await run(kander, ['start', '--launcher', 'dsh', task], { cwd: directory, timeout: START_TIMEOUT_MS, env: this.envOf(directory), execFile: this.execFile })
    return { ok: result.code === 0, ...result }
  }
}
