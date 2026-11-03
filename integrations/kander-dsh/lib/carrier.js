// Socket ownership between the dsh hosts of one DSH_HOME. The socket path is
// the lock-guarded state: the host whose server the path names is the carrier
// host, and only it hosts dsh-launcher panes. A host that finds a live socket
// stays a non-carrier and checks again on every tick, so it takes the socket
// over without a restart once the carrier exits.
//
// Judging the path, removing a stale socket, publishing, and removing a
// carrier's own socket on stop all run under the DirectoryLock the hosts
// share, and the judgement is made on the object at the path inside the lock.
// So a stale verdict can never meet a socket published after it, and two hosts
// never both publish.
//
// A server binds a private temporary name (0600 before anything can reach
// it) and hard-links it onto the path. The temporary name stays while the
// host is the carrier, so a path that was removed from outside can be linked
// again; closing the server unlinks only that name, never the path. The name
// is short and per process and instance: a unix socket path is limited to
// about 100 bytes.
//
// A carrier whose path names another socket closes its server and calls
// onLost, which ends its panes: kander can no longer reach them, and an agent
// that kander reads as stopped must not keep running beside the one a resume
// starts.
import { chmodSync, linkSync, lstatSync, readdirSync, unlinkSync } from 'node:fs'
import { request } from 'node:http'
import { dirname, join } from 'node:path'
import { PROTOCOL_VERSION } from './protocol.js'

const PROBE_TIMEOUT_MS = 1000
let instances = 0

const alive = (pid) => {
  try {
    process.kill(pid, 0)
    return true
  } catch (error) {
    return error?.code === 'EPERM'
  }
}

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

const lstatOrNull = (path) => {
  try {
    return lstatSync(path)
  } catch (error) {
    if (error?.code === 'ENOENT') return null
    throw error
  }
}

export class Carrier {
  /**
   * @param options.path - the socket path.
   * @param options.server - an unlistened node:http server that answers the socket operations.
   * @param options.lock - the DirectoryLock shared by the hosts of this DSH_HOME.
   * @param options.log - a logger with info and warn.
   * @param options.onLost - awaited after this host stopped being the carrier.
   * @param options.intervalMs - how often a non-carrier retries and a carrier rechecks the path.
   * @param options.probe - resolves {pid} for a live socket, rejects with the connect error.
   */
  constructor({ path, server, lock, log, onLost = async () => {}, intervalMs = 1000, probe = probeSocket }) {
    this.path = path
    this.server = server
    this.lock = lock
    this.log = log
    this.onLost = onLost
    this.intervalMs = intervalMs
    this.probe = probe
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
    try {
      if (this.state.carrier) {
        if (this.ownsPath() || !(await this.lock.run(() => this.lostPath()))) return
        await this.closeServer()
        this.setState(false, 'lost', null, `${this.path} names another host's socket; ending this host's kander panes and competing again`)
        try {
          await this.onLost()
        } catch (error) {
          this.log.warn(`could not end every kander pane after losing ${this.path}: ${error?.message ?? error}`)
        }
      }
      await this.claim()
    } catch (error) {
      if (this.state.carrier) {
        // The carrier keeps serving; the next round checks the path again.
        this.log.warn(`cannot check ${this.path}: ${error?.message ?? error}`)
        return
      }
      if (this.listening) await this.closeServer()
      if (error instanceof Blocked) {
        this.setState(false, 'blocked', null, `${this.path} exists and is not a socket; remove it to let this host serve kander`)
      } else {
        this.setState(false, 'unreachable', null, `cannot claim ${this.path}: ${error?.message ?? error}`)
      }
    }
  }

  // Under the lock: true when the path names another socket. A path removed
  // from outside is linked again, and the host stays the carrier.
  lostPath() {
    const stat = lstatOrNull(this.path)
    if (stat === null) {
      linkSync(this.temporary, this.path)
      return false
    }
    return !(stat.isSocket() && stat.ino === this.inode)
  }

  async claim() {
    await this.listenTemporary()
    const holder = await this.lock.run(async () => {
      const stat = lstatOrNull(this.path)
      if (stat !== null) {
        if (!stat.isSocket()) throw new Blocked()
        try {
          return await this.probe(this.path)
        } catch (error) {
          if (error?.code !== 'ECONNREFUSED' && error?.code !== 'ENOENT') throw error
        }
        // Nobody serves the socket, and no host can publish while this one
        // holds the lock: this is the stale object the probe judged.
        unlinkSync(this.path)
      }
      linkSync(this.temporary, this.path)
      this.sweepTemporaries()
      return null
    })
    if (holder === null) {
      this.setState(true, 'carrier', process.pid, `serving kander on ${this.path}`)
      return
    }
    await this.closeServer()
    this.setState(false, 'held', holder.pid, `${this.path} is held by another dsh host${holder.pid ? ` (pid ${holder.pid})` : ''}; this host does not host kander panes`)
  }

  // Under the lock: removes the temporary names of hosts that are gone. A
  // carrier keeps its name while it serves, so a killed carrier leaves one.
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

  /** Stops competing; a carrier removes its socket so another host can take over at once. */
  async stop() {
    this.stopped = true
    if (this.timer) clearInterval(this.timer)
    await this.ticking
    if (this.state.carrier) {
      await this.lock.run(() => {
        if (this.ownsPath()) unlinkSync(this.path)
      }).catch((error) => this.log.warn(`could not remove ${this.path}: ${error?.message ?? error}`))
    }
    if (this.listening) await this.closeServer()
    this.state = { carrier: false, reason: 'stopped', holderPid: null }
  }
}
