package probe

import (
	"context"
	"strconv"
	"strings"
	"time"
)

const (
	PaneSessionOption = "@kander_session"
	LegacyPaneSession = "@onevoke_session"
)

var tmuxGoneMarkers = []string{"can't find", "no server running", "no sessions"}

// TmuxPaneFacts are the facts of a live tmux pane.
type TmuxPaneFacts struct {
	Command       string
	InMode        string
	Dead          string
	SessionMarker string
}

// TmuxPaneProbe is the result of probing a tmux pane: the facts, or gone.
type TmuxPaneProbe struct {
	Facts      *TmuxPaneFacts
	GoneDetail string
}

// TmuxContainerFacts are the facts of the session/window container owning the pane.
type TmuxContainerFacts struct {
	SessionID   string
	SessionName string
	WindowID    string
	PaneCount   string
}

func tmuxGone(detail string) bool {
	lowered := strings.ToLower(detail)
	for _, marker := range tmuxGoneMarkers {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}

func optionMissing(detail, option string) bool {
	lowered := strings.ToLower(strings.TrimSpace(detail))
	if lowered == "" {
		return true
	}
	return lowered == "invalid option: "+option || lowered == "unknown option: "+option
}

func showPaneOption(tmux, paneID, option string, timeout time.Duration) (value string, gone string, missing bool, err error) {
	res, runErr := runCommand(tmux, []string{"show-options", "-p", "-v", "-t", paneID, option}, timeout)
	if runErr != nil {
		return "", "", false, runErr
	}
	detail := strings.TrimSpace(res.Stderr)
	if res.Code != 0 {
		if tmuxGone(detail) {
			return "", detail, false, nil
		}
		if optionMissing(detail, option) {
			return "", "", true, nil
		}
		return "", "", false, probeError(
			"probe.failed_to_probe_the_tmux_pane", orExit(detail, res.Code),
		)
	}
	return strings.TrimSpace(res.Stdout), "", false, nil
}

func orExit(detail string, code int) string {
	if detail != "" {
		return detail
	}
	return "exit " + strconv.Itoa(code)
}

// ProbeTmuxPane collects the command/mode/dead state of a tmux pane plus its session marker. It reads both @kander_session and @onevoke_session.
func ProbeTmuxPane(tmux, paneID string) (TmuxPaneProbe, error) {
	return ProbeTmuxPaneWithin(tmux, paneID, 0)
}

// ProbeTmuxPaneWithin collects the pane facts within the given deadline; a non-positive value uses the default bounded deadline.
func ProbeTmuxPaneWithin(tmux, paneID string, timeout time.Duration) (TmuxPaneProbe, error) {
	if timeout <= 0 {
		timeout = DefaultCommandTimeout
	}
	deadline := time.Now().Add(timeout)
	res, err := runCommand(tmux, []string{
		"display-message", "-p", "-t", paneID,
		"#{pane_current_command}\t#{pane_in_mode}\t#{pane_dead}",
	}, timeout)
	if err != nil {
		return TmuxPaneProbe{}, err
	}
	detail := strings.TrimSpace(res.Stderr)
	if res.Code != 0 {
		if tmuxGone(detail) {
			return TmuxPaneProbe{GoneDetail: detail}, nil
		}
		return TmuxPaneProbe{}, probeError(
			"launch.tmux_pane_does_not_exist", paneID, orExit(detail, res.Code),
		)
	}
	fields := strings.Split(strings.TrimSpace(res.Stdout), "\t")
	if len(fields) != 3 {
		return TmuxPaneProbe{}, probeError("launch.tmux_pane_probe_returned_an_invalid_response")
	}
	marker, gone, err := readSessionMarker(tmux, paneID, deadline)
	if err != nil {
		return TmuxPaneProbe{}, err
	}
	if gone != "" {
		return TmuxPaneProbe{GoneDetail: gone}, nil
	}
	facts := TmuxPaneFacts{Command: fields[0], InMode: fields[1], Dead: fields[2], SessionMarker: marker}
	return TmuxPaneProbe{Facts: &facts}, nil
}

func readSessionMarker(tmux, paneID string, deadline time.Time) (string, string, error) {
	for _, option := range []string{PaneSessionOption, LegacyPaneSession} {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return "", "", context.DeadlineExceeded
		}
		value, gone, missing, err := showPaneOption(tmux, paneID, option, remaining)
		if err != nil {
			return "", "", err
		}
		if gone != "" {
			return "", gone, nil
		}
		if missing {
			continue
		}
		return value, "", nil
	}
	return "", "", nil
}

// TmuxPaneFactsOf returns the pane facts, or nil when the pane is gone.
func TmuxPaneFactsOf(tmux, paneID string) (*TmuxPaneFacts, error) {
	probe, err := ProbeTmuxPane(tmux, paneID)
	if err != nil {
		return nil, err
	}
	return probe.Facts, nil
}

// ProbeTmuxContainer collects the container coordinates owning the pane.
func ProbeTmuxContainer(tmux, paneID string) (TmuxContainerFacts, error) {
	return ProbeTmuxContainerWithin(tmux, paneID, 0)
}

// ProbeTmuxContainerWithin collects the pane container coordinates within the given deadline.
func ProbeTmuxContainerWithin(tmux, paneID string, timeout time.Duration) (TmuxContainerFacts, error) {
	res, err := runCommand(tmux, []string{
		"display-message", "-p", "-t", paneID,
		"#{session_id}\t#{session_name}\t#{window_id}\t#{window_panes}",
	}, timeout)
	if err != nil {
		return TmuxContainerFacts{}, err
	}
	if res.Code != 0 {
		detail := orExit(strings.TrimSpace(res.Stderr), res.Code)
		return TmuxContainerFacts{}, probeError(
			"probe.failed_to_probe_the_tmux_pane_container", detail,
		)
	}
	fields := strings.Split(strings.TrimSpace(res.Stdout), "\t")
	if len(fields) != 4 {
		return TmuxContainerFacts{}, probeError(
			"probe.tmux_pane_container_probe_returned_an_invalid_response",
		)
	}
	return TmuxContainerFacts{
		SessionID:   fields[0],
		SessionName: fields[1],
		WindowID:    fields[2],
		PaneCount:   fields[3],
	}, nil
}
