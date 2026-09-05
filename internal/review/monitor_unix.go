//go:build unix

package review

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

func attachTracker(ctx reviewContext, lp *launchedProcess) {
	if lp == nil || lp.cmd == nil || lp.cmd.Process == nil {
		return
	}
	if ctx.settings.spawnsHelperProcesses {
		lp.tracker = newReviewerProcessTree(lp.cmd.Process.Pid)
	}
}

func observeTracker(lp *launchedProcess) {
	if lp != nil && lp.tracker != nil {
		lp.tracker.observe()
	}
}

func monitorSlice(ctx reviewContext, remaining time.Duration) time.Duration {
	cap := time.Second
	if ctx.settings.spawnsHelperProcesses {
		cap = time.Duration(treeObserveInterval * float64(time.Second))
	}
	if remaining < cap {
		return remaining
	}
	return cap
}

func waitExitCode(err error, lp *launchedProcess) int {
	if lp != nil && lp.cmd != nil && lp.cmd.ProcessState != nil {
		return unixWaitStatus(lp.cmd.ProcessState)
	}
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ProcessState != nil {
		return unixWaitStatus(ee.ProcessState)
	}
	return 1
}

func unixWaitStatus(state *os.ProcessState) int {
	if ws, ok := state.Sys().(syscall.WaitStatus); ok {
		if ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return ws.ExitStatus()
	}
	code := state.ExitCode()
	if code < 0 {
		return 128 - code
	}
	return code
}
