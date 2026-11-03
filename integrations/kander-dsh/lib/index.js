// kander-dsh: hosts Kander task agents in dsh-owned terminals. kander drives
// the panes through a local socket with its embedded dsh.json definition
// (launcher `dsh`); see docs/dsh-launcher.md in the kander repository. The
// page half (client.js) is the Kander view; its routes are in routes.js, see
// docs/dsh-kander-view.md.
import { Boards } from './boards.js'
import { KanderHost } from './host.js'
import { createRoutes } from './routes.js'

export const name = 'kander-dsh'
export const inject = ['subprocess']

/**
 * The environment added to every pane. dsh removes DSH_* and credential-shaped
 * names from its children; DSH_HOME is forwarded so a kander inside the pane
 * finds this socket, and config.env names further host variables to forward.
 */
export function paneEnvironment(config = {}, env = process.env) {
  const out = {}
  if (env.DSH_HOME) out.DSH_HOME = env.DSH_HOME
  for (const variable of config.env ?? []) {
    if (typeof variable === 'string' && env[variable] !== undefined) out[variable] = env[variable]
  }
  return out
}

/**
 * @param config.env - names of host environment variables forwarded to the panes.
 * @param config.kander - absolute path of the kander the Kander view runs; found per board when absent.
 */
export function apply(ctx, config = {}) {
  const log = ctx.logger?.('kander-dsh') ?? console
  const host = new KanderHost({ subprocess: ctx.subprocess, log, env: paneEnvironment(config) })
  host.start().catch((error) => log.warn(`kander-dsh did not start: ${error?.message ?? error}`))
  ctx.effect(() => () => host.stop(), 'kander-dsh.host')

  const boards = new Boards({ kander: typeof config.kander === 'string' ? config.kander : '' })
  ctx.inject(['connection'], (inner) => {
    const connection = inner.get('connection')
    if (typeof connection?.fetch?.register !== 'function') return
    const disposers = createRoutes(host, boards).map((route) => connection.fetch.register(route))
    inner.effect(() => () => Promise.all(disposers.map((dispose) => dispose())), 'kander-dsh.routes')
  })
}
