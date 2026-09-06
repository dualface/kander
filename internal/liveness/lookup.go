package liveness

import (
	"path/filepath"
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
	value := board.MetadataFrom(text, "会话")
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
	if !contains(config.ExecutionAgents, agent) || len(parts) > 2 || (reference != "" && !sessionReferenceRe.MatchString(reference)) {
		return nil
	}
	return &TaskSession{Agent: agent, Reference: reference}
}

func agentCommandName(agent string) string {
	return filepath.Base(config.AgentExecutableName(agent))
}

func markersMatch(kander, onevoke, reference string) bool {
	if reference == "" {
		return false
	}
	return kander == reference || onevoke == reference
}

// HerdrReverseLookup uniquely locates a herdr pane by agent and session identity.
func HerdrReverseLookup(herdr string, session TaskSession) (tabID, paneID string, err error) {
	res, err := probe.Capture(herdr, []string{"pane", "list"}, 0)
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
		pane, _ := item.(map[string]any)
		if pane == nil {
			continue
		}
		identity, _ := pane["agent_session"].(map[string]any)
		var reference any
		if identity != nil {
			reference = identity["value"]
		}
		if pane["agent"] != session.Agent || reference != session.Reference {
			continue
		}
		tab := probe.PublicID(pane["tab_id"])
		id := probe.PublicID(pane["pane_id"])
		if tab != "" && id != "" {
			matches = append(matches, [2]string{tab, id})
		}
	}
	if len(matches) == 0 {
		return "", "", &probe.Error{Message: t(
			"liveness.herdr_session_lookup_found_no_match", session.Reference,
		)}
	}
	if len(matches) != 1 {
		return "", "", &probe.Error{Message: t(
			"liveness.herdr_session_lookup_is_ambiguous_panes", session.Reference, strconv.Itoa(len(matches)),
		)}
	}
	return matches[0][0], matches[0][1], nil
}

// TmuxReverseLookup uniquely locates a tmux pane by session marker, liveness and foreground process name. It reads both the kander and onevoke markers.
func TmuxReverseLookup(tmux string, session TaskSession) (TmuxPaneLocation, error) {
	res, err := probe.Capture(tmux, []string{
		"list-panes", "-a", "-F",
		"#{pane_id}\t#{session_id}\t#{session_name}\t#{window_id}\t#{pane_current_command}\t#{pane_dead}\t#{@kander_session}\t#{@onevoke_session}",
	}, 0)
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
	expected := agentCommandName(session.Agent)
	var matches []TmuxPaneLocation
	for _, line := range strings.Split(res.Stdout, "\n") {
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
		if markersMatch(kander, onevoke, session.Reference) && dead == "0" && command == expected {
			matches = append(matches, TmuxPaneLocation{SessionID: sessionID, SessionName: sessionName, WindowID: windowID, PaneID: paneID})
		}
	}
	if len(matches) == 0 {
		return TmuxPaneLocation{}, &probe.Error{Message: t(
			"liveness.tmux_session_lookup_found_no_match_0_panes", session.Reference,
		)}
	}
	if len(matches) != 1 {
		return TmuxPaneLocation{}, &probe.Error{Message: t(
			"liveness.tmux_session_lookup_is_ambiguous_panes", session.Reference, strconv.Itoa(len(matches)),
		)}
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
