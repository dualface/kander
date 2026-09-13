package liveness

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
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

func staleReport(ctx context.Context, entry board.Entry, session TaskSession, channel, container, detail, program string, backend terminal.Backend, allowReverseLookup bool) Report {
	sess := session
	lookupFailure := func(err error) Report {
		status := Unknown
		var matchError *terminal.MatchError
		if errors.As(err, &matchError) && matchError.Matches == 0 {
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
	address, err := ReverseLookup(ctx, backend, program, session)
	if err != nil {
		return lookupFailure(err)
	}
	newWindow := terminal.FormatAddress(backend, address)
	runtimeState := Unknown
	if backend.Capabilities().AgentIdentity {
		// An agent-aware backend revalidates the found pane before reporting drift.
		pane, err := backend.PaneFacts(ctx, terminal.Conn{Program: program, Run: terminal.ProbeRunner}, address.Pane)
		if err != nil {
			return lookupFailure(&probe.Error{Message: t(
				"liveness.failed_to_probe_the_reverse_looked_up_pane", probe.FailureDetail(err))})
		}
		if pane.Gone || pane.Agent != session.Agent {
			return report(entry, &sess, Stopped, channel, container, detail, "")
		}
		if session.Reference != "" && pane.AgentSession != "" && pane.AgentSession != session.Reference {
			return report(entry, &sess, Stopped, channel, container, detail, "")
		}
		status := pane.AgentStatus
		runtimeState = status
		if status != "idle" && status != "working" && status != "blocked" && status != "done" {
			if status == "" {
				status = "N/A"
			}
			return lookupFailure(&probe.Error{Message: t(
				"liveness.the_reverse_looked_up_pane_agent_status_cannot_be", status)})
		}
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
	if terminal.HasCapability(window, func(c terminal.Capabilities) bool { return !c.Container }) {
		return report(entry, session, Unknown, window, window, t("liveness.this_launcher_has_no_probeable_address"), "")
	}
	backend, address, ok := terminal.ParseWindow(window)
	if !ok {
		return report(entry, session, Unknown, "unknown", window, t("liveness.window_metadata_is_empty_or_invalid"), "")
	}
	switch caps := backend.Capabilities(); {
	case caps.AgentIdentity:
		return classifyAgentPane(ctx, entry, *session, backend, address, allowReverseLookup)
	case caps.ForegroundProcess:
		return classifyProcessPane(ctx, entry, *session, backend, address, allowReverseLookup)
	default:
		// A container whose panes report neither identity has nothing to classify.
		return report(entry, session, Unknown, window, window, t("liveness.this_launcher_has_no_probeable_address"), "")
	}
}

// classifyAgentPane classifies a pane whose backend reports the agent, its
// status and session identity (herdr).
func classifyAgentPane(ctx context.Context, entry board.Entry, session TaskSession, backend terminal.Backend, address terminal.Address, allowReverseLookup bool) Report {
	channel, container, paneID := backend.Name(), address.Container, address.Pane
	program, err := lookPath(backend.Executable())
	if err != nil {
		return report(entry, &session, Unknown, channel, container, t("liveness.herdr_is_not_in_path"), "")
	}
	pane, err := backend.PaneFacts(ctx, terminal.Conn{Program: program, Run: terminal.ProbeRunner}, paneID)
	if err != nil {
		return report(entry, &session, Unknown, channel, container, probe.FailureDetail(err), "")
	}
	if pane.Gone {
		return staleReport(ctx, entry, session, channel, container, t("launch.pane_does_not_exist_2", paneID), program, backend, allowReverseLookup)
	}
	if pane.Agent != session.Agent {
		actual := pane.Agent
		if actual == "" {
			actual = "N/A"
		}
		return staleReport(ctx, entry, session, channel, container, t(
			"liveness.agent_mismatch_expected_actual", session.Agent, actual,
		), program, backend, allowReverseLookup)
	}
	actualSession := pane.AgentSession
	if session.Reference != "" && actualSession != "" && actualSession != session.Reference {
		return staleReport(ctx, entry, session, channel, container, t("liveness.session_identity_mismatch"), program, backend, allowReverseLookup)
	}
	status := pane.AgentStatus
	if status != "idle" && status != "working" && status != "blocked" && status != "done" {
		if status == "" {
			status = "N/A"
		}
		return report(entry, &session, Unknown, channel, container, t(
			"liveness.agent_status_cannot_be_classified", status,
		), "")
	}
	if session.Reference != "" && actualSession == "" {
		out := report(entry, &session, Alive, channel, container, t(
			"liveness.session_identity_was_not_reported_direct_delivery_is_unavailable",
		), "")
		out.RuntimeState = status
		return out
	}
	out := report(entry, &session, Alive, channel, container, t("liveness.agent_status", status), "")
	out.RuntimeState = status
	return out
}

// classifyProcessPane classifies a pane whose backend reports the foreground
// process and a session marker (tmux).
func classifyProcessPane(ctx context.Context, entry board.Entry, session TaskSession, backend terminal.Backend, address terminal.Address, allowReverseLookup bool) Report {
	launcher, paneID := backend.Name(), address.Pane
	container := address.Session + ":" + address.Container
	program, err := lookPath(backend.Executable())
	if err != nil {
		return report(entry, &session, Unknown, launcher, container, t("liveness.tmux_is_not_in_path"), "")
	}
	facts, err := backend.PaneFacts(ctx, terminal.Conn{Program: program, Run: terminal.ProbeRunner}, paneID)
	if err != nil {
		return report(entry, &session, Unknown, launcher, container, probe.FailureDetail(err), "")
	}
	if facts.Gone {
		return staleReport(ctx, entry, session, launcher, container, t("launch.pane_does_not_exist_2", paneID), program, backend, allowReverseLookup)
	}
	expected, err := agentCommandName(session.Agent)
	if err != nil {
		return report(entry, &session, Unknown, backend.Executable(), address.Session, err.Error(), "")
	}
	if facts.Dead != "0" || facts.Command != expected {
		actual := facts.Command
		if actual == "" {
			actual = "N/A"
		}
		detail := t(
			"liveness.pane_is_dead_or_foreground_process_mismatches_expected_actual", expected, actual,
		)
		return staleReport(ctx, entry, session, launcher, container, detail, program, backend, allowReverseLookup)
	}
	if facts.SessionMarker == "" {
		return report(entry, &session, Unknown, launcher, container, t("liveness.tmux_pane_has_no_session_marker"), "")
	}
	if session.Reference != "" && facts.SessionMarker != session.Reference {
		return staleReport(ctx, entry, session, launcher, container, t("liveness.tmux_session_marker_mismatch"), program, backend, allowReverseLookup)
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
