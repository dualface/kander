package takeover

import (
	"context"
	"fmt"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/launch"
	"github.com/dualface/kander/internal/liveness"
	"github.com/dualface/kander/internal/notify"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
	"github.com/dualface/kander/internal/window"
)

func reverseLookupStale[T any](detail string, lookup func() (T, error)) (T, error) {
	value, err := lookup()
	if err != nil {
		var zero T
		return zero, takeoverError(
			"notify.stale_address_lookup", detail, err.Error(),
		)
	}
	return value, nil
}

func commandDismiss(root, taskID string, timeout float64) error {
	if _, err := config.Load(false); err != nil {
		return err
	}
	loaded, err := board.LoadBoard(root)
	if err != nil {
		return err
	}
	entry, err := board.Locate(loaded, taskID)
	if err != nil {
		return err
	}
	if entry.State != "done" && entry.State != "archived" {
		return takeoverError(
			"takeover.only_tasks_in_done_or_archived_can_be_dismissed", entry.TaskID, entry.State,
		)
	}
	if err := launch.ValidateTimeout(timeout, "dismiss"); err != nil {
		return err
	}
	text, err := board.ReadDocument(entry)
	if err != nil {
		return err
	}
	if err := board.ValidateMutable(entry, text); err != nil {
		return err
	}
	windowValue := board.MetadataFrom(text, window.WindowField)
	backend, address, parsed := terminal.ParseWindow(windowValue)
	if windowValue != "" && !parsed {
		return takeoverError(
			"takeover.task_has_no_dismissible_terminal_container", windowValue,
		)
	}
	session, err := launch.ResolvedSessionIdentity(entry.TaskID, text)
	if err != nil {
		return err
	}
	command, err := AgentExitCommand(session.Agent)
	if err != nil {
		return err
	}
	live := toLive(session)
	var channel, container string
	if !parsed {
		agent, ok := agentBackend()
		if !ok {
			return takeoverError("takeover.task_has_no_dismissible_terminal_container", windowValue)
		}
		backend = agent
	}
	if backend.Capabilities().AgentIdentity {
		channel, container, err = dismissAgentPane(backend, address, parsed, live, session, command, timeout)
	} else {
		channel, container, err = dismissProcessPane(backend, address, live, session, command, timeout)
	}
	if err != nil {
		return err
	}
	fmt.Println(t(
		"takeover.dismissed_channel_closed_container", entry.TaskID, channel, container,
	))
	return nil
}

func dismissAgentPane(backend terminal.Backend, address terminal.Address, parsed bool, live liveness.TaskSession, session launch.AgentSession, command string, timeout float64) (string, string, error) {
	program, err := lookPath(backend.Executable())
	if err != nil {
		return "", "", notInPath(backend)
	}
	var tabID, paneID string
	if parsed {
		tabID, paneID = address.Container, address.Pane
		probeResult := notify.AgentNotifyProbe(backend, program, paneID, live, probe.DefaultCommandTimeout)
		if probeResult.State == "stale" {
			discovered, err := reverseLookupStale(probeResult.Detail, func() (terminal.Address, error) {
				return liveness.ReverseLookup(context.Background(), backend, program, live)
			})
			if err != nil {
				return "", "", err
			}
			tabID, paneID, err = notify.ExplicitPaneTarget(backend, program, discovered.Pane)
			if err != nil {
				return "", "", err
			}
		}
	} else {
		discovered, err := liveness.ReverseLookup(context.Background(), backend, program, live)
		if err != nil {
			return "", "", err
		}
		tabID, paneID, err = notify.ExplicitPaneTarget(backend, program, discovered.Pane)
		if err != nil {
			return "", "", err
		}
	}
	pane, err := notify.AgentNotifyTarget(backend, program, paneID, live)
	if err != nil {
		return "", "", err
	}
	target := terminal.Address{Container: tabID, Pane: paneID}
	if err := validateAgentContainer(backend, program, target, pane); err != nil {
		return "", "", err
	}
	if err := notify.AgentPrompt(backend, program, paneID, command); err != nil {
		return "", "", err
	}
	paneExists, err := agentWaitExit(backend, program, target, session, timeout)
	if err != nil {
		return "", "", err
	}
	if paneExists {
		if err := closeContainer(backend, program, target); err != nil {
			return "", "", err
		}
	}
	return backend.Name(), tabID, nil
}

func dismissProcessPane(backend terminal.Backend, address terminal.Address, live liveness.TaskSession, session launch.AgentSession, command string, timeout float64) (string, string, error) {
	target := address
	program, err := lookPath(backend.Executable())
	if err != nil {
		return "", "", notInPath(backend)
	}
	probeResult := notify.ProcessNotifyProbe(backend, program, target.Pane, live, probe.DefaultCommandTimeout)
	if probeResult.State == "stale" {
		location, err := reverseLookupStale(probeResult.Detail, func() (terminal.Address, error) {
			return liveness.ReverseLookup(context.Background(), backend, program, live)
		})
		if err != nil {
			return "", "", err
		}
		target.Pane = location.Pane
		topology, err := backend.Topology(context.Background(), probeConn(program), target)
		if err != nil {
			return "", "", err
		}
		target.Session, target.Container = topology.Session, topology.Container
	}
	if err := notify.ProcessNotifyTarget(backend, program, target.Pane, live); err != nil {
		return "", "", err
	}
	if err := validateProcessContainer(backend, program, target); err != nil {
		return "", "", err
	}
	if err := sendAgentExit(backend, program, target.Pane, command); err != nil {
		return "", "", err
	}
	windowExists, err := processWaitExit(backend, program, target, session, timeout)
	if err != nil {
		return "", "", err
	}
	if windowExists {
		if err := closeContainer(backend, program, target); err != nil {
			return "", "", err
		}
	}
	return backend.Name(), target.Container, nil
}
