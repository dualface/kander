// Socket ownership between the dsh hosts of one DSH_HOME. The carrier host is
// the host that holds the carrier lock (lock.js) on a file description it
// keeps open itself; only the carrier serves the socket path and hosts
// dsh-launcher panes. The lock ends only when the carrier releases it or its
// process dies, so a host that still runs never stops being the carrier
// behind its own back. Every other host tries the lock once per interval and
// becomes the carrier as soon as it is free, without a restart.
//
// Holding the lock makes the carrier the only host that touches the socket
// path, so whatever socket it finds there is left over and is replaced. Its
// server binds a private temporary name first (0600 before anything can reach
// it) and links that name onto the path; the name stays while the host is the
// carrier, so a path changed from outside can be linked again. Closing the
// server unlinks only that name. The name is short and per process and
// instance: a unix socket path is limited to about 100 bytes.
import { chmodSync, linkSync, lstatSync, readdirSync, unlinkSync } from 'node:fs'
import { request } from 'node:http'
import { dirname, join } from 'node:path'
import { CarrierLock } from './lock.js'
import { alive } from './processes.js'
import { PROTOCOL_VERSION } from './protocol.js'

const PROBE_TIMEOUT_MS = 1000
let instances = 0

/** Asks the server at path for its hello; resolves {pid} or rejects with the connect error. */
export function probeSocket(path, timeoutMs = PROBE_TIMEOUT_MS) {
  return new Promise((resolve, reject) => {
    const body = `protocol=${PROTOCOL_VERSION}`
    const outgoing = request({
      socketPath: path,
      path: '/op/hello',
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded', 'Content-Length': Buffer.byteLength(body) },
      timeout: timeoutMs,
    }, (incoming) => {
      let text = ''
      incoming.setEncoding('utf8')
      incoming.on('data', (chunk) => { text += chunk })
      incoming.on('end', () => {
        let pid = null
        try { pid = JSON.parse(text).pid ?? null } catch {}
        resolve({ pid })
      })
    })
    outgoing.on('timeout', () => outgoing.destroy(Object.assign(new Error('socket did not answer'), { code: 'ETIMEDOUT' })))
    outgoing.on('error', reject)
    outgoing.end(body)
  })
}

/** The path names something other than a socket; it is never replaced. */
class Blocked extends Error {}

export class Carrier {
  /**
   * @param options.path - the socket path.
   * @param options.lockFile - the carrier lock file in the same directory.
   * @param options.server - an unlistened node:http server that answers the socket operations.
   * @param options.log - a logger with info and warn.
   * @param options.intervalMs - how often a non-carrier tries the lock and the carrier rechecks the path.
   * @param options.lockOptions - CarrierLock options, for tests.
   */
  constructor({ path, lockFile, server, log, intervalMs = 1000, lockOptions = {} }) {
    this.path = path
    this.server = server
    this.log = log
    this.intervalMs = intervalMs
    this.lock = new CarrierLock(lockFile, lockOptions)
    this.listening = false
    this.inode = undefined
    this.state = { carrier: false, reason: 'starting', holderPid: null }
    this.timer = undefined
    this.ticking = undefined
    this.stopped = false
    this.temporary = join(dirname(path), `.mux-${process.pid}-${instances++}`)
  }

  isCarrier() {
    return this.state.carrier
  }

  /** {carrier, socket, reason, holderPid}: why this host does or does not hold the socket. */
  status() {
    return { carrier: this.state.carrier, socket: this.path, reason: this.state.reason, holderPid: this.state.holderPid }
  }

  async start() {
    await this.tick()
    this.timer = setInterval(() => { void this.tick() }, this.intervalMs)
    this.timer.unref?.()
  }

  /** One round; concurrent calls share the round in progress. */
  tick() {
    this.ticking ??= this.round().finally(() => { this.ticking = undefined })
    return this.ticking
  }

  async round() {
    if (this.stopped) return
    if (this.state.carrier) {
      this.recheck()
      return
    }
    try {
      if (!(await this.lock.tryAcquire())) {
        await this.describeHolder()
        return
      }
      if (this.stopped) {
        this.lock.release()
        return
      }
      await this.listenTemporary()
      this.publish()
      this.sweepTemporaries()
      this.setState(true, 'carrier', process.pid, `serving kander on ${this.path}`)
    } catch (error) {
      if (this.listening) await this.closeServer()
      // Let another host try; this one tries again next round.
      this.lock.release()
      if (error instanceof Blocked) {
        this.setState(false, 'blocked', null, `${this.path} exists and is not a socket; remove it to let this host serve kander`)
      } else {
        this.setState(false, 'unreachable', null, `cannot serve ${this.path}: ${error?.message ?? error}`)
      }
    }
  }

  // The carrier holds the lock for as long as it runs, so a path that no
  // longer names its socket was changed from outside.
  recheck() {
    if (this.ownsPath()) return
    try {
      this.publish()
      this.log.warn(`${this.path} was changed from outside; linked this host's socket again`)
    } catch (error) {
      this.log.warn(`cannot link ${this.path} again: ${error?.message ?? error}`)
    }
  }

  async describeHolder() {
    try {
      const holder = await probeSocket(this.path)
      this.setState(false, 'held', holder.pid, `${this.path} is held by another dsh host${holder.pid ? ` (pid ${holder.pid})` : ''}; this host does not host kander panes and waits for the carrier lock`)
    } catch {
      // Another host holds the lock and has not published yet.
      this.setState(false, 'waiting', null, `waiting for the carrier lock of ${this.path}`)
    }
  }

  // Only the lock holder runs this: a socket at the path is left over. A path
  // that is not a socket is never replaced.
  publish() {
    let stat
    try {
      stat = lstatSync(this.path)
    } catch (error) {
      if (error?.code !== 'ENOENT') throw error
    }
    if (stat && !stat.isSocket()) throw new Blocked()
    if (stat) unlinkSync(this.path)
    linkSync(this.temporary, this.path)
  }

  // Removes the temporary names of hosts that are gone; a host killed while
  // it was the carrier leaves its name behind.
  sweepTemporaries() {
    const directory = dirname(this.path)
    for (const name of readdirSync(directory)) {
      const match = /^\.mux-([0-9]+)-[0-9]+$/.exec(name)
      if (!match || join(directory, name) === this.temporary || alive(Number(match[1]))) continue
      try { unlinkSync(join(directory, name)) } catch {}
    }
  }

  listenTemporary() {
    // A name left by a killed host with the same pid would refuse the bind.
    try { unlinkSync(this.temporary) } catch {}
    return new Promise((resolve, reject) => {
      const failed = (error) => {
        this.server.off('listening', listened)
        reject(error)
      }
      const listened = () => {
        this.server.off('error', failed)
        this.listening = true
        try {
          chmodSync(this.temporary, 0o600)
          this.inode = lstatSync(this.temporary).ino
        } catch (error) {
          reject(error)
          return
        }
        resolve()
      }
      this.server.once('error', failed)
      this.server.once('listening', listened)
      this.server.listen(this.temporary)
    })
  }

  ownsPath() {
    try {
      const stat = lstatSync(this.path)
      return stat.isSocket() && stat.ino === this.inode
    } catch {
      return false
    }
  }

  closeServer() {
    this.listening = false
    this.inode = undefined
    return new Promise((resolve) => {
      this.server.close(() => resolve())
      this.server.closeAllConnections?.()
    })
  }

  setState(carrier, reason, holderPid, message) {
    const changed = this.state.carrier !== carrier || this.state.reason !== reason || this.state.holderPid !== holderPid
    this.state = { carrier, reason, holderPid }
    if (!changed) return
    if (carrier) this.log.info(message)
    else this.log.warn(message)
  }

  /**
   * Stops serving: the carrier removes its socket and closes its server, then
   * awaits cleanup (ending its panes) while it still holds the lock, and only
   * then releases the lock to the next host.
   */
  async stop(cleanup = async () => {}) {
    this.stopped = true
    if (this.timer) clearInterval(this.timer)
    await this.ticking
    if (this.state.carrier && this.ownsPath()) {
      try { unlinkSync(this.path) } catch {}
    }
    if (this.listening) await this.closeServer()
    try {
      await cleanup()
    } finally {
      this.lock.release()
      this.state = { carrier: false, reason: 'stopped', holderPid: null }
    }
  }
}
