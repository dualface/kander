package takeover

import (
	"context"
	"strings"

	"github.com/dualface/kander/internal/launch"
	"github.com/dualface/kander/internal/notify"
	"github.com/dualface/kander/internal/terminal"
)

func closed(oldWindow, channel, container string) launch.CleanupResult {
	return launch.CleanupResult{Cleaned: true, OldWindow: orNA(oldWindow), Channel: channel, Container: container}
}

func retained(oldWindow, detail string) launch.CleanupResult {
	return launch.CleanupResult{Cleaned: false, OldWindow: orNA(oldWindow), Detail: strings.Join(strings.Fields(detail), " ")}
}

// Cleanup closes the original container under the dismiss gates once the new agent is alive after a takeover; on failure it only keeps and reports it.
func Cleanup(oldWindow string, oldSession launch.AgentSession, newWindow string, timeout float64) launch.CleanupResult {
	if oldWindow == "" || terminal.HasCapability(oldWindow, func(c terminal.Capabilities) bool { return !c.Container }) {
		return closed("N/A", "N/A", "N/A")
	}
	if oldWindow == newWindow {
		return closed("N/A", "N/A", "N/A")
	}
	backend, address, ok := terminal.ParseWindow(oldWindow)
	if !ok {
		return retained(oldWindow, t("takeover.old_window_metadata_is_invalid"))
	}
	cleanup := cleanupProcessContainer
	if backend.Capabilities().AgentIdentity {
		cleanup = cleanupAgentContainer
	}
	result, err := cleanup(backend, address, oldWindow, oldSession, timeout)
	if err != nil {
		return retained(oldWindow, err.Error())
	}
	return result
}

func cleanupAgentContainer(backend terminal.Backend, address terminal.Address, oldWindow string, oldSession launch.AgentSession, timeout float64) (launch.CleanupResult, error) {
	tabID, paneID := address.Container, address.Pane
	program, err := lookPath(backend.Executable())
	if err != nil {
		return launch.CleanupResult{}, notInPath(backend)
	}
	pane, err := paneFacts(backend, program, paneID, 0)
	if err != nil {
		return launch.CleanupResult{}, err
	}
	if pane.Gone {
		topology, err := backend.Topology(context.Background(), probeConn(program), address)
		if err != nil {
			return launch.CleanupResult{}, err
		}
		if len(topology.Panes) > 0 {
			return launch.CleanupResult{}, takeoverError(
				"takeover.the_old_pane_is_gone_but_its_tab_still", strings.Join(topology.Panes, ","),
			)
		}
		if err := closeContainer(backend, program, address); err != nil {
			return launch.CleanupResult{}, err
		}
		return closed(oldWindow, backend.Name(), tabID), nil
	}
	if err := validateAgentContainer(backend, program, address, pane); err != nil {
		return launch.CleanupResult{}, err
	}
	if pane.Agent != "" {
		if pane.Agent != oldSession.Agent || oldSession.Reference == "" || pane.AgentSession == "" || pane.AgentSession != oldSession.Reference {
			return launch.CleanupResult{}, takeoverError(
				"takeover.the_old_pane_agent_or_session_identity_does_not",
			)
		}
		if pane.AgentStatus != "idle" && pane.AgentStatus != "done" {
			return launch.CleanupResult{}, takeoverError(
				"takeover.the_old_pane_status_cannot_be_dismissed", orNA(pane.AgentStatus),
			)
		}
		command, err := AgentExitCommand(oldSession.Agent)
		if err != nil {
			return launch.CleanupResult{}, err
		}
		if err := notify.AgentPrompt(backend, program, paneID, command); err != nil {
			return launch.CleanupResult{}, err
		}
		paneExists, err := agentWaitExit(backend, program, address, oldSession, timeout)
		if err != nil {
			return launch.CleanupResult{}, err
		}
		if paneExists {
			if err := closeContainer(backend, program, address); err != nil {
				return launch.CleanupResult{}, err
			}
		}
	} else if err := closeContainer(backend, program, address); err != nil {
		return launch.CleanupResult{}, err
	}
	return closed(oldWindow, backend.Name(), tabID), nil
}

func cleanupProcessContainer(backend terminal.Backend, address terminal.Address, oldWindow string, oldSession launch.AgentSession, timeout float64) (launch.CleanupResult, error) {
	launcher, windowID, paneID := backend.Name(), address.Container, address.Pane
	program, err := lookPath(backend.Executable())
	if err != nil {
		return launch.CleanupResult{}, notInPath(backend)
	}
	facts, err := paneFacts(backend, program, paneID, 0)
	if err != nil {
		return launch.CleanupResult{}, err
	}
	if facts.Gone {
		exists, err := backend.ContainerExists(context.Background(), probeConn(program), address)
		if err != nil {
			return launch.CleanupResult{}, err
		}
		if !exists {
			return closed(oldWindow, launcher, windowID), nil
		}
		return launch.CleanupResult{}, takeoverError(
			"takeover.the_old_pane_is_gone_but_its_window_still",
		)
	}
	if err := validateProcessContainer(backend, program, address); err != nil {
		return launch.CleanupResult{}, err
	}
	expected, err := agentCommandName(oldSession.Agent)
	if err != nil {
		return launch.CleanupResult{}, err
	}
	if facts.Dead == "1" {
		if err := closeContainer(backend, program, address); err != nil {
			return launch.CleanupResult{}, err
		}
		return closed(oldWindow, launcher, windowID), nil
	}
	if facts.Dead != "0" || facts.Command != expected || oldSession.Reference == "" || facts.SessionMarker == "" || facts.SessionMarker != oldSession.Reference {
		return launch.CleanupResult{}, takeoverError(
			"takeover.the_old_tmux_pane_agent_or_session_identity_does",
		)
	}
	if facts.InMode != "0" {
		return launch.CleanupResult{}, takeoverError("takeover.the_old_tmux_pane_is_in_copy_mode")
	}
	command, err := AgentExitCommand(oldSession.Agent)
	if err != nil {
		return launch.CleanupResult{}, err
	}
	if err := sendAgentExit(backend, program, paneID, command); err != nil {
		return launch.CleanupResult{}, err
	}
	windowExists, err := processWaitExit(backend, program, address, oldSession, timeout)
	if err != nil {
		return launch.CleanupResult{}, err
	}
	if windowExists {
		if err := closeContainer(backend, program, address); err != nil {
			return launch.CleanupResult{}, err
		}
	}
	return closed(oldWindow, launcher, windowID), nil
}
