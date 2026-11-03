// The carrier lock: an exclusive file lock that the kernel releases when its
// holder process ends, so no host ever has to judge or reclaim a lock left by
// a crashed one. Node has no flock(2), so a child process holds it: flock(1)
// from util-linux on Linux, lockf(1) of the base system elsewhere (macOS).
// The child prints `held` once it holds the lock and then waits on its stdin.
// Ending stdin releases the lock; when the dsh host dies, its end of the pipe
// closes and the lock goes with it.
import { spawn as nodeSpawn } from 'node:child_process'
import { EventEmitter } from 'node:events'

const HOLD = ['/bin/sh', '-c', 'echo held; exec cat']

/** The argv that waits for the lock on file, then holds it. */
export function lockCommand(file, platform = process.platform) {
  // -o: the waiting command does not inherit the lock, so the lock ends with flock itself.
  if (platform === 'linux') return ['flock', ['-o', file, ...HOLD]]
  // -k: keep the lock file; lockf(1) waits without a timeout by default.
  return ['lockf', ['-k', file, ...HOLD]]
}

/**
 * One attempt to hold the lock. Emits `held` once the lock is held, and
 * `ended` with a reason when the holder process ended, before or after it
 * held the lock.
 */
export class LockHolder extends EventEmitter {
  constructor(file, { spawn = nodeSpawn, platform = process.platform } = {}) {
    super()
    this.file = file
    this.spawn = spawn
    this.platform = platform
    this.held = false
    this.child = undefined
  }

  start() {
    const [command, args] = lockCommand(this.file, this.platform)
    const child = this.spawn(command, args, { stdio: ['pipe', 'pipe', 'pipe'] })
    this.child = child
    let output = ''
    let errors = ''
    child.stdout.setEncoding('utf8')
    child.stdout.on('data', (chunk) => {
      output += chunk
      if (!this.held && output.includes('held\n')) {
        this.held = true
        this.emit('held')
      }
    })
    child.stderr.setEncoding('utf8')
    child.stderr.on('data', (chunk) => { errors = (errors + chunk).slice(-2000) })
    // A write to the stdin of an ended holder is not an error worth a crash.
    child.stdin.on('error', () => {})
    let ended = false
    const end = (reason) => {
      if (ended) return
      ended = true
      this.held = false
      // A waiting command that kept the lock fd (lockf may pass it on) ends with its stdin.
      child.stdin.end()
      this.emit('ended', reason)
    }
    child.on('error', (error) => end(`${command}: ${error?.message ?? error}`))
    child.on('exit', (code, signal) => end(errors.trim() || `${command} ended (${signal ?? `exit ${code}`})`))
    this.ended = new Promise((resolve) => this.once('ended', resolve))
  }

  /** Releases the lock, or stops waiting for it; resolves once the holder ended. */
  async release() {
    if (!this.child) return
    this.child.stdin.end()
    await this.ended
  }
}
