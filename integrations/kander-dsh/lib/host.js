// The host half of the plugin in one dsh host process: the pane multiplexer,
// the socket kander drives, and the JavaScript interface the plugin's page
// routes use. Only the carrier host (the one holding the socket) ever gets
// panes; another host's interface lists none and reports why it is not the
// carrier.
import { Carrier } from './carrier.js'
import { PaneSequence } from './ids.js'
import { Mux } from './mux.js'
import { createOps } from './ops.js'
import { ensurePrivateDirectory, sequencePath, socketPath, stateDirectory } from './paths.js'
import { Gone, createOpsServer } from './protocol.js'

export class KanderHost {
  /**
   * @param options.subprocess - the dsh subprocess service.
   * @param options.log - a logger with info and warn (the dsh host log).
   * @param options.directory - the plugin directory; defaults to the one of DSH_HOME.
   * @param options.env - variables added to every pane.
   * @param options.intervalMs - the socket ownership check interval.
   * @param options.foregroundName - resolves a process group id to a command name.
   */
  constructor({ subprocess, log, directory = stateDirectory(), env = {}, intervalMs, foregroundName }) {
    this.log = log
    this.directory = directory
    this.mux = new Mux({ subprocess, sequence: new PaneSequence(sequencePath(directory)), env, foregroundName })
    this.focusRequest = null
    this.carrier = new Carrier({ path: socketPath(directory), server: createOpsServer(createOps(this)), log, intervalMs })
  }

  async start() {
    ensurePrivateDirectory(this.directory)
    await this.carrier.start()
  }

  /** Stops serving kander and ends every pane program with the host. */
  async stop() {
    await this.carrier.stop()
    await this.mux.closeAll()
  }

  /** Whether this host holds the socket and so runs the dsh-launcher panes. */
  isCarrier() {
    return this.carrier.isCarrier()
  }

  /** {carrier, socket, reason, holderPid}; reason is carrier, held, lost, blocked, unreachable, starting or stopped. */
  carrierStatus() {
    return this.carrier.status()
  }

  /**
   * Every pane of this host: {pane, container, label, project, cwd, exited,
   * command, inputWaiting}. project is the main worktree kander sent.
   * inputWaiting is true while the foreground process group reads terminal
   * input, false when it does not, and null when dsh cannot tell.
   */
  async listPanes() {
    return Promise.all(this.mux.list().map(async (pane) => {
      const { command, inputWaiting } = await this.mux.foreground(pane)
      return { pane: pane.id, container: pane.container, label: pane.label, project: pane.project, cwd: pane.cwd, exited: pane.exited, command, inputWaiting }
    }))
  }

  /**
   * Calls listener with the buffered output of a pane, then with each new
   * chunk, and with null once its program exited.
   * @returns a function that ends the subscription.
   * @throws Gone for an unknown pane.
   */
  subscribe(paneId, listener) {
    return this.mux.subscribe(this.paneOf(paneId), listener)
  }

  /** Writes text to a pane program's terminal input. */
  write(paneId, data) {
    return this.mux.write(this.paneOf(paneId), data)
  }

  /** Changes a pane's terminal size. */
  resize(paneId, cols, rows) {
    return this.mux.resize(this.paneOf(paneId), cols, rows)
  }

  /** The latest kander focus request: {pane, container, cwd, at} with at in epoch milliseconds, or null. */
  lastFocus() {
    return this.focusRequest
  }

  recordFocus(pane) {
    this.focusRequest = { pane: pane.id, container: pane.container, cwd: pane.cwd, at: Date.now() }
  }

  paneOf(paneId) {
    const pane = this.mux.get(paneId)
    if (!pane) throw new Gone(`pane ${paneId} does not exist`)
    return pane
  }
}
