package liveness

import (
	"context"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
)

func taskGroupFrom(text string) string {
	return board.TaskGroupFrom(text)
}

// ParseTaskSession parses the session field of a card; it returns nil when the field cannot be parsed.
func ParseTaskSession(text string) *TaskSession {
	value := board.MetadataFrom(text, board.FieldSession)
	if value == "" {
		return nil
	}
	parts := strings.Fields(value)
	if len(parts) == 0 {
		return nil
	}
	agent := parts[0]
	reference := ""
	if len(parts) > 1 {
		reference = parts[1]
	}
	if !config.ValidAgentName(agent) || len(parts) > 2 || (reference != "" && !sessionReferenceRe.MatchString(reference)) {
		return nil
	}
	return &TaskSession{Agent: agent, Reference: reference}
}

func agentCommandName(agent string) (string, error) {
	definition, err := config.LoadAgent(agent)
	return definition.ProcessName, err
}

func markersMatch(kander, onevoke, reference string) bool {
	if reference == "" {
		return false
	}
	return kander == reference || onevoke == reference
}

// lookupMatchError distinguishes a completed search from a collection failure.
// Existing delivery callers still reject every non-unique result as an error.
type lookupMatchError struct {
	matches int
	cause   error
}

func (e *lookupMatchError) Error() string { return e.cause.Error() }
func (e *lookupMatchError) Unwrap() error { return e.cause }

// HerdrReverseLookup uniquely locates a herdr pane by agent and session identity.
func HerdrReverseLookup(herdr string, session TaskSession) (tabID, paneID string, err error) {
	return HerdrReverseLookupContext(context.Background(), herdr, session)
}

// HerdrReverseLookupContext locates a session within the caller's shared budget.
func HerdrReverseLookupContext(ctx context.Context, herdr string, session TaskSession) (tabID, paneID string, err error) {
	ctx, cancel := probe.WithDefaultTimeout(ctx)
	defer cancel()
	res, err := probe.CaptureContext(ctx, herdr, []string{"pane", "list"})
	if err != nil {
		return "", "", err
	}
	data, err := probe.HerdrResult(res)
	if err != nil {
		return "", "", err
	}
	panes, _ := data["panes"].([]any)
	if panes == nil {
		return "", "", &probe.Error{Message: t("liveness.herdr_pane_list_response_has_no_panes")}
	}
	var matches [][2]string
	for _, item := range panes {
		if err := ctx.Err(); err != nil {
			return "", "", err
		}
		pane, _ := item.(map[string]any)
		if pane == nil {
			return "", "", &probe.Error{Message: t("liveness.herdr_session_lookup_returned_invalid_output")}
		}
		tab := probe.PublicID(pane["tab_id"])
		id := probe.PublicID(pane["pane_id"])
		agent, agentOK := pane["agent"].(string)
		if tab == "" || id == "" || !herdrWindowRe.MatchString("herdr:"+tab+":"+id) || (pane["agent"] != nil && !agentOK) {
			return "", "", &probe.Error{Message: t("liveness.herdr_session_lookup_returned_invalid_output")}
		}
		identity, _ := pane["agent_session"].(map[string]any)
		if pane["agent_session"] != nil && identity == nil {
			return "", "", &probe.Error{Message: t("liveness.herdr_session_lookup_returned_invalid_output")}
		}
		var reference string
		if identity != nil {
			var ok bool
			reference, ok = identity["value"].(string)
			if identity["value"] != nil && !ok {
				return "", "", &probe.Error{Message: t("liveness.herdr_session_lookup_returned_invalid_output")}
			}
		}
		if agent != session.Agent || reference != session.Reference {
			continue
		}
		matches = append(matches, [2]string{tab, id})
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if len(matches) == 0 {
		return "", "", &lookupMatchError{matches: 0, cause: &probe.Error{Message: t(
			"liveness.herdr_session_lookup_found_no_match", session.Reference,
		)}}
	}
	if len(matches) != 1 {
		return "", "", &lookupMatchError{matches: len(matches), cause: &probe.Error{Message: t(
			"liveness.herdr_session_lookup_is_ambiguous_panes", session.Reference, strconv.Itoa(len(matches)),
		)}}
	}
	return matches[0][0], matches[0][1], nil
}

// TmuxReverseLookup uniquely locates a tmux pane by session marker, liveness and foreground process name. It reads both the kander and onevoke markers.
func TmuxReverseLookup(tmux string, session TaskSession) (TmuxPaneLocation, error) {
	return TmuxReverseLookupContext(context.Background(), tmux, session)
}

// TmuxReverseLookupContext locates a session within the caller's shared budget.
func TmuxReverseLookupContext(ctx context.Context, tmux string, session TaskSession) (TmuxPaneLocation, error) {
	ctx, cancel := probe.WithDefaultTimeout(ctx)
	defer cancel()
	res, err := probe.CaptureContext(ctx, tmux, []string{
		"list-panes", "-a", "-F",
		"#{pane_id}\t#{session_id}\t#{session_name}\t#{window_id}\t#{pane_current_command}\t#{pane_dead}\t#{@kander_session}\t#{@onevoke_session}",
	})
	if err != nil {
		return TmuxPaneLocation{}, err
	}
	if res.Code != 0 || strings.TrimSpace(res.Stdout) == "" {
		detail := strings.TrimSpace(res.Stderr)
		if detail == "" {
			detail = t("liveness.empty_output")
		}
		return TmuxPaneLocation{}, &probe.Error{Message: detail}
	}
	expected, err := agentCommandName(session.Agent)
	if err != nil {
		return TmuxPaneLocation{}, err
	}
	var matches []TmuxPaneLocation
	for _, line := range strings.Split(res.Stdout, "\n") {
		if err := ctx.Err(); err != nil {
			return TmuxPaneLocation{}, err
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		var paneID, sessionID, sessionName, windowID, command, dead, kander, onevoke string
		switch len(fields) {
		case 7:
			paneID, sessionID, sessionName, windowID, command, dead, onevoke = fields[0], fields[1], fields[2], fields[3], fields[4], fields[5], fields[6]
		case 8:
			paneID, sessionID, sessionName, windowID, command, dead, kander, onevoke = fields[0], fields[1], fields[2], fields[3], fields[4], fields[5], fields[6], fields[7]
		default:
			return TmuxPaneLocation{}, &probe.Error{Message: t("liveness.tmux_session_lookup_returned_invalid_output")}
		}
		if !tmuxWindowRe.MatchString("tmux:"+sessionID+":"+windowID+":"+paneID) ||
			sessionName == "" ||
			command == "" || (dead != "0" && dead != "1") || strings.ContainsRune(line, 0) {
			return TmuxPaneLocation{}, &probe.Error{Message: t("liveness.tmux_session_lookup_returned_invalid_output")}
		}
		if markersMatch(kander, onevoke, session.Reference) && dead == "0" && command == expected {
			matches = append(matches, TmuxPaneLocation{SessionID: sessionID, SessionName: sessionName, WindowID: windowID, PaneID: paneID})
		}
	}
	if err := ctx.Err(); err != nil {
		return TmuxPaneLocation{}, err
	}
	if len(matches) == 0 {
		return TmuxPaneLocation{}, &lookupMatchError{matches: 0, cause: &probe.Error{Message: t(
			"liveness.tmux_session_lookup_found_no_match_0_panes", session.Reference,
		)}}
	}
	if len(matches) != 1 {
		return TmuxPaneLocation{}, &lookupMatchError{matches: len(matches), cause: &probe.Error{Message: t(
			"liveness.tmux_session_lookup_is_ambiguous_panes", session.Reference, strconv.Itoa(len(matches)),
		)}}
	}
	return matches[0], nil
}

// RenderTmuxWindow formats reverse-lookup coordinates as the window address stored on a card.
func RenderTmuxWindow(launcher string, loc TmuxPaneLocation) string {
	container := loc.SessionID
	if launcher == "tmux-session" {
		container = loc.SessionName
	}
	return launcher + ":" + container + ":" + loc.WindowID + ":" + loc.PaneID
}
