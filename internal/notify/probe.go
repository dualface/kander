package notify

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/liveness"
	"github.com/dualface/kander/internal/probe"
)

// TargetProbe is the probe of a direct-delivery target: ready / busy / stale / fallback.
type TargetProbe struct {
	State  string
	Detail string
	Pane   map[string]any
}

func herdrSessionReference(pane map[string]any) string {
	identity, _ := pane["agent_session"].(map[string]any)
	if identity == nil {
		return ""
	}
	ref, _ := identity["value"].(string)
	if ref == "" {
		return ""
	}
	return ref
}

func herdrPane(herdr, paneID string) (map[string]any, error) {
	probeResult, err := probe.ProbeHerdrPane(herdr, paneID, 0)
	if err != nil {
		return nil, err
	}
	if probeResult.Pane == nil {
		return nil, notifyError(
			"launch.pane_does_not_exist", paneID, probeResult.GoneDetail,
		)
	}
	return probeResult.Pane, nil
}

// HerdrNotifyTarget validates that an existing herdr pane accepts direct delivery (idle/done + matching identity).
func HerdrNotifyTarget(herdr, paneID string, session liveness.TaskSession) (map[string]any, error) {
	pane, err := herdrPane(herdr, paneID)
	if err != nil {
		return nil, err
	}
	actualAgent, _ := pane["agent"].(string)
	if actualAgent != session.Agent {
		return nil, notifyError(
			"notify.agent_mismatch_task_pane", session.Agent, orNA(actualAgent),
		)
	}
	status, _ := pane["agent_status"].(string)
	if status != "idle" && status != "done" {
		return nil, notifyError(
			"notify.pane_status_does_not_accept_delivery", paneID, orNA(status),
		)
	}
	actualSession := herdrSessionReference(pane)
	if actualSession != session.Reference {
		return nil, notifyError(
			"notify.session_mismatch_task_pane", session.Reference, orNA(actualSession),
		)
	}
	return pane, nil
}

// HerdrNotifyProbe classifies a herdr target: stale / busy / ready. A mismatched identity is stale; only a busy status is retried.
func HerdrNotifyProbe(herdr, paneID string, session liveness.TaskSession, timeout time.Duration) TargetProbe {
	paneProbe, err := probe.ProbeHerdrPane(herdr, paneID, timeout)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return TargetProbe{State: "busy", Detail: t("notify.target_probe_timed_out")}
		}
		return TargetProbe{State: "stale", Detail: err.Error()}
	}
	if paneProbe.Pane == nil {
		return TargetProbe{State: "stale", Detail: t(
			"launch.pane_does_not_exist", paneID, paneProbe.GoneDetail,
		)}
	}
	pane := paneProbe.Pane
	actualAgent, _ := pane["agent"].(string)
	if actualAgent != session.Agent {
		return TargetProbe{State: "stale", Detail: t(
			"notify.agent_mismatch_task_pane", session.Agent, orNA(actualAgent),
		)}
	}
	actualSession := herdrSessionReference(pane)
	if actualSession != session.Reference {
		return TargetProbe{State: "stale", Detail: t(
			"notify.session_mismatch_task_pane", session.Reference, orNA(actualSession),
		)}
	}
	status, _ := pane["agent_status"].(string)
	if status != "idle" && status != "done" {
		return TargetProbe{State: "busy", Detail: t(
			"notify.pane_status_does_not_accept_delivery", paneID, orNA(status),
		), Pane: pane}
	}
	return TargetProbe{State: "ready", Pane: pane}
}

// HerdrExplicitTarget resolves the tab/pane of a --pane override, without stale-address reverse lookup.
func HerdrExplicitTarget(herdr, paneID string) (tabID, resolvedPane string, err error) {
	pane, err := herdrPane(herdr, paneID)
	if err != nil {
		return "", "", err
	}
	tabID = probe.PublicID(pane["tab_id"])
	if tabID == "" {
		return "", "", notifyError(
			"notify.pane_has_no_usable_tab_id", paneID,
		)
	}
	return tabID, paneID, nil
}

func agentCommandName(agent string) string {
	return filepath.Base(config.AgentExecutableName(agent))
}

// TmuxNotifyTarget validates that an existing tmux pane accepts direct delivery.
func TmuxNotifyTarget(tmux, paneID string, session liveness.TaskSession) error {
	paneProbe, err := probe.ProbeTmuxPane(tmux, paneID)
	if err != nil {
		return err
	}
	if paneProbe.Facts == nil {
		return notifyError(
			"launch.tmux_pane_does_not_exist", paneID, paneProbe.GoneDetail,
		)
	}
	facts := paneProbe.Facts
	if facts.Dead != "0" {
		return notifyError("launch.tmux_pane_is_dead", paneID)
	}
	if facts.InMode != "0" {
		return notifyError("launch.tmux_pane_is_in_copy_mode", paneID)
	}
	expected := agentCommandName(session.Agent)
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

// TmuxNotifyProbe classifies a tmux target. A missing marker is fallback (no direct-delivery channel), a mismatched one is stale.
func TmuxNotifyProbe(tmux, paneID string, session liveness.TaskSession, timeout time.Duration) TargetProbe {
	paneProbe, err := probe.ProbeTmuxPaneWithin(tmux, paneID, timeout)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return TargetProbe{State: "busy", Detail: t("notify.target_probe_timed_out")}
		}
		return TargetProbe{State: "stale", Detail: err.Error()}
	}
	if paneProbe.Facts == nil {
		return TargetProbe{State: "stale", Detail: t(
			"launch.tmux_pane_does_not_exist", paneID, paneProbe.GoneDetail,
		)}
	}
	facts := paneProbe.Facts
	if facts.Dead != "0" {
		return TargetProbe{State: "stale", Detail: t("launch.tmux_pane_is_dead", paneID)}
	}
	expected := agentCommandName(session.Agent)
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

func orNA(v string) string {
	if v == "" {
		return "N/A"
	}
	return v
}
