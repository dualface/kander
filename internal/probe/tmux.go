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

// TmuxPaneFacts 是存活的 tmux pane 事实.
type TmuxPaneFacts struct {
	Command       string
	InMode        string
	Dead          string
	SessionMarker string
}

// TmuxPaneProbe 是 tmux pane 探查结果: 事实或已消失.
type TmuxPaneProbe struct {
	Facts      *TmuxPaneFacts
	GoneDetail string
}

// TmuxContainerFacts 是 pane 所属 session/window 容器事实.
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

// ProbeTmuxPane 采集 tmux pane 命令/模式/dead 与会话标记. 双读 @kander_session 与 @onevoke_session.
func ProbeTmuxPane(tmux, paneID string) (TmuxPaneProbe, error) {
	return ProbeTmuxPaneWithin(tmux, paneID, 0)
}

// ProbeTmuxPaneWithin 在指定时限内采集 pane 事实; 非正值使用默认有界时限.
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

// TmuxPaneFactsOf 返回 pane 事实或 nil (已消失).
func TmuxPaneFactsOf(tmux, paneID string) (*TmuxPaneFacts, error) {
	probe, err := ProbeTmuxPane(tmux, paneID)
	if err != nil {
		return nil, err
	}
	return probe.Facts, nil
}

// ProbeTmuxContainer 采集 pane 所属容器坐标.
func ProbeTmuxContainer(tmux, paneID string) (TmuxContainerFacts, error) {
	return ProbeTmuxContainerWithin(tmux, paneID, 0)
}

// ProbeTmuxContainerWithin 在指定时限内采集 pane 容器坐标.
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
