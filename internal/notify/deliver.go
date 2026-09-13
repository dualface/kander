package notify

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dualface/kander/internal/terminal"
)

// commandDetail renders a backend failure the way direct delivery reports it:
// the run error itself, or the trimmed stderr or exit status.
func commandDetail(err error) string {
	if commandErr, ok := terminal.AsCommandError(err); ok {
		return commandErr.Detail()
	}
	return err.Error()
}

// AgentPrompt delivers the body to the agent TUI already running in the pane, without using pane run.
func AgentPrompt(backend terminal.Backend, program, paneID, text string) error {
	// The probe runner bounds each command with the default budget.
	err := backend.DeliverText(context.Background(), probeConn(program), paneID, text)
	if err == nil {
		return nil
	}
	commandErr, ok := terminal.AsCommandError(err)
	if !ok {
		return err
	}
	if commandErr.Kind == terminal.KindExec {
		return notifyError("launch.herdr_invocation_failed", commandErr.Cause.Error())
	}
	return notifyError("notify.herdr_agent_prompt_failed", commandErr.Detail())
}

// directNotify delivers the instruction line and waits for its marker. A
// backend that waits on output itself is asked to; otherwise the pane text is
// polled until the timeout.
func directNotify(target DirectTarget, instruction, marker string, timeout float64) (bool, string, error) {
	if target.Backend.Capabilities().WaitOutput {
		return waitingNotify(target, instruction, marker, timeout)
	}
	return pollingNotify(target, instruction, marker, timeout)
}

func waitingNotify(target DirectTarget, instruction, marker string, timeout float64) (bool, string, error) {
	if err := AgentPrompt(target.Backend, target.Program, target.PaneID, instruction); err != nil {
		return false, "", err
	}
	fmt.Println(t("notify.delivered_waiting_for_acknowledgement_channel_herdr_direct"))
	flushStdout()
	ms := int(timeout * 1000)
	if ms < 1 {
		ms = 1
	}
	if err := target.Backend.WaitOutput(context.Background(), probeConn(target.Program), target.PaneID, marker, ms); err != nil {
		return false, commandDetail(err), nil
	}
	return true, "", nil
}

func pollingNotify(target DirectTarget, instruction, marker string, timeout float64) (bool, string, error) {
	if err := target.Backend.DeliverText(context.Background(), probeConn(target.Program), target.PaneID, instruction); err != nil {
		return false, "", notifyError("notify.tmux_direct_notification_failed", commandDetail(err))
	}
	fmt.Println(t("notify.delivered_waiting_for_acknowledgement_channel_tmux_direct"))
	flushStdout()
	deadline := nowFn().Add(time.Duration(timeout * float64(time.Second)))
	for {
		output, err := target.Backend.ReadOutput(context.Background(), probeConn(target.Program), target.PaneID)
		if err != nil {
			return false, commandDetail(err), nil
		}
		if strings.Contains(output, marker) {
			return true, "", nil
		}
		remaining := deadline.Sub(nowFn())
		if remaining <= 0 {
			return false, t("notify.acknowledgement_timed_out", marker), nil
		}
		d := pollInterval
		if remaining < d {
			d = remaining
		}
		sleepFn(d)
	}
}

// flushStdout only syncs regular files. On Windows, FlushFileBuffers on the write end of a pipe blocks until the read
// end has drained everything, and it is meaningless on a console handle; os.File writes are unbuffered anyway, so no extra flush is needed.
func flushStdout() {
	info, err := os.Stdout.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	_ = os.Stdout.Sync()
}
