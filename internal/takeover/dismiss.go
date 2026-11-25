package takeover

import (
	"context"

	"github.com/dualface/kander/internal/launch"
	"github.com/dualface/kander/internal/liveness"
	"github.com/dualface/kander/internal/notify"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/terminal"
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

// paneExit is a dismiss target whose pre-send checks all passed: nothing has
// been sent to its pane yet. Checking and sending are separate steps so the
// caller can record its exit intent between them, after a check that may still
// fail without effect and before the first text reaches the pane.
type paneExit struct {
	backend terminal.Backend
	program string
	target  terminal.Address
	// agentAware selects the agent-identity delivery and exit wait.
	agentAware bool
}

// checkAgentPaneAt runs the pre-send checks of an agent-identity pane: the
// probe, the delivery target and the container. It sends nothing.
func checkAgentPaneAt(backend terminal.Backend, address terminal.Address, parsed bool, live liveness.TaskSession, allowReverse bool) (paneExit, error) {
	program, err := lookPath(backend.Executable())
	if err != nil {
		return paneExit{}, notInPath(backend)
	}
	var tabID, paneID string
	if parsed {
		tabID, paneID = address.Container, address.Pane
		probeResult := notify.AgentNotifyProbe(backend, program, paneID, live, probe.DefaultCommandTimeout)
		if probeResult.State == "stale" {
			if !allowReverse {
				return paneExit{}, remote.LocalExecutionRefused(remote.RefusalPaneStale, paneID, probeResult.Detail)
			}
			discovered, err := reverseLookupStale(probeResult.Detail, func() (terminal.Address, error) {
				return liveness.ReverseLookup(context.Background(), backend, program, live)
			})
			if err != nil {
				return paneExit{}, err
			}
			tabID, paneID, err = notify.ExplicitPaneTarget(backend, program, discovered.Pane)
			if err != nil {
				return paneExit{}, err
			}
		}
	} else {
		discovered, err := liveness.ReverseLookup(context.Background(), backend, program, live)
		if err != nil {
			return paneExit{}, err
		}
		tabID, paneID, err = notify.ExplicitPaneTarget(backend, program, discovered.Pane)
		if err != nil {
			return paneExit{}, err
		}
	}
	pane, err := notify.AgentNotifyTarget(backend, program, paneID, live)
	if err != nil {
		return paneExit{}, err
	}
	target := terminal.Address{Container: tabID, Pane: paneID}
	if err := validateAgentContainer(backend, program, target, pane); err != nil {
		return paneExit{}, err
	}
	return paneExit{backend: backend, program: program, target: target, agentAware: true}, nil
}

// checkProcessPaneAt runs the pre-send checks of a process-identified pane:
// the probe, the delivery target and the container. It sends nothing.
func checkProcessPaneAt(backend terminal.Backend, address terminal.Address, live liveness.TaskSession, allowReverse bool) (paneExit, error) {
	target := address
	program, err := lookPath(backend.Executable())
	if err != nil {
		return paneExit{}, notInPath(backend)
	}
	probeResult := notify.ProcessNotifyProbe(backend, program, target.Pane, live, probe.DefaultCommandTimeout)
	if probeResult.State == "stale" {
		if !allowReverse {
			return paneExit{}, remote.LocalExecutionRefused(remote.RefusalPaneStale, target.Pane, probeResult.Detail)
		}
		location, err := reverseLookupStale(probeResult.Detail, func() (terminal.Address, error) {
			return liveness.ReverseLookup(context.Background(), backend, program, live)
		})
		if err != nil {
			return paneExit{}, err
		}
		target.Pane = location.Pane
		topology, err := backend.Topology(context.Background(), probeConn(program), target)
		if err != nil {
			return paneExit{}, err
		}
		target.Session, target.Container = topology.Session, topology.Container
	}
	if err := notify.ProcessNotifyTarget(backend, program, target.Pane, live); err != nil {
		return paneExit{}, err
	}
	if err := validateProcessContainer(backend, program, target); err != nil {
		return paneExit{}, err
	}
	return paneExit{backend: backend, program: program, target: target}, nil
}

// send types the exit command into the checked pane, waits for the agent to
// exit and closes the container when it is still there. A failure from here on
// may follow a delivered exit command.
func (e paneExit) send(session launch.AgentSession, command string, timeout float64) error {
	var exists bool
	var err error
	if e.agentAware {
		if err = notify.AgentPrompt(e.backend, e.program, e.target.Pane, command); err != nil {
			return err
		}
		exists, err = agentWaitExit(e.backend, e.program, e.target, session, timeout)
	} else {
		if err = sendAgentExit(e.backend, e.program, e.target.Pane, command); err != nil {
			return err
		}
		exists, err = processWaitExit(e.backend, e.program, e.target, session, timeout)
	}
	if err != nil {
		return err
	}
	if exists {
		return closeContainer(e.backend, e.program, e.target)
	}
	return nil
}
