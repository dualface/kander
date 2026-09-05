package takeover

import (
	"fmt"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/launch"
	"github.com/dualface/kander/internal/liveness"
	"github.com/dualface/kander/internal/notify"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/window"
)

func reverseLookupStale[T any](detail string, lookup func() (T, error)) (T, error) {
	value, err := lookup()
	if err != nil {
		var zero T
		return zero, takeoverError(
			"notify.stale_address_lookup", detail, err.Error(),
		)
	}
	return value, nil
}

func commandDismiss(root, taskID string, timeout float64) error {
	loaded, err := board.LoadBoard(root)
	if err != nil {
		return err
	}
	entry, err := board.Locate(loaded, taskID)
	if err != nil {
		return err
	}
	if entry.State != "done" && entry.State != "archived" {
		return takeoverError(
			"takeover.only_tasks_in_done_or_archived_can_be_dismissed", entry.TaskID, entry.State,
		)
	}
	if err := launch.ValidateTimeout(timeout, "dismiss"); err != nil {
		return err
	}
	if isWindows() {
		return takeoverError(
			"takeover.windows_currently_has_no_direct_terminal_channel_for_dismiss",
		)
	}
	text, err := board.ReadDocument(entry)
	if err != nil {
		return err
	}
	windowValue := board.MetadataFrom(text, window.WindowField)
	herdrMatch := herdrWindowRe.FindStringSubmatch(windowValue)
	tmuxMatch := tmuxWindowRe.FindStringSubmatch(windowValue)
	if windowValue != "" && herdrMatch == nil && tmuxMatch == nil {
		return takeoverError(
			"takeover.task_has_no_dismissible_terminal_container", windowValue,
		)
	}
	session, err := launch.ResolvedSession(entry.TaskID, text)
	if err != nil {
		return err
	}
	command, err := AgentExitCommand(session.Agent)
	if err != nil {
		return err
	}
	live := toLive(session)
	var channel, container string
	if herdrMatch != nil || windowValue == "" {
		herdr, err := lookPath("herdr")
		if err != nil {
			return takeoverError("liveness.herdr_is_not_in_path")
		}
		var tabID, paneID string
		if herdrMatch != nil {
			tabID, paneID = herdrMatch[1], herdrMatch[2]
			probeResult := notify.HerdrNotifyProbe(herdr, paneID, live, probe.DefaultCommandTimeout)
			if probeResult.State == "stale" {
				discovered, err := reverseLookupStale(probeResult.Detail, func() (struct{ Tab, Pane string }, error) {
					tab, pane, err := liveness.HerdrReverseLookup(herdr, live)
					return struct{ Tab, Pane string }{tab, pane}, err
				})
				if err != nil {
					return err
				}
				tabID, paneID, err = notify.HerdrExplicitTarget(herdr, discovered.Pane)
				if err != nil {
					return err
				}
			}
		} else {
			_, pane, err := liveness.HerdrReverseLookup(herdr, live)
			if err != nil {
				return err
			}
			tabID, paneID, err = notify.HerdrExplicitTarget(herdr, pane)
			if err != nil {
				return err
			}
		}
		pane, err := notify.HerdrNotifyTarget(herdr, paneID, live)
		if err != nil {
			return err
		}
		if err := ValidateHerdrContainer(herdr, tabID, paneID, pane); err != nil {
			return err
		}
		if err := notify.HerdrAgentPrompt(herdr, paneID, command); err != nil {
			return err
		}
		paneExists, err := herdrWaitAgentExit(herdr, tabID, paneID, session, timeout)
		if err != nil {
			return err
		}
		if paneExists {
			if err := herdrCloseTab(herdr, tabID); err != nil {
				return err
			}
		}
		channel, container = "herdr", tabID
	} else {
		launcher, tmuxSession, windowID, paneID := tmuxMatch[1], tmuxMatch[2], tmuxMatch[3], tmuxMatch[4]
		tmux, err := lookPath("tmux")
		if err != nil {
			return takeoverError("liveness.tmux_is_not_in_path")
		}
		probeResult := notify.TmuxNotifyProbe(tmux, paneID, live, probe.DefaultCommandTimeout)
		if probeResult.State == "stale" {
			location, err := reverseLookupStale(probeResult.Detail, func() (liveness.TmuxPaneLocation, error) {
				return liveness.TmuxReverseLookup(tmux, live)
			})
			if err != nil {
				return err
			}
			paneID = location.PaneID
			facts, err := probe.ProbeTmuxContainer(tmux, paneID)
			if err != nil {
				return err
			}
			if launcher == "tmux" {
				tmuxSession = facts.SessionID
			} else {
				tmuxSession = facts.SessionName
			}
			windowID = facts.WindowID
		}
		if err := notify.TmuxNotifyTarget(tmux, paneID, live); err != nil {
			return err
		}
		if err := ValidateTmuxContainer(tmux, paneID, launcher, tmuxSession, windowID); err != nil {
			return err
		}
		if err := tmuxSendAgentExit(tmux, paneID, command); err != nil {
			return err
		}
		windowExists, err := tmuxWaitAgentExit(tmux, launcher, tmuxSession, windowID, paneID, session, timeout)
		if err != nil {
			return err
		}
		if windowExists {
			if err := tmuxCloseWindow(tmux, windowID); err != nil {
				return err
			}
		}
		channel, container = launcher, windowID
	}
	fmt.Println(t(
		"takeover.dismissed_channel_closed_container", entry.TaskID, channel, container,
	))
	return nil
}
