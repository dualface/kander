//go:build windows

package review

import (
	"os/exec"
	"time"
)

func attachTracker(ctx reviewContext, lp *launchedProcess) {}

func observeTracker(lp *launchedProcess) {}

func monitorSlice(ctx reviewContext, remaining time.Duration) time.Duration {
	cap := 100 * time.Millisecond
	if remaining < cap {
		return remaining
	}
	return cap
}

func waitExitCode(err error, lp *launchedProcess) int {
	if lp != nil && lp.cmd != nil && lp.cmd.ProcessState != nil {
		return lp.cmd.ProcessState.ExitCode()
	}
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return 1
}
