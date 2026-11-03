// The carrier lock: an exclusive flock(2)-style lock on carrier.lock, held on a
// file description that the dsh host process itself keeps open. The lock
// therefore lives exactly as long as that process holds the descriptor: it
// ends when the host releases it or dies, never earlier, and no host ever
// judges whether a lock was left over.
//
// Node has no flock(2). On Linux the host opens the file and lets flock(1)
// lock that very descriptor (`flock -n -x 3`, the descriptor passed as fd 3);
// flock exits at once and the lock stays with the shared file description.
// macOS opens the file with O_EXLOCK | O_NONBLOCK, which takes the same lock in
// open(2). Both attempts return at once, so no helper process waits for the
// lock and nothing outlives the host.
import { spawn as nodeSpawn } from 'node:child_process'
import { closeSync, constants, openSync } from 'node:fs'

// <fcntl.h> of macOS and the BSDs; Node does not export them.
const O_EXLOCK = 0x20
const BSD_O_NONBLOCK = 0x4

export class CarrierLock {
  /**
   * @param file - the lock file; created 0600 when missing.
   * @param options.platform - selects flock(1) (linux) or O_EXLOCK (others).
   * @param options.spawn - child_process.spawn, for tests.
   */
  constructor(file, { platform = process.platform, spawn = nodeSpawn } = {}) {
    this.file = file
    this.platform = platform
    this.spawn = spawn
    this.fd = undefined
  }

  get held() {
    return this.fd !== undefined
  }

  /** One attempt that returns at once; resolves true when this process now holds the lock. */
  async tryAcquire() {
    if (this.held) return true
    if (this.platform !== 'linux') return this.tryExclusiveOpen()
    const fd = openSync(this.file, constants.O_RDWR | constants.O_CREAT, 0o600)
    let locked = false
    try {
      locked = await this.flockDescriptor(fd)
    } finally {
      if (locked) this.fd = fd
      else closeSync(fd)
    }
    return locked
  }

  tryExclusiveOpen() {
    try {
      this.fd = openSync(this.file, constants.O_RDWR | constants.O_CREAT | O_EXLOCK | BSD_O_NONBLOCK, 0o600)
      return true
    } catch (error) {
      if (error?.code === 'EAGAIN' || error?.code === 'EWOULDBLOCK') return false
      throw error
    }
  }

  // flock -n exits 0 when it locked the descriptor, 1 when another holds it.
  flockDescriptor(fd) {
    return new Promise((resolve, reject) => {
      const child = this.spawn('flock', ['-n', '-x', '3'], { stdio: ['ignore', 'ignore', 'pipe', fd] })
      let errors = ''
      child.stderr.setEncoding('utf8')
      child.stderr.on('data', (chunk) => { errors = (errors + chunk).slice(-2000) })
      child.on('error', (error) => reject(new Error(`flock: ${error?.message ?? error}`)))
      child.on('exit', (code, signal) => {
        if (code === 0) resolve(true)
        else if (code === 1) resolve(false)
        else reject(new Error(errors.trim() || `flock ended (${signal ?? `exit ${code}`})`))
      })
    })
  }

  /** Releases the lock; the next attempt of any host can take it. */
  release() {
    if (!this.held) return
    const fd = this.fd
    this.fd = undefined
    closeSync(fd)
  }
}
