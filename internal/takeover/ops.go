package takeover

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/launch"
	"github.com/dualface/kander/internal/liveness"
	"github.com/dualface/kander/internal/notify"
	"github.com/dualface/kander/internal/probe"
)

const pollInterval = 100 * time.Millisecond

var (
	lookPath = exec.LookPath
	sleepFn  = time.Sleep
	nowFn    = time.Now
)

func t(id string, args ...any) string {
	return config.Text(id, args...)
}

func takeoverError(id string, args ...any) error {
	return &notify.Error{Message: config.Text(id, args...)}
}

var agentExitCommands = map[string]string{
	"claude": "/exit",
	"codex":  "/exit",
	"grok":   "/quit",
	"cursor": "/quit",
}

// AgentExitCommand returns /exit for Claude/Codex or /quit for Grok/Cursor.
func AgentExitCommand(agent string) (string, error) {
	cmd, ok := agentExitCommands[agent]
	if !ok {
		return "", takeoverError("launch.unsupported_agent", agent)
	}
	return cmd, nil
}

func herdrFailureDetail(res probe.Result) string {
	if s := strings.TrimSpace(res.Stderr); s != "" {
		return s
	}
	return fmt.Sprintf("exit %d", res.Code)
}

func herdrCloseTab(herdr, tabID string) error {
	res, err := probe.Capture(herdr, []string{"tab", "close", tabID}, 0)
	if err != nil {
		return takeoverError("launch.failed_to_close_tab", tabID, err.Error())
	}
	if res.Code != 0 {
		return takeoverError("launch.failed_to_close_tab", tabID, herdrFailureDetail(res))
	}
	return nil
}

func tmuxCloseWindow(tmux, windowID string) error {
	if windowID == "" {
		return nil
	}
	res, err := probe.Capture(tmux, []string{"kill-window", "-t", windowID}, 0)
	if err != nil {
		return takeoverError("launch.failed_to_close_tmux_window", err.Error())
	}
	if res.Code == 0 {
		return nil
	}
	detail := strings.TrimSpace(res.Stderr)
	if detail == "" {
		detail = fmt.Sprintf("exit %d", res.Code)
	}
	return takeoverError("launch.failed_to_close_tmux_window", detail)
}

func tmuxSendAgentExit(tmux, paneID, command string) error {
	for _, args := range [][]string{
		{"send-keys", "-t", paneID, "-l", command},
		{"send-keys", "-t", paneID, "Enter"},
	} {
		res, err := probe.Capture(tmux, args, 0)
		if err != nil {
			return takeoverError("takeover.tmux_failed_to_deliver_the_agent_exit_command", err.Error())
		}
		if res.Code != 0 {
			detail := strings.TrimSpace(res.Stderr)
			if detail == "" {
				detail = fmt.Sprintf("exit %d", res.Code)
			}
			return takeoverError("takeover.tmux_failed_to_deliver_the_agent_exit_command", detail)
		}
	}
	return nil
}

func herdrTabPanes(herdr, tabID string) ([]string, error) {
	res, err := probe.Capture(herdr, []string{"pane", "list"}, 0)
	if err != nil {
		return nil, err
	}
	data, err := probe.HerdrResult(res)
	if err != nil {
		return nil, takeoverError("takeover.herdr_pane_list_failed", err.Error())
	}
	panes, _ := data["panes"].([]any)
	if panes == nil {
		return nil, takeoverError("liveness.herdr_pane_list_response_has_no_panes")
	}
	var matching []string
	for _, item := range panes {
		pane, _ := item.(map[string]any)
		if pane == nil {
			return nil, takeoverError("takeover.herdr_pane_list_response_contains_an_invalid_pane")
		}
		currentTab := probe.PublicID(pane["tab_id"])
		currentPane := probe.PublicID(pane["pane_id"])
		if currentTab == "" || currentPane == "" {
			return nil, takeoverError("takeover.a_pane_in_the_herdr_pane_list_response_has")
		}
		if currentTab == tabID {
			matching = append(matching, currentPane)
		}
	}
	return matching, nil
}

// ValidateHerdrContainer requires the pane to still live in the target tab and that tab to hold only this pane.
func ValidateHerdrContainer(herdr, tabID, paneID string, pane map[string]any) error {
	actualTab := probe.PublicID(pane["tab_id"])
	if actualTab != tabID {
		return takeoverError(
			"takeover.herdr_pane_tab_mismatch_task_pane", tabID, orNA(actualTab),
		)
	}
	panes, err := herdrTabPanes(herdr, tabID)
	if err != nil {
		return err
	}
	if len(panes) != 1 || panes[0] != paneID {
		return takeoverError(
			"takeover.the_herdr_tab_does_not_contain_only_the_target", tabID, orNA(strings.Join(panes, ",")),
		)
	}
	return nil
}

// ValidateTmuxContainer requires the pane to still live in the target session/window and that window to hold only this pane.
func ValidateTmuxContainer(tmux, paneID, launcher, expectedSession, expectedWindow string) error {
	facts, err := probe.ProbeTmuxContainer(tmux, paneID)
	if err != nil {
		return err
	}
	actualSession := facts.SessionID
	if launcher == "tmux-session" {
		actualSession = facts.SessionName
	}
	if actualSession != expectedSession || facts.WindowID != expectedWindow {
		return takeoverError(
			"takeover.tmux_pane_container_mismatch_task_pane", expectedSession, expectedWindow, orNA(actualSession), orNA(facts.WindowID),
		)
	}
	if facts.PaneCount != "1" {
		return takeoverError(
			"takeover.the_tmux_window_does_not_contain_only_the_target", expectedWindow, orNA(facts.PaneCount),
		)
	}
	return nil
}

func tmuxWindowExists(tmux, windowID string) (bool, error) {
	res, err := probe.Capture(tmux, []string{"display-message", "-p", "-t", windowID, "#{window_id}"}, 0)
	if err != nil {
		return false, err
	}
	if res.Code != 0 {
		detail := strings.TrimSpace(res.Stderr)
		lower := strings.ToLower(detail)
		for _, marker := range []string{"can't find", "no server running", "no sessions"} {
			if strings.Contains(lower, marker) {
				return false, nil
			}
		}
		if detail == "" {
			detail = fmt.Sprintf("exit %d", res.Code)
		}
		return false, takeoverError("takeover.failed_to_probe_the_tmux_window", detail)
	}
	if strings.TrimSpace(res.Stdout) != windowID {
		return false, takeoverError(
			"takeover.tmux_window_probe_returned_an_invalid_response", orNA(strings.TrimSpace(res.Stdout)),
		)
	}
	return true, nil
}

func herdrSessionReference(pane map[string]any) string {
	identity, _ := pane["agent_session"].(map[string]any)
	if identity == nil {
		return ""
	}
	ref, _ := identity["value"].(string)
	return ref
}

func orNA(v string) string {
	if v == "" {
		return "N/A"
	}
	return v
}

func agentCommandName(agent string) string {
	return filepath.Base(config.AgentExecutableName(agent))
}

func toLive(session launch.AgentSession) liveness.TaskSession {
	return liveness.TaskSession{Agent: session.Agent, Reference: session.Reference}
}
