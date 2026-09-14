package takeover

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/launch"
	"github.com/dualface/kander/internal/liveness"
	"github.com/dualface/kander/internal/notify"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

const pollInterval = 100 * time.Millisecond

var (
	lookPath = exec.LookPath
	sleepFn  = time.Sleep
	nowFn    = time.Now
)

func t(id string, args ...any) string {
	return config.Text(id, args...)
}

func takeoverError(id string, args ...any) error {
	return &notify.Error{Message: config.Text(id, args...)}
}

// AgentExitCommand reads exit_command from the resolved agent definition,
// including dialect inheritance. An omitted or empty value is refused so
// dismiss keeps the container.
func AgentExitCommand(agent string) (string, error) {
	definition, err := config.LoadAgent(agent)
	if err != nil {
		return "", err
	}
	if definition.ExitCommand == nil || *definition.ExitCommand == "" {
		return "", takeoverError("takeover.agent_exit_command_missing", agent)
	}
	return *definition.ExitCommand, nil
}

func probeConn(program string) terminal.Conn {
	return terminal.Conn{Program: program, Run: terminal.ProbeRunner}
}

func paneFacts(backend terminal.Backend, program, paneID string, timeout time.Duration) (terminal.PaneFacts, error) {
	ctx, cancel := probe.TimeoutContext(timeout)
	defer cancel()
	return backend.PaneFacts(ctx, probeConn(program), paneID)
}

// agentBackend is the backend that locates a session without a recorded
// address, because its panes report agent identity.
func agentBackend() (terminal.Backend, bool) {
	return terminal.FindBackend(func(c terminal.Capabilities) bool { return c.AgentIdentity })
}

func notInPath(backend terminal.Backend) error {
	if backend.Capabilities().AgentIdentity {
		return takeoverError("liveness.herdr_is_not_in_path")
	}
	return takeoverError("liveness.tmux_is_not_in_path")
}

// closeContainer closes the container and reports a run failure with its
// original error rather than the launch-time invocation wrapper.
func closeContainer(backend terminal.Backend, program string, address terminal.Address) error {
	err := backend.CloseContainer(context.Background(), probeConn(program), address)
	commandErr, ok := terminal.AsCommandError(err)
	if !ok || commandErr.Kind != terminal.KindExec {
		return err
	}
	if backend.Capabilities().AgentIdentity {
		return takeoverError("launch.failed_to_close_tab", address.Container, commandErr.Cause.Error())
	}
	return takeoverError("launch.failed_to_close_tmux_window", commandErr.Cause.Error())
}

// sendAgentExit types the exit command into a pane without agent-aware delivery.
func sendAgentExit(backend terminal.Backend, program, paneID, command string) error {
	err := backend.DeliverText(context.Background(), probeConn(program), paneID, command)
	if commandErr, ok := terminal.AsCommandError(err); ok {
		return takeoverError("takeover.tmux_failed_to_deliver_the_agent_exit_command", commandErr.Detail())
	}
	return err
}

// validateAgentContainer requires the pane to still live in the target tab and that tab to hold only this pane.
func validateAgentContainer(backend terminal.Backend, program string, address terminal.Address, pane terminal.PaneFacts) error {
	if pane.Container != address.Container {
		return takeoverError(
			"takeover.herdr_pane_tab_mismatch_task_pane", address.Container, orNA(pane.Container),
		)
	}
	topology, err := backend.Topology(context.Background(), probeConn(program), address)
	if err != nil {
		return err
	}
	panes := topology.Panes
	if len(panes) != 1 || panes[0] != address.Pane {
		return takeoverError(
			"takeover.the_herdr_tab_does_not_contain_only_the_target", address.Container, orNA(strings.Join(panes, ",")),
		)
	}
	return nil
}

// validateProcessContainer requires the pane to still live in the target session/window and that window to hold only this pane.
func validateProcessContainer(backend terminal.Backend, program string, address terminal.Address) error {
	topology, err := backend.Topology(context.Background(), probeConn(program), address)
	if err != nil {
		return err
	}
	if topology.Session != address.Session || topology.Container != address.Container {
		return takeoverError(
			"takeover.tmux_pane_container_mismatch_task_pane", address.Session, address.Container, orNA(topology.Session), orNA(topology.Container),
		)
	}
	if topology.PaneCount != "1" {
		return takeoverError(
			"takeover.the_tmux_window_does_not_contain_only_the_target", address.Container, orNA(topology.PaneCount),
		)
	}
	return nil
}

func orNA(v string) string {
	if v == "" {
		return "N/A"
	}
	return v
}

func agentCommandName(agent string) (string, error) {
	definition, err := config.LoadAgent(agent)
	return definition.ProcessName, err
}

func toLive(session launch.AgentSession) liveness.TaskSession {
	return liveness.TaskSession{Agent: session.Agent, Reference: session.Reference}
}
