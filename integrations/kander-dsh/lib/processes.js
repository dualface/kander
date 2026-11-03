// Whether a process exists, for the cleanup of names left by dead hosts.
export function alive(pid) {
  try {
    process.kill(pid, 0)
    return true
  } catch (error) {
    // EPERM: the process exists but belongs to another user.
    return error?.code === 'EPERM'
  }
}
