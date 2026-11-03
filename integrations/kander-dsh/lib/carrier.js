// Socket ownership between the dsh hosts of one DSH_HOME. The socket path is
// the lock: the host whose server the path names is the carrier host, and only
// it hosts dsh-launcher panes. A host that finds a live socket stays a
// non-carrier and checks again on every tick, so it takes the socket over
// without a restart once the carrier exits. A carrier whose path stops naming
// its own socket (another host replaced a socket it judged stale) closes its
// server and competes again.
//
// A server binds a private temporary name and renames it onto the path. The
// rename replaces a stale socket atomically, the socket is 0600 before it
// becomes reachable, and closing the server only unlinks the temporary name
// node bound, never a path that already names another host's socket. The
// temporary name is short and per pid: a unix socket path is limited to about
// 100 bytes, and one host binds one name at a time.
import { chmodSync, lstatSync, renameSync, unlinkSync } from 'node:fs'
import { request } from 'node:http'
import { dirname, join } from 'node:path'
import { PROTOCOL_VERSION } from './protocol.js'

const PROBE_TIMEOUT_MS = 1000

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

export class Carrier {
  /**
   * @param options.path - the socket path.
   * @param options.server - an unlistened node:http server that answers the socket operations.
   * @param options.log - a logger with info and warn.
   * @param options.intervalMs - how often a non-carrier retries and a carrier rechecks the path.
   */
  constructor({ path, server, log, intervalMs = 1000 }) {
    this.path = path
    this.server = server
    this.log = log
    this.intervalMs = intervalMs
    this.listening = false
    this.inode = undefined
    this.state = { carrier: false, reason: 'starting', holderPid: null }
    this.timer = undefined
    this.ticking = undefined
    this.stopped = false
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

  /** One ownership round; concurrent calls share the round in progress. */
  tick() {
    this.ticking ??= this.round().finally(() => { this.ticking = undefined })
    return this.ticking
  }

  async round() {
    if (this.stopped) return
    if (this.listening) {
      if (this.ownsPath()) return
      await this.closeServer()
      this.setState(false, 'lost', null, `${this.path} no longer names this host's socket; competing for it again`)
    }
    let holder
    try {
      holder = await probeSocket(this.path)
    } catch (error) {
      await this.claimFree(error)
      return
    }
    this.setState(false, 'held', holder.pid, `${this.path} is held by another dsh host${holder.pid ? ` (pid ${holder.pid})` : ''}; this host does not host kander panes`)
  }

  // The probe failed: the path is absent, or names a socket nobody serves.
  async claimFree(error) {
    if (error?.code === 'ECONNREFUSED') {
      let stat
      try {
        stat = lstatSync(this.path)
      } catch (statError) {
        if (statError?.code !== 'ENOENT') {
          this.setState(false, 'unreachable', null, `cannot inspect ${this.path}: ${statError.message}`)
          return
        }
      }
      if (stat && !stat.isSocket()) {
        this.setState(false, 'blocked', null, `${this.path} exists and is not a socket; remove it to let this host serve kander`)
        return
      }
    } else if (error?.code !== 'ENOENT') {
      this.setState(false, 'unreachable', null, `cannot probe ${this.path}: ${error?.message ?? error}`)
      return
    }
    try {
      await this.listen()
    } catch (listenError) {
      this.setState(false, 'unreachable', null, `could not bind ${this.path}: ${listenError?.message ?? listenError}`)
      return
    }
    this.setState(true, 'carrier', process.pid, `serving kander on ${this.path}`)
  }

  listen() {
    const temporary = join(dirname(this.path), `.mux-${process.pid}`)
    // A name left by a killed host with the same pid would refuse the bind.
    try { unlinkSync(temporary) } catch {}
    return new Promise((resolve, reject) => {
      const failed = (error) => {
        this.server.off('listening', listened)
        reject(error)
      }
      const listened = () => {
        this.server.off('error', failed)
        try {
          chmodSync(temporary, 0o600)
          this.inode = lstatSync(temporary).ino
          renameSync(temporary, this.path)
        } catch (error) {
          this.server.close()
          reject(error)
          return
        }
        this.listening = true
        resolve()
      }
      this.server.once('error', failed)
      this.server.once('listening', listened)
      this.server.listen(temporary)
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

  /** Stops competing; a carrier removes its socket so another host can take over at once. */
  async stop() {
    this.stopped = true
    if (this.timer) clearInterval(this.timer)
    await this.ticking
    if (!this.listening) return
    const owned = this.ownsPath()
    await this.closeServer()
    if (owned) {
      try { unlinkSync(this.path) } catch {}
    }
    this.state = { carrier: false, reason: 'stopped', holderPid: null }
  }
}
