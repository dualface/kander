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

// TargetProbe 是直投目标探查: ready / busy / stale / fallback.
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

// HerdrNotifyTarget 校验既有 herdr pane 可直投 (idle/done + 身份匹配).
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

// HerdrNotifyProbe 分类 herdr 目标: stale / busy / ready. 身份不匹配为过期, 状态忙才重试.
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

// HerdrExplicitTarget 解析 --pane 覆盖的 tab/pane, 不做过期反查.
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

// TmuxNotifyTarget 校验既有 tmux pane 可直投.
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

// TmuxNotifyProbe 分类 tmux 目标. 标记缺失为 fallback (无直投通道), 不一致为 stale.
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
