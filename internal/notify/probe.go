package notify

import (
	"context"
	"errors"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/liveness"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

// TargetProbe is the probe of a direct-delivery target: ready / busy / stale / fallback.
type TargetProbe struct {
	State  string
	Detail string
}

func probeConn(program string) terminal.Conn {
	return terminal.Conn{Program: program, Run: terminal.ProbeRunner}
}

func paneFactsWithin(backend terminal.Backend, program, paneID string, timeout time.Duration) (terminal.PaneFacts, error) {
	ctx, cancel := probe.TimeoutContext(timeout)
	defer cancel()
	return backend.PaneFacts(ctx, probeConn(program), paneID)
}

func agentPane(backend terminal.Backend, program, paneID string) (terminal.PaneFacts, error) {
	facts, err := paneFactsWithin(backend, program, paneID, 0)
	if err != nil {
		return terminal.PaneFacts{}, err
	}
	if facts.Gone {
		return terminal.PaneFacts{}, notifyError(
			"launch.pane_does_not_exist", paneID, facts.GoneDetail,
		)
	}
	return facts, nil
}

// AgentNotifyTarget validates that an existing pane of an agent-aware backend
// accepts direct delivery (idle/done + matching identity).
func AgentNotifyTarget(backend terminal.Backend, program, paneID string, session liveness.TaskSession) (terminal.PaneFacts, error) {
	pane, err := agentPane(backend, program, paneID)
	if err != nil {
		return terminal.PaneFacts{}, err
	}
	if pane.Agent != session.Agent {
		return terminal.PaneFacts{}, notifyError(
			"notify.agent_mismatch_task_pane", session.Agent, orNA(pane.Agent),
		)
	}
	if pane.AgentStatus != "idle" && pane.AgentStatus != "done" {
		return terminal.PaneFacts{}, notifyError(
			"notify.pane_status_does_not_accept_delivery", paneID, orNA(pane.AgentStatus),
		)
	}
	if pane.AgentSession != session.Reference {
		return terminal.PaneFacts{}, notifyError(
			"notify.session_mismatch_task_pane", session.Reference, orNA(pane.AgentSession),
		)
	}
	return pane, nil
}

// AgentNotifyProbe classifies an agent-aware target: stale / busy / ready. A
// mismatched identity is stale; only a busy status is retried.
func AgentNotifyProbe(backend terminal.Backend, program, paneID string, session liveness.TaskSession, timeout time.Duration) TargetProbe {
	pane, err := paneFactsWithin(backend, program, paneID, timeout)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return TargetProbe{State: "busy", Detail: t("notify.target_probe_timed_out")}
		}
		return TargetProbe{State: "stale", Detail: err.Error()}
	}
	if pane.Gone {
		return TargetProbe{State: "stale", Detail: t(
			"launch.pane_does_not_exist", paneID, pane.GoneDetail,
		)}
	}
	if pane.Agent != session.Agent {
		return TargetProbe{State: "stale", Detail: t(
			"notify.agent_mismatch_task_pane", session.Agent, orNA(pane.Agent),
		)}
	}
	if pane.AgentSession != session.Reference {
		return TargetProbe{State: "stale", Detail: t(
			"notify.session_mismatch_task_pane", session.Reference, orNA(pane.AgentSession),
		)}
	}
	if pane.AgentStatus != "idle" && pane.AgentStatus != "done" {
		return TargetProbe{State: "busy", Detail: t(
			"notify.pane_status_does_not_accept_delivery", paneID, orNA(pane.AgentStatus),
		)}
	}
	return TargetProbe{State: "ready"}
}

// ExplicitPaneTarget resolves the container of a --pane override, without
// stale-address reverse lookup.
func ExplicitPaneTarget(backend terminal.Backend, program, paneID string) (container, resolvedPane string, err error) {
	pane, err := agentPane(backend, program, paneID)
	if err != nil {
		return "", "", err
	}
	if pane.Container == "" {
		return "", "", notifyError(
			"notify.pane_has_no_usable_tab_id", paneID,
		)
	}
	return pane.Container, paneID, nil
}

func agentCommandName(agent string) (string, error) {
	definition, err := config.LoadAgent(agent)
	return definition.ProcessName, err
}

// ProcessNotifyTarget validates that an existing pane identified by its
// foreground process and session marker accepts direct delivery.
func ProcessNotifyTarget(backend terminal.Backend, program, paneID string, session liveness.TaskSession) error {
	facts, err := paneFactsWithin(backend, program, paneID, 0)
	if err != nil {
		return err
	}
	if facts.Gone {
		return notifyError(
			"launch.tmux_pane_does_not_exist", paneID, facts.GoneDetail,
		)
	}
	if facts.Dead != "0" {
		return notifyError("launch.tmux_pane_is_dead", paneID)
	}
	if facts.InMode != "0" {
		return notifyError("launch.tmux_pane_is_in_copy_mode", paneID)
	}
	expected, err := agentCommandName(session.Agent)
	if err != nil {
		return err
	}
	if facts.Command != expected {
		return notifyError(
			"launch.tmux_foreground_process_mismatch_expected_actual", expected, orNA(facts.Command),
		)
	}
	if facts.SessionMarker == "" {
		return notifyError("launch.tmux_pane_has_no_session_marker", paneID)
	}
	if facts.SessionMarker != session.Reference {
		return notifyError(
			"launch.tmux_session_mismatch_task_pane", session.Reference, facts.SessionMarker,
		)
	}
	return nil
}

// ProcessNotifyProbe classifies a process-identified target. A missing marker
// is fallback (no direct-delivery channel), a mismatched one is stale.
func ProcessNotifyProbe(backend terminal.Backend, program, paneID string, session liveness.TaskSession, timeout time.Duration) TargetProbe {
	facts, err := paneFactsWithin(backend, program, paneID, timeout)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return TargetProbe{State: "busy", Detail: t("notify.target_probe_timed_out")}
		}
		return TargetProbe{State: "stale", Detail: err.Error()}
	}
	if facts.Gone {
		return TargetProbe{State: "stale", Detail: t(
			"launch.tmux_pane_does_not_exist", paneID, facts.GoneDetail,
		)}
	}
	if facts.Dead != "0" {
		return TargetProbe{State: "stale", Detail: t("launch.tmux_pane_is_dead", paneID)}
	}
	expected, err := agentCommandName(session.Agent)
	if err != nil {
		return TargetProbe{State: "fallback", Detail: err.Error()}
	}
	if facts.Command != expected {
		return TargetProbe{State: "stale", Detail: t(
			"launch.tmux_foreground_process_mismatch_expected_actual", expected, orNA(facts.Command),
		)}
	}
	if facts.SessionMarker == "" {
		return TargetProbe{State: "fallback", Detail: t(
			"launch.tmux_pane_has_no_session_marker", paneID,
		)}
	}
	if facts.SessionMarker != session.Reference {
		return TargetProbe{State: "stale", Detail: t(
			"launch.tmux_session_mismatch_task_pane", session.Reference, facts.SessionMarker,
		)}
	}
	if facts.InMode != "0" {
		return TargetProbe{State: "busy", Detail: t(
			"launch.tmux_pane_is_in_copy_mode", paneID,
		)}
	}
	return TargetProbe{State: "ready"}
}

// notifyProbe dispatches to the probe matching the backend capabilities.
func notifyProbe(backend terminal.Backend, program, paneID string, session liveness.TaskSession, timeout time.Duration) TargetProbe {
	if backend.Capabilities().AgentIdentity {
		return AgentNotifyProbe(backend, program, paneID, session, timeout)
	}
	return ProcessNotifyProbe(backend, program, paneID, session, timeout)
}

func orNA(v string) string {
	if v == "" {
		return "N/A"
	}
	return v
}
