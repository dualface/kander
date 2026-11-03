// kander-dsh: hosts Kander task agents in dsh-owned terminals. kander drives
// the panes through a local socket with its embedded dsh.json definition
// (launcher `dsh`); see docs/dsh-launcher.md in the kander repository.
import { KanderHost } from './host.js'

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

/** @param config.env - names of host environment variables forwarded to the panes. */
export function apply(ctx, config = {}) {
  const log = ctx.logger?.('kander-dsh') ?? console
  const host = new KanderHost({ subprocess: ctx.subprocess, log, env: paneEnvironment(config) })
  host.start().catch((error) => log.warn(`kander-dsh did not start: ${error?.message ?? error}`))
  ctx.effect(() => () => host.stop(), 'kander-dsh.host')
}
