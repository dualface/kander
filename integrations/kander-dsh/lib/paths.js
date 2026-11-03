// The plugin keeps its socket and pane sequence in one directory per DSH_HOME,
// so every dsh host of that home (Web and Desktop profiles alike) competes for
// the same socket. The embedded kander definition dsh.json derives the same
// path: $DSH_HOME/kander/mux.sock, or ~/.dsh/kander/mux.sock without DSH_HOME.
import { chmodSync, lstatSync, mkdirSync } from 'node:fs'
import { homedir } from 'node:os'
import { join } from 'node:path'

/** The plugin directory of a DSH_HOME; an empty DSH_HOME counts as unset, as in kander. */
export function stateDirectory(env = process.env, home = homedir()) {
  const root = env.DSH_HOME ? env.DSH_HOME : join(home, '.dsh')
  return join(root, 'kander')
}

export const socketPath = (directory) => join(directory, 'mux.sock')
export const lockPath = (directory) => join(directory, 'lock')

/**
 * Creates the directory with mode 0700, or tightens an existing one. A symbolic
 * link, a non-directory, or a directory of another user is refused: the socket
 * inside it is the only barrier against other local users.
 */
export function ensurePrivateDirectory(directory) {
  mkdirSync(directory, { recursive: true, mode: 0o700 })
  const stat = lstatSync(directory)
  if (stat.isSymbolicLink() || !stat.isDirectory()) {
    throw new Error(`${directory} is not a directory`)
  }
  if (typeof process.getuid === 'function' && stat.uid !== process.getuid()) {
    throw new Error(`${directory} belongs to another user`)
  }
  if ((stat.mode & 0o777) !== 0o700) chmodSync(directory, 0o700)
}
