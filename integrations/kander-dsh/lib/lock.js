// A lock shared by the dsh hosts of one DSH_HOME. It serializes every step
// that judges or changes the shared state: the stale-socket verdict, its
// removal, the socket publication and release, and taking a pane number.
//
// The lock is a directory created with mkdir, which fails while it exists;
// the owner file inside names the holder. Holders keep it for at most a probe
// timeout, so a lock whose holder process is gone, or that is older than
// staleMs, is left over from a crash and may be broken. Breaking renames the
// directory aside and removes it only when it still names the holder judged
// stale; a lock taken in between is put back.
import { randomBytes } from 'node:crypto'
import { mkdirSync, readFileSync, renameSync, rmSync, rmdirSync, statSync, unlinkSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

const alive = (pid) => {
  try {
    process.kill(pid, 0)
    return true
  } catch (error) {
    // EPERM: the process exists but belongs to another user.
    return error?.code === 'EPERM'
  }
}

export class DirectoryLock {
  /**
   * @param path - the lock directory.
   * @param options.staleMs - age after which a held lock counts as left over.
   * @param options.timeoutMs - how long acquire waits for a live holder.
   */
  constructor(path, { staleMs = 5000, timeoutMs = 4000, retryMs = 10 } = {}) {
    this.path = path
    this.staleMs = staleMs
    this.timeoutMs = timeoutMs
    this.retryMs = retryMs
  }

  /** Runs fn while holding the lock and releases it afterwards. */
  async run(fn) {
    const token = await this.acquire()
    try {
      return await fn()
    } finally {
      this.release(token)
    }
  }

  async acquire() {
    const token = `${process.pid} ${randomBytes(8).toString('hex')}`
    const deadline = Date.now() + this.timeoutMs
    for (;;) {
      try {
        mkdirSync(this.path, { mode: 0o700 })
      } catch (error) {
        if (error?.code !== 'EEXIST') throw error
        if (this.breakIfStale()) continue
        if (Date.now() > deadline) throw new Error(`${this.path} stayed locked by another dsh host`)
        await sleep(this.retryMs)
        continue
      }
      try {
        writeFileSync(join(this.path, 'owner'), token, { mode: 0o600 })
      } catch (error) {
        rmSync(this.path, { recursive: true, force: true })
        throw error
      }
      return token
    }
  }

  release(token) {
    const owner = join(this.path, 'owner')
    let current
    try {
      current = readFileSync(owner, 'utf8')
    } catch {
      return
    }
    // A lock broken as stale and taken by another host is not ours to remove.
    if (current !== token) return
    unlinkSync(owner)
    rmdirSync(this.path)
  }

  // Returns true when the lock was broken or vanished, so acquire retries at once.
  breakIfStale() {
    const judged = this.holder()
    if (judged === undefined) return true
    if (!judged.stale) return false
    const aside = `${this.path}.stale-${process.pid}-${randomBytes(4).toString('hex')}`
    try {
      renameSync(this.path, aside)
    } catch (error) {
      if (error?.code === 'ENOENT') return true
      throw error
    }
    let moved = null
    try { moved = readFileSync(join(aside, 'owner'), 'utf8') } catch {}
    if (moved !== judged.token) {
      // A live lock was taken in between: put it back unless the path is taken again.
      try {
        renameSync(aside, this.path)
        return false
      } catch {}
    }
    rmSync(aside, { recursive: true, force: true })
    return true
  }

  /** {token, stale} of the current holder, or undefined when the lock is free. */
  holder() {
    let token = null
    let changed
    try {
      token = readFileSync(join(this.path, 'owner'), 'utf8')
      changed = statSync(join(this.path, 'owner')).mtimeMs
    } catch (error) {
      if (error?.code !== 'ENOENT') throw error
      // A holder between mkdir and writing its owner file, or one that crashed there.
      try {
        changed = statSync(this.path).mtimeMs
      } catch (statError) {
        if (statError?.code === 'ENOENT') return undefined
        throw statError
      }
    }
    const pid = token === null ? NaN : Number(token.split(' ')[0])
    const stale = Date.now() - changed > this.staleMs || (Number.isInteger(pid) && pid > 0 && !alive(pid))
    return { token, stale }
  }
}
