package takeover

import (
	"regexp"
	"runtime"
	"strings"

	"github.com/dualface/kander/internal/launch"
	"github.com/dualface/kander/internal/notify"
	"github.com/dualface/kander/internal/probe"
)

var (
	herdrWindowRe = regexp.MustCompile(`^herdr:([^:\s]+:[^:\s]+):([^:\s]+:[^:\s]+)$`)
	tmuxWindowRe  = regexp.MustCompile(`^(tmux|tmux-session):([^:\s]+):([^:\s]+):([^:\s]+)$`)
)

func init() {
	isWindows = func() bool { return runtime.GOOS == "windows" }
}

func closed(oldWindow, channel, container string) launch.CleanupResult {
	return launch.CleanupResult{Cleaned: true, OldWindow: orNA(oldWindow), Channel: channel, Container: container}
}

func retained(oldWindow, detail string) launch.CleanupResult {
	return launch.CleanupResult{Cleaned: false, OldWindow: orNA(oldWindow), Detail: strings.Join(strings.Fields(detail), " ")}
}

// Cleanup closes the original container under the dismiss gates once the new agent is alive after a takeover; on failure it only keeps and reports it.
func Cleanup(oldWindow string, oldSession launch.AgentSession, newWindow string, timeout float64) launch.CleanupResult {
	if isWindows() || oldWindow == "" || oldWindow == "foreground" || oldWindow == "console" {
		return closed("N/A", "N/A", "N/A")
	}
	if oldWindow == newWindow {
		return closed("N/A", "N/A", "N/A")
	}
	herdrMatch := herdrWindowRe.FindStringSubmatch(oldWindow)
	tmuxMatch := tmuxWindowRe.FindStringSubmatch(oldWindow)
	if herdrMatch == nil && tmuxMatch == nil {
		return retained(oldWindow, t("takeover.old_window_metadata_is_invalid"))
	}
	result, err := cleanupContainer(herdrMatch, tmuxMatch, oldWindow, oldSession, timeout)
	if err != nil {
		return retained(oldWindow, err.Error())
	}
	return result
}

func cleanupContainer(herdrMatch, tmuxMatch []string, oldWindow string, oldSession launch.AgentSession, timeout float64) (launch.CleanupResult, error) {
	if herdrMatch != nil {
		tabID, paneID := herdrMatch[1], herdrMatch[2]
		herdr, err := lookPath("herdr")
		if err != nil {
			return launch.CleanupResult{}, takeoverError("liveness.herdr_is_not_in_path")
		}
		paneProbe, err := probe.ProbeHerdrPane(herdr, paneID, 0)
		if err != nil {
			return launch.CleanupResult{}, err
		}
		pane := paneProbe.Pane
		if pane == nil {
			panes, err := herdrTabPanes(herdr, tabID)
			if err != nil {
				return launch.CleanupResult{}, err
			}
			if len(panes) > 0 {
				return launch.CleanupResult{}, takeoverError(
					"takeover.the_old_pane_is_gone_but_its_tab_still", strings.Join(panes, ","),
				)
			}
			if err := herdrCloseTab(herdr, tabID); err != nil {
				return launch.CleanupResult{}, err
			}
			return closed(oldWindow, "herdr", tabID), nil
		}
		if err := ValidateHerdrContainer(herdr, tabID, paneID, pane); err != nil {
			return launch.CleanupResult{}, err
		}
		actualAgent, _ := pane["agent"].(string)
		if actualAgent != "" {
			actualSession := herdrSessionReference(pane)
			if actualAgent != oldSession.Agent || oldSession.Reference == "" || actualSession == "" || actualSession != oldSession.Reference {
				return launch.CleanupResult{}, takeoverError(
					"takeover.the_old_pane_agent_or_session_identity_does_not",
				)
			}
			status, _ := pane["agent_status"].(string)
			if status != "idle" && status != "done" {
				return launch.CleanupResult{}, takeoverError(
					"takeover.the_old_pane_status_cannot_be_dismissed", orNA(status),
				)
			}
			command, err := AgentExitCommand(oldSession.Agent)
			if err != nil {
				return launch.CleanupResult{}, err
			}
			if err := notifyHerdrPrompt(herdr, paneID, command); err != nil {
				return launch.CleanupResult{}, err
			}
			paneExists, err := herdrWaitAgentExit(herdr, tabID, paneID, oldSession, timeout)
			if err != nil {
				return launch.CleanupResult{}, err
			}
			if paneExists {
				if err := herdrCloseTab(herdr, tabID); err != nil {
					return launch.CleanupResult{}, err
				}
			}
		} else if err := herdrCloseTab(herdr, tabID); err != nil {
			return launch.CleanupResult{}, err
		}
		return closed(oldWindow, "herdr", tabID), nil
	}

	launcher, sessionID, windowID, paneID := tmuxMatch[1], tmuxMatch[2], tmuxMatch[3], tmuxMatch[4]
	tmux, err := lookPath("tmux")
	if err != nil {
		return launch.CleanupResult{}, takeoverError("liveness.tmux_is_not_in_path")
	}
	paneProbe, err := probe.ProbeTmuxPane(tmux, paneID)
	if err != nil {
		return launch.CleanupResult{}, err
	}
	if paneProbe.Facts == nil {
		exists, err := tmuxWindowExists(tmux, windowID)
		if err != nil {
			return launch.CleanupResult{}, err
		}
		if !exists {
			return closed(oldWindow, launcher, windowID), nil
		}
		return launch.CleanupResult{}, takeoverError(
			"takeover.the_old_pane_is_gone_but_its_window_still",
		)
	}
	facts := paneProbe.Facts
	if err := ValidateTmuxContainer(tmux, paneID, launcher, sessionID, windowID); err != nil {
		return launch.CleanupResult{}, err
	}
	expected := agentCommandName(oldSession.Agent)
	if facts.Dead == "1" {
		if err := tmuxCloseWindow(tmux, windowID); err != nil {
			return launch.CleanupResult{}, err
		}
		return closed(oldWindow, launcher, windowID), nil
	}
	if facts.Dead != "0" || facts.Command != expected || oldSession.Reference == "" || facts.SessionMarker == "" || facts.SessionMarker != oldSession.Reference {
		return launch.CleanupResult{}, takeoverError(
			"takeover.the_old_tmux_pane_agent_or_session_identity_does",
		)
	}
	if facts.InMode != "0" {
		return launch.CleanupResult{}, takeoverError("takeover.the_old_tmux_pane_is_in_copy_mode")
	}
	command, err := AgentExitCommand(oldSession.Agent)
	if err != nil {
		return launch.CleanupResult{}, err
	}
	if err := tmuxSendAgentExit(tmux, paneID, command); err != nil {
		return launch.CleanupResult{}, err
	}
	windowExists, err := tmuxWaitAgentExit(tmux, launcher, sessionID, windowID, paneID, oldSession, timeout)
	if err != nil {
		return launch.CleanupResult{}, err
	}
	if windowExists {
		if err := tmuxCloseWindow(tmux, windowID); err != nil {
			return launch.CleanupResult{}, err
		}
	}
	return closed(oldWindow, launcher, windowID), nil
}

func notifyHerdrPrompt(herdr, paneID, command string) error {
	return notify.HerdrAgentPrompt(herdr, paneID, command)
}
