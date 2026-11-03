// The name of a pane's foreground process, compared by kander with the agent's
// process name the way tmux reports pane_current_command. The process group
// leader's pid equals the group id.
import { execFile as nodeExecFile } from 'node:child_process'
import { readFile as nodeReadFile } from 'node:fs/promises'
import { basename } from 'node:path'

const run = (execFile, file, args) => new Promise((resolve, reject) => {
  execFile(file, args, { timeout: 2000, maxBuffer: 64 << 10 }, (error, stdout) => {
    if (error) reject(error)
    else resolve(String(stdout))
  })
})

/**
 * Returns the command name of a process: /proc/<pid>/comm on Linux, the
 * basename of `ps -o comm=` elsewhere (macOS prints the executable path).
 * Returns '' when the process cannot be read.
 */
export async function processName(pid, { platform = process.platform, readFile = nodeReadFile, execFile = nodeExecFile } = {}) {
  if (!Number.isInteger(pid) || pid <= 0) return ''
  try {
    if (platform === 'linux') return (await readFile(`/proc/${pid}/comm`, 'utf8')).trim()
    const line = (await run(execFile, 'ps', ['-o', 'comm=', '-p', String(pid)])).trim()
    return line === '' ? '' : basename(line)
  } catch {
    return ''
  }
}
