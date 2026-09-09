package liveness

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
)

func report(entry board.Entry, session *TaskSession, status, channel, container, detail, newWindow string) Report {
	agent := "N/A"
	if session != nil {
		agent = session.Agent
	}
	if container == "" {
		container = "N/A"
	}
	return Report{
		TaskID:       entry.TaskID,
		Agent:        agent,
		Status:       status,
		Channel:      channel,
		Container:    container,
		Detail:       strings.Join(strings.Fields(detail), " "),
		NewWindow:    newWindow,
		RuntimeState: Unknown,
	}
}

func herdrSessionValue(pane map[string]any) string {
	identity, _ := pane["agent_session"].(map[string]any)
	if identity == nil {
		return ""
	}
	value, _ := identity["value"].(string)
	return value
}

func staleReport(ctx context.Context, entry board.Entry, session TaskSession, channel, container, detail, program, launcher string, allowReverseLookup bool) Report {
	sess := session
	lookupFailure := func(err error) Report {
		status := Unknown
		var matchError *lookupMatchError
		if errors.As(err, &matchError) && matchError.matches == 0 {
			status = Stopped
		}
		return report(entry, &sess, status, channel, container,
			t("liveness.stale_address_reverse_lookup", detail, probe.FailureDetail(err)), "")
	}
	if !allowReverseLookup || session.Reference == "" {
		if session.Reference == "" {
			if definition, err := config.LoadAgent(session.Agent); err == nil && config.SessionResolvesEmptyReference(definition.Session.Mode) {
				detail += t("liveness.use_notify_directly_for_this_codex_task_the_command")
			}
		}
		return report(entry, &sess, Stopped, channel, container, detail, "")
	}
	var newWindow string
	runtimeState := Unknown
	if channel == "herdr" {
		tabID, paneID, err := HerdrReverseLookupContext(ctx, program, session)
		if err != nil {
			return lookupFailure(err)
		}
		newWindow = "herdr:" + tabID + ":" + paneID
		paneProbe, err := probe.ProbeHerdrPaneContext(ctx, program, paneID)
		if err != nil {
			return lookupFailure(&probe.Error{Message: t(
				"liveness.failed_to_probe_the_reverse_looked_up_pane", probe.FailureDetail(err))})
		}
		pane := paneProbe.Pane
		agent, _ := pane["agent"].(string)
		if pane == nil || agent != session.Agent {
			return report(entry, &sess, Stopped, channel, container, detail, "")
		}
		actualSession := herdrSessionValue(pane)
		if session.Reference != "" && actualSession != "" && actualSession != session.Reference {
			return report(entry, &sess, Stopped, channel, container, detail, "")
		}
		status, _ := pane["agent_status"].(string)
		runtimeState = status
		if status != "idle" && status != "working" && status != "blocked" && status != "done" {
			if status == "" {
				status = "N/A"
			}
			return lookupFailure(&probe.Error{Message: t(
				"liveness.the_reverse_looked_up_pane_agent_status_cannot_be", status)})
		}
	} else {
		location, err := TmuxReverseLookupContext(ctx, program, session)
		if err != nil {
			return lookupFailure(err)
		}
		newWindow = RenderTmuxWindow(launcher, location)
	}
	out := report(entry, &sess, Drifted, channel, container, detail, newWindow)
	out.RuntimeState = runtimeState
	return out
}

func probeTaskLiveness(ctx context.Context, entry board.Entry, text string, allowReverseLookup bool) Report {
	session := ParseTaskSession(text)
	window := board.MetadataFrom(text, board.FieldWindow)
	if session == nil {
		return report(entry, nil, Unknown, "unknown", window, t("liveness.missing_or_invalid_session_metadata"), "")
	}
	if window == "foreground" || window == "console" {
		return report(entry, session, Unknown, window, window, t("liveness.this_launcher_has_no_probeable_address"), "")
	}
	herdrMatch := herdrWindowRe.FindStringSubmatch(window)
	tmuxMatch := tmuxWindowRe.FindStringSubmatch(window)
	if herdrMatch == nil && tmuxMatch == nil {
		return report(entry, session, Unknown, "unknown", window, t("liveness.window_metadata_is_empty_or_invalid"), "")
	}
	if herdrMatch != nil {
		return classifyHerdr(ctx, entry, *session, herdrMatch[1], herdrMatch[2], allowReverseLookup)
	}
	return classifyTmux(ctx, entry, *session, tmuxMatch[1], tmuxMatch[2], tmuxMatch[3], tmuxMatch[4], allowReverseLookup)
}

func classifyHerdr(ctx context.Context, entry board.Entry, session TaskSession, tabID, paneID string, allowReverseLookup bool) Report {
	herdr, err := lookPath("herdr")
	if err != nil {
		return report(entry, &session, Unknown, "herdr", tabID, t("liveness.herdr_is_not_in_path"), "")
	}
	paneProbe, err := probe.ProbeHerdrPaneContext(ctx, herdr, paneID)
	if err != nil {
		return report(entry, &session, Unknown, "herdr", tabID, probe.FailureDetail(err), "")
	}
	if paneProbe.Pane == nil {
		return staleReport(ctx, entry, session, "herdr", tabID, t("launch.pane_does_not_exist_2", paneID), herdr, "herdr", allowReverseLookup)
	}
	pane := paneProbe.Pane
	actualAgent, _ := pane["agent"].(string)
	if actualAgent != session.Agent {
		actual := actualAgent
		if actual == "" {
			actual = "N/A"
		}
		return staleReport(ctx, entry, session, "herdr", tabID, t(
			"liveness.agent_mismatch_expected_actual", session.Agent, actual,
		), herdr, "herdr", allowReverseLookup)
	}
	actualSession := herdrSessionValue(pane)
	if session.Reference != "" && actualSession != "" && actualSession != session.Reference {
		return staleReport(ctx, entry, session, "herdr", tabID, t("liveness.session_identity_mismatch"), herdr, "herdr", allowReverseLookup)
	}
	status, _ := pane["agent_status"].(string)
	if status != "idle" && status != "working" && status != "blocked" && status != "done" {
		if status == "" {
			status = "N/A"
		}
		return report(entry, &session, Unknown, "herdr", tabID, t(
			"liveness.agent_status_cannot_be_classified", status,
		), "")
	}
	if session.Reference != "" && actualSession == "" {
		out := report(entry, &session, Alive, "herdr", tabID, t(
			"liveness.session_identity_was_not_reported_direct_delivery_is_unavailable",
		), "")
		out.RuntimeState = status
		return out
	}
	out := report(entry, &session, Alive, "herdr", tabID, t("liveness.agent_status", status), "")
	out.RuntimeState = status
	return out
}

func classifyTmux(ctx context.Context, entry board.Entry, session TaskSession, launcher, tmuxContainer, windowID, paneID string, allowReverseLookup bool) Report {
	container := tmuxContainer + ":" + windowID
	tmux, err := lookPath("tmux")
	if err != nil {
		return report(entry, &session, Unknown, launcher, container, t("liveness.tmux_is_not_in_path"), "")
	}
	paneProbe, err := probe.ProbeTmuxPaneContext(ctx, tmux, paneID)
	if err != nil {
		return report(entry, &session, Unknown, launcher, container, probe.FailureDetail(err), "")
	}
	if paneProbe.Facts == nil {
		return staleReport(ctx, entry, session, launcher, container, t("launch.pane_does_not_exist_2", paneID), tmux, launcher, allowReverseLookup)
	}
	facts := paneProbe.Facts
	expected, err := agentCommandName(session.Agent)
	if err != nil {
		return report(entry, &session, Unknown, "tmux", tmuxContainer, err.Error(), "")
	}
	if facts.Dead != "0" || facts.Command != expected {
		actual := facts.Command
		if actual == "" {
			actual = "N/A"
		}
		detail := t(
			"liveness.pane_is_dead_or_foreground_process_mismatches_expected_actual", expected, actual,
		)
		return staleReport(ctx, entry, session, launcher, container, detail, tmux, launcher, allowReverseLookup)
	}
	if facts.SessionMarker == "" {
		return report(entry, &session, Unknown, launcher, container, t("liveness.tmux_pane_has_no_session_marker"), "")
	}
	if session.Reference != "" && facts.SessionMarker != session.Reference {
		return staleReport(ctx, entry, session, launcher, container, t("liveness.tmux_session_marker_mismatch"), tmux, launcher, allowReverseLookup)
	}
	detail := t("liveness.agent_is_reachable")
	if facts.InMode != "0" {
		detail = t(
			"liveness.agent_is_alive_but_the_pane_is_in_copy",
		)
	}
	return report(entry, &session, Alive, launcher, container, detail, "")
}

func unknownFrom(entry board.Entry, text, detail string) Report {
	return report(entry, ParseTaskSession(text), Unknown, "unknown", board.MetadataFrom(text, board.FieldWindow), detail, "")
}

// ClassifyTask classifies one card; a failed probe collapses to unknown, and reverse lookup is allowed by default.
func ClassifyTask(entry board.Entry, text string) Report {
	return ClassifyTaskLookup(entry, text, true)
}

// ClassifyTaskLookup allows reverse lookup to be disabled. It never writes to the card.
func ClassifyTaskLookup(entry board.Entry, text string, allowReverseLookup bool) Report {
	return ClassifyTaskLookupContext(context.Background(), entry, text, allowReverseLookup)
}

// ClassifyTaskContext shares one deadline across forward lookup, reverse lookup,
// revalidation and process cleanup. It never writes to the card.
func ClassifyTaskContext(ctx context.Context, entry board.Entry, text string) Report {
	return ClassifyTaskLookupContext(ctx, entry, text, true)
}

// ClassifyTaskLookupContext additionally controls whether stale addresses are searched.
func ClassifyTaskLookupContext(ctx context.Context, entry board.Entry, text string, allowReverseLookup bool) (out Report) {
	ctx, cancel := probe.WithDefaultTimeout(ctx)
	defer cancel()
	defer func() {
		if rec := recover(); rec != nil {
			out = unknownFrom(entry, text, "panic")
		}
		out.Identity = identityFrom(entry, text)
		out.ObservedAt = time.Now().UTC()
		out.ObservationValid = out.Status != Unknown
	}()
	if err := ctx.Err(); err != nil {
		return unknownFrom(entry, text, probe.FailureDetail(err))
	}
	out = probeTaskLiveness(ctx, entry, text, allowReverseLookup)
	if err := ctx.Err(); err != nil && out.Status != Unknown {
		return unknownFrom(entry, text, probe.FailureDetail(err))
	}
	return out
}
