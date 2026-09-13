package notify

import (
	"context"
	"os/exec"
	"time"

	"github.com/dualface/kander/internal/liveness"
	"github.com/dualface/kander/internal/terminal"
)

var (
	lookPath = exec.LookPath
	sleepFn  = time.Sleep
	nowFn    = time.Now
)

// DirectTarget is a fully validated direct-delivery address that may receive a payload.
type DirectTarget struct {
	Backend terminal.Backend
	Program string
	PaneID  string
	Window  string
	Timeout float64
}

func waitForTarget(probeFn func(time.Duration) TargetProbe, timeout float64) (TargetProbe, float64, error) {
	deadline := nowFn().Add(time.Duration(timeout * float64(time.Second)))
	last := TargetProbe{State: "busy", Detail: t("notify.target_probe_timed_out")}
	for {
		remainingDuration := deadline.Sub(nowFn())
		if remainingDuration <= 0 {
			return last, 0, &BusyError{Message: t(
				"notify.target_agent_is_busy_nothing_was_delivered", last.Detail,
			)}
		}
		result := probeFn(remainingDuration)
		last = result
		if result.State != "busy" {
			remaining := deadline.Sub(nowFn()).Seconds()
			if remaining < 0 {
				remaining = 0
			}
			return result, remaining, nil
		}
		remaining := deadline.Sub(nowFn())
		if remaining <= 0 {
			return result, 0, &BusyError{Message: t(
				"notify.target_agent_is_busy_nothing_was_delivered", result.Detail,
			)}
		}
		d := pollInterval
		if remaining < d {
			d = remaining
		}
		sleepFn(d)
	}
}

func requireReady(probe TargetProbe) error {
	if probe.State != "ready" {
		return &Error{Message: probe.Detail}
	}
	return nil
}

func staleLookup(detail string, lookup func() (DirectTarget, error)) (DirectTarget, error) {
	target, err := lookup()
	if err != nil {
		if isBusy(err) {
			return DirectTarget{}, err
		}
		return DirectTarget{}, notifyError(
			"notify.stale_address_lookup", detail, err.Error(),
		)
	}
	return target, nil
}

func notInPath(backend terminal.Backend) error {
	if backend.Capabilities().AgentIdentity {
		return notifyError("liveness.herdr_is_not_in_path")
	}
	return notifyError("liveness.tmux_is_not_in_path")
}

// agentBackend is the backend that can locate a session without a recorded
// address: it reports agent identity, so a pane override or a reverse lookup
// needs no launcher-specific coordinates.
func agentBackend() (terminal.Backend, bool) {
	return terminal.FindBackend(func(c terminal.Capabilities) bool { return c.AgentIdentity })
}

// ResolveTarget resolves and fully validates the direct-delivery target before any payload is created.
func ResolveTarget(window, paneOverride string, session liveness.TaskSession, timeout float64) (DirectTarget, error) {
	backend, address, parsed := terminal.ParseWindow(window)
	if paneOverride != "" {
		agent, ok := agentBackend()
		if !ok {
			return DirectTarget{}, notifyError("liveness.herdr_is_not_in_path")
		}
		program, err := lookPath(agent.Executable())
		if err != nil {
			return DirectTarget{}, notInPath(agent)
		}
		container, paneID, err := ExplicitPaneTarget(agent, program, paneOverride)
		if err != nil {
			return DirectTarget{}, err
		}
		probe, remaining, err := waitForTarget(func(remaining time.Duration) TargetProbe {
			return AgentNotifyProbe(agent, program, paneID, session, remaining)
		}, timeout)
		if err != nil {
			return DirectTarget{}, err
		}
		if err := requireReady(probe); err != nil {
			return DirectTarget{}, err
		}
		window := terminal.FormatAddress(agent, terminal.Address{Container: container, Pane: paneID})
		return DirectTarget{Backend: agent, Program: program, PaneID: paneID, Window: window, Timeout: remaining}, nil
	}
	if parsed {
		paneID := address.Pane
		program, err := lookPath(backend.Executable())
		if err != nil {
			return DirectTarget{}, notInPath(backend)
		}
		probe, remaining, err := waitForTarget(func(remaining time.Duration) TargetProbe {
			return notifyProbe(backend, program, paneID, session, remaining)
		}, timeout)
		if err != nil {
			return DirectTarget{}, err
		}
		if probe.State != "stale" {
			if err := requireReady(probe); err != nil {
				return DirectTarget{}, err
			}
			return DirectTarget{Backend: backend, Program: program, PaneID: paneID, Timeout: remaining}, nil
		}
		return staleLookup(probe.Detail, func() (DirectTarget, error) {
			discovered, err := liveness.ReverseLookup(context.Background(), backend, program, session)
			if err != nil {
				return DirectTarget{}, err
			}
			found, finalTimeout, err := waitForTarget(func(remaining time.Duration) TargetProbe {
				return notifyProbe(backend, program, discovered.Pane, session, remaining)
			}, remaining)
			if err != nil {
				return DirectTarget{}, err
			}
			if err := requireReady(found); err != nil {
				return DirectTarget{}, err
			}
			return DirectTarget{
				Backend: backend, Program: program, PaneID: discovered.Pane,
				Window: terminal.FormatAddress(backend, discovered), Timeout: finalTimeout,
			}, nil
		})
	}
	if window == "" {
		agent, ok := agentBackend()
		if !ok {
			return DirectTarget{}, notifyError("notify.herdr_is_not_in_path_cannot_look_up_a")
		}
		program, err := lookPath(agent.Executable())
		if err != nil {
			return DirectTarget{}, notifyError(
				"notify.herdr_is_not_in_path_cannot_look_up_a",
			)
		}
		discovered, err := liveness.ReverseLookup(context.Background(), agent, program, session)
		if err != nil {
			return DirectTarget{}, err
		}
		probe, remaining, err := waitForTarget(func(remaining time.Duration) TargetProbe {
			return AgentNotifyProbe(agent, program, discovered.Pane, session, remaining)
		}, timeout)
		if err != nil {
			return DirectTarget{}, err
		}
		if err := requireReady(probe); err != nil {
			return DirectTarget{}, err
		}
		return DirectTarget{
			Backend: agent, Program: program, PaneID: discovered.Pane,
			Window: terminal.FormatAddress(agent, discovered), Timeout: remaining,
		}, nil
	}
	return DirectTarget{}, notifyError(
		"notify.no_direct_notification_address", orNA(window),
	)
}
