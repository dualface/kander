// Socket ownership between the dsh hosts of one DSH_HOME. The socket path is
// the lock: the host whose server the path names is the carrier host, and only
// it hosts dsh-launcher panes. A host that finds a live socket stays a
// non-carrier and checks again on every tick, so it takes the socket over
// without a restart once the carrier exits.
//
// Publication never replaces a path. A server binds a private temporary name
// (0600 before anything can reach it) and hard-links it onto the path; link
// fails with EEXIST while any socket is there, so two hosts never both publish.
// A socket nobody serves (left by a killed host) is moved aside with rename,
// and when the moved inode is not the stale one this host inspected (another
// host published in between) it is linked back. Closing a server unlinks only
// the temporary name node bound, so a host never removes another host's
// socket. The temporary name is short and per process and instance: a unix
// socket path is limited to about 100 bytes.
//
// A carrier whose path no longer names its own socket closes its server and
// calls onLost, which ends its panes: kander can no longer reach them, and an
// agent that kander reads as stopped must not keep running beside the one a
// resume starts.
import { randomBytes } from 'node:crypto'
import { chmodSync, linkSync, lstatSync, renameSync, unlinkSync } from 'node:fs'
import { request } from 'node:http'
import { dirname, join } from 'node:path'
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
   * @param options.server - an unlistened node:http server that answers the socket operations.
   * @param options.log - a logger with info and warn.
   * @param options.onLost - awaited after this host stopped being the carrier.
   * @param options.intervalMs - how often a non-carrier retries and a carrier rechecks the path.
   */
  constructor({ path, server, log, onLost = async () => {}, intervalMs = 1000 }) {
    this.path = path
    this.server = server
    this.log = log
    this.onLost = onLost
    this.intervalMs = intervalMs
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

  /** One ownership round; concurrent calls share the round in progress. */
  tick() {
    this.ticking ??= this.round().finally(() => { this.ticking = undefined })
    return this.ticking
  }

  async round() {
    if (this.stopped) return
    if (this.state.carrier) {
      if (this.ownsPath()) return
      await this.closeServer()
      this.setState(false, 'lost', null, `${this.path} no longer names this host's socket; ending this host's kander panes and competing again`)
      try {
        await this.onLost()
      } catch (error) {
        this.log.warn(`could not end every kander pane after losing ${this.path}: ${error?.message ?? error}`)
      }
    }
    try {
      await this.claim()
    } catch (error) {
      if (this.listening) await this.closeServer()
      if (error instanceof Blocked) {
        this.setState(false, 'blocked', null, `${this.path} exists and is not a socket; remove it to let this host serve kander`)
      } else {
        this.setState(false, 'unreachable', null, `cannot claim ${this.path}: ${error?.message ?? error}`)
      }
    }
  }

  async claim() {
    const temporary = await this.listenTemporary()
    // The second attempt follows the removal of a stale socket.
    for (let attempt = 0; attempt < 2; attempt++) {
      try {
        linkSync(temporary, this.path)
      } catch (error) {
        if (error?.code !== 'EEXIST') throw error
        let holder
        try {
          holder = await probeSocket(this.path)
        } catch (probeError) {
          if (probeError?.code !== 'ECONNREFUSED' && probeError?.code !== 'ENOENT') throw probeError
          this.removeStale()
          continue
        }
        await this.closeServer()
        this.setState(false, 'held', holder.pid, `${this.path} is held by another dsh host${holder.pid ? ` (pid ${holder.pid})` : ''}; this host does not host kander panes`)
        return
      }
      unlinkSync(temporary)
      this.setState(true, 'carrier', process.pid, `serving kander on ${this.path}`)
      return
    }
    // Another host published right after this one removed a stale socket.
    await this.closeServer()
    this.setState(false, 'held', null, `${this.path} was taken by another dsh host; this host does not host kander panes`)
  }

  // Moves aside the socket at the path only while it is the stale inode this
  // host inspected. A socket another host published in between goes back,
  // unless the path is taken again; then that host steps down on its next
  // round, because the path no longer names its socket.
  removeStale() {
    let stale
    try {
      stale = lstatSync(this.path)
    } catch (error) {
      if (error?.code === 'ENOENT') return
      throw error
    }
    if (!stale.isSocket()) throw new Blocked()
    const aside = join(dirname(this.path), `.stale-${process.pid}-${randomBytes(4).toString('hex')}`)
    try {
      renameSync(this.path, aside)
    } catch (error) {
      if (error?.code === 'ENOENT') return
      throw error
    }
    try {
      if (lstatSync(aside).ino !== stale.ino) {
        try { linkSync(aside, this.path) } catch (error) { if (error?.code !== 'EEXIST') throw error }
      }
    } finally {
      unlinkSync(aside)
    }
  }

  /** Binds the server to this host's temporary name; resolves the name. */
  listenTemporary() {
    const temporary = this.temporary
    // A name left by a killed host with the same pid would refuse the bind.
    try { unlinkSync(temporary) } catch {}
    return new Promise((resolve, reject) => {
      const failed = (error) => {
        this.server.off('listening', listened)
        reject(error)
      }
      const listened = () => {
        this.server.off('error', failed)
        this.listening = true
        try {
          chmodSync(temporary, 0o600)
          this.inode = lstatSync(temporary).ino
        } catch (error) {
          reject(error)
          return
        }
        resolve(temporary)
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
    const owned = this.state.carrier && this.ownsPath()
    await this.closeServer()
    if (owned) {
      try { unlinkSync(this.path) } catch {}
    }
    this.state = { carrier: false, reason: 'stopped', holderPid: null }
  }
}
