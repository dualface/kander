// The socket operations, one per step of kander's dsh.json definition. Each
// takes the form fields kander sends and returns the JSON fields the
// definition reads.
import { BadRequest, Gone, PROTOCOL_VERSION, required, upgradeSide } from './protocol.js'

const flag = (value) => (value ? '1' : '0')

/** @param host - the KanderHost that owns the panes. */
export function createOps(host) {
  const { mux } = host
  const paneOf = (params) => {
    const pane = mux.get(required(params, 'pane'))
    if (!pane) throw new Gone()
    return pane
  }
  const containerOf = (params) => {
    const pane = mux.byContainer(required(params, 'container'))
    if (!pane) throw new Gone()
    return pane
  }

  return {
    async hello(params) {
      const text = required(params, 'protocol')
      if (!/^[0-9]+$/.test(text)) throw new BadRequest(`invalid protocol ${text}`)
      return { protocol: String(PROTOCOL_VERSION), upgrade: upgradeSide(Number(text)), pid: process.pid }
    },
    async create_container(params) {
      const pane = mux.create({ cwd: required(params, 'cwd'), label: params.get('label') ?? '', project: params.get('project') ?? '' })
      return { container: pane.container, pane: pane.id }
    },
    async run_command(params) {
      const pane = paneOf(params)
      if (params.get('posix') !== 'true') throw new BadRequest('the dsh launcher runs only POSIX commands')
      // The pane starts in / and enters the startup directory itself, as tmux does.
      const argv = ['/bin/sh', '-c', `cd -- ${required(params, 'cwd_quoted')} && exec ${required(params, 'command')}`]
      await mux.run(pane, argv, '/')
      return {}
    },
    async set_session_marker(params) {
      paneOf(params).marker = params.get('value') ?? ''
      return {}
    },
    async pane_facts(params) {
      const pane = paneOf(params)
      const { command } = await mux.foreground(pane)
      return { command, in_mode: '0', dead: flag(pane.exited), session_marker: pane.marker }
    },
    async read_output(params) {
      return { text: mux.capture(paneOf(params)) }
    },
    async deliver_text(params) {
      await mux.write(paneOf(params), required(params, 'text'))
      return {}
    },
    async enter(params) {
      await mux.write(paneOf(params), '\r')
      return {}
    },
    async topology(params) {
      const pane = paneOf(params)
      return { container: pane.container, pane_count: '1' }
    },
    async container_exists(params) {
      return { container: containerOf(params).container }
    },
    async list() {
      const panes = await Promise.all(mux.list().map(async (pane) => ({
        pane: pane.id,
        container: pane.container,
        command: (await mux.foreground(pane)).command,
        dead: flag(pane.exited),
        marker: pane.marker,
      })))
      return { panes }
    },
    async focus(params) {
      host.recordFocus(paneOf(params))
      return {}
    },
    async close_container(params) {
      const pane = mux.byContainer(required(params, 'container'))
      if (pane) await mux.close(pane)
      return {}
    },
  }
}
