package takeover

import (
	"context"
	"strings"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/launch"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

// waitProbeError renders a failed pane probe while waiting for the agent to
// exit: a run failure keeps its original error, a response failure keeps the
// diagnostics of the wait loop.
func waitProbeError(err error) error {
	commandErr, ok := terminal.AsCommandError(err)
	if !ok {
		return err
	}
	switch commandErr.Kind {
	case terminal.KindExec:
		return commandErr.Cause
	case terminal.KindExit:
		return takeoverError("takeover.failed_to_probe_the_pane_while_waiting_for_the", commandErr.Detail())
	case terminal.KindNotJSON:
		return &probe.Error{Message: config.Text(commandErr.Cause.Error(), commandErr.Cause.Error())}
	case terminal.KindNotObject, terminal.KindMissingResult:
		return &probe.Error{Message: config.Text("probe.herdr_response_is_missing_result")}
	default:
		return takeoverError("takeover.herdr_pane_get_returned_an_invalid_response")
	}
}

func agentWaitExit(backend terminal.Backend, program string, address terminal.Address, session launch.AgentSession, timeout float64) (paneExists bool, err error) {
	tabID, paneID := address.Container, address.Pane
	deadline := nowFn().Add(time.Duration(timeout * float64(time.Second)))
	for {
		remaining := deadline.Sub(nowFn())
		if remaining <= 0 {
			return false, takeoverError("takeover.timed_out_waiting_for_the_agent_to_exit", paneID)
		}
		// Only the pane get command is bounded by the remaining budget: a
		// response that arrived in time is classified even if the budget
		// expires while it is parsed.
		budget := remaining
		conn := terminal.Conn{Program: program, Run: func(_ context.Context, name string, args []string) (probe.Result, error) {
			ctx, cancel := probe.TimeoutContext(budget)
			defer cancel()
			return probe.CaptureContext(ctx, name, args)
		}}
		pane, probeErr := backend.PaneFacts(context.Background(), conn, paneID)
		if probeErr != nil {
			return false, waitProbeError(probeErr)
		}
		if pane.Gone {
			topology, listErr := backend.Topology(context.Background(), probeConn(program), address)
			if listErr != nil {
				return false, listErr
			}
			if len(topology.Panes) > 0 {
				return false, takeoverError(
					"takeover.the_target_herdr_pane_disappeared_while_its_tab_still", tabID, strings.Join(topology.Panes, ","),
				)
			}
			return false, nil
		}
		if pane.Container != tabID {
			return false, takeoverError(
				"takeover.the_herdr_pane_moved_to_another_tab_while_waiting", tabID, orNA(pane.Container),
			)
		}
		if pane.Agent == "" {
			if err := validateAgentContainer(backend, program, address, pane); err != nil {
				return false, err
			}
			return true, nil
		}
		if pane.Agent != session.Agent {
			return false, takeoverError(
				"takeover.the_pane_changed_to_another_agent_while_waiting_for", session.Agent, pane.Agent,
			)
		}
		if session.Reference == "" || pane.AgentSession != session.Reference {
			return false, takeoverError(
				"takeover.the_pane_session_changed_while_waiting_for_exit_task", orNA(session.Reference), orNA(pane.AgentSession),
			)
		}
		remaining = deadline.Sub(nowFn())
		if remaining > 0 {
			d := pollInterval
			if remaining < d {
				d = remaining
			}
			sleepFn(d)
		}
	}
}

func processWaitExit(backend terminal.Backend, program string, address terminal.Address, session launch.AgentSession, timeout float64) (windowExists bool, err error) {
	paneID, windowID := address.Pane, address.Container
	deadline := nowFn().Add(time.Duration(timeout * float64(time.Second)))
	expected, err := agentCommandName(session.Agent)
	if err != nil {
		return false, err
	}
	for {
		remaining := deadline.Sub(nowFn())
		if remaining <= 0 {
			return false, takeoverError("takeover.timed_out_waiting_for_the_agent_to_exit", paneID)
		}
		facts, probeErr := paneFacts(backend, program, paneID, remaining)
		if probeErr != nil {
			return false, probeErr
		}
		if facts.Gone {
			exists, existsErr := backend.ContainerExists(context.Background(), probeConn(program), address)
			if existsErr != nil {
				return false, existsErr
			}
			if !exists {
				return false, nil
			}
			return false, takeoverError(
				"takeover.the_target_tmux_pane_disappeared_while_its_window_still", paneID, windowID,
			)
		}
		if facts.Dead == "1" {
			if err := validateProcessContainer(backend, program, address); err != nil {
				return false, err
			}
			return backend.ContainerExists(context.Background(), probeConn(program), address)
		}
		if facts.Dead != "0" {
			return false, takeoverError(
				"takeover.tmux_pane_returned_an_invalid_dead_state", orNA(facts.Dead),
			)
		}
		if facts.Command != expected {
			return false, takeoverError(
				"takeover.the_tmux_foreground_process_changed_while_waiting_for_exit", expected, orNA(facts.Command),
			)
		}
		if session.Reference == "" || facts.SessionMarker != session.Reference {
			return false, takeoverError(
				"takeover.the_tmux_pane_session_changed_while_waiting_for_exit", orNA(session.Reference), orNA(facts.SessionMarker),
			)
		}
		if err := validateProcessContainer(backend, program, address); err != nil {
			return false, err
		}
		remaining = deadline.Sub(nowFn())
		if remaining > 0 {
			d := pollInterval
			if remaining < d {
				d = remaining
			}
			sleepFn(d)
		}
	}
}
