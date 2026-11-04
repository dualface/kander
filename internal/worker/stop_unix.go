//go:build unix

package worker

import "syscall"

// stopProcess asks the agent to end; the session group keeps its own
// cleanup, and the worktree is never touched.
func stopProcess(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }
