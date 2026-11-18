package notify

import (
	"context"

	"github.com/dualface/kander/internal/terminal"
)

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
