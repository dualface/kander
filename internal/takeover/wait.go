package takeover

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/dualface/kander/internal/launch"
	"github.com/dualface/kander/internal/probe"
)

func herdrJSONErrorCode(res probe.Result) string {
	for _, output := range []string{res.Stderr, res.Stdout} {
		var payload any
		if err := json.Unmarshal([]byte(output), &payload); err != nil {
			continue
		}
		obj, _ := payload.(map[string]any)
		if obj == nil {
			continue
		}
		errObj, _ := obj["error"].(map[string]any)
		if errObj == nil {
			continue
		}
		code, _ := errObj["code"].(string)
		if code != "" {
			return code
		}
	}
	return ""
}

func herdrWaitAgentExit(herdr, tabID, paneID string, session launch.AgentSession, timeout float64) (paneExists bool, err error) {
	deadline := nowFn().Add(time.Duration(timeout * float64(time.Second)))
	for {
		remaining := deadline.Sub(nowFn())
		if remaining <= 0 {
			return false, takeoverError("takeover.timed_out_waiting_for_the_agent_to_exit", paneID)
		}
		res, capErr := probe.Capture(herdr, []string{"pane", "get", paneID}, remaining)
		if capErr != nil {
			return false, capErr
		}
		if res.Code != 0 {
			if herdrJSONErrorCode(res) == "pane_not_found" {
				panes, listErr := herdrTabPanes(herdr, tabID)
				if listErr != nil {
					return false, listErr
				}
				if len(panes) > 0 {
					return false, takeoverError(
						"takeover.the_target_herdr_pane_disappeared_while_its_tab_still", tabID, strings.Join(panes, ","),
					)
				}
				return false, nil
			}
			return false, takeoverError(
				"takeover.failed_to_probe_the_pane_while_waiting_for_the", herdrFailureDetail(res),
			)
		}
		data, jsonErr := probe.HerdrResult(res)
		if jsonErr != nil {
			return false, jsonErr
		}
		pane, _ := data["pane"].(map[string]any)
		if pane == nil || probe.PublicID(pane["pane_id"]) != paneID {
			return false, takeoverError("takeover.herdr_pane_get_returned_an_invalid_response")
		}
		actualTab := probe.PublicID(pane["tab_id"])
		if actualTab != tabID {
			return false, takeoverError(
				"takeover.the_herdr_pane_moved_to_another_tab_while_waiting", tabID, orNA(actualTab),
			)
		}
		actualAgent, _ := pane["agent"].(string)
		if actualAgent == "" {
			if err := ValidateHerdrContainer(herdr, tabID, paneID, pane); err != nil {
				return false, err
			}
			return true, nil
		}
		if actualAgent != session.Agent {
			return false, takeoverError(
				"takeover.the_pane_changed_to_another_agent_while_waiting_for", session.Agent, actualAgent,
			)
		}
		actualSession := herdrSessionReference(pane)
		if session.Reference == "" || actualSession != session.Reference {
			return false, takeoverError(
				"takeover.the_pane_session_changed_while_waiting_for_exit_task", orNA(session.Reference), orNA(actualSession),
			)
		}
		remaining = deadline.Sub(nowFn())
		if remaining > 0 {
			d := pollInterval
			if remaining < d {
				d = remaining
			}
			sleepFn(d)
		}
	}
}

func tmuxWaitAgentExit(tmux, launcher, sessionID, windowID, paneID string, session launch.AgentSession, timeout float64) (windowExists bool, err error) {
	deadline := nowFn().Add(time.Duration(timeout * float64(time.Second)))
	expected := agentCommandName(session.Agent)
	for {
		remaining := deadline.Sub(nowFn())
		if remaining <= 0 {
			return false, takeoverError("takeover.timed_out_waiting_for_the_agent_to_exit", paneID)
		}
		paneProbe, probeErr := probe.ProbeTmuxPaneWithin(tmux, paneID, remaining)
		if probeErr != nil {
			return false, probeErr
		}
		if paneProbe.Facts == nil {
			exists, existsErr := tmuxWindowExists(tmux, windowID)
			if existsErr != nil {
				return false, existsErr
			}
			if !exists {
				return false, nil
			}
			return false, takeoverError(
				"takeover.the_target_tmux_pane_disappeared_while_its_window_still", paneID, windowID,
			)
		}
		facts := paneProbe.Facts
		if facts.Dead == "1" {
			if err := ValidateTmuxContainer(tmux, paneID, launcher, sessionID, windowID); err != nil {
				return false, err
			}
			return tmuxWindowExists(tmux, windowID)
		}
		if facts.Dead != "0" {
			return false, takeoverError(
				"takeover.tmux_pane_returned_an_invalid_dead_state", orNA(facts.Dead),
			)
		}
		if facts.Command != expected {
			return false, takeoverError(
				"takeover.the_tmux_foreground_process_changed_while_waiting_for_exit", expected, orNA(facts.Command),
			)
		}
		if session.Reference == "" || facts.SessionMarker != session.Reference {
			return false, takeoverError(
				"takeover.the_tmux_pane_session_changed_while_waiting_for_exit", orNA(session.Reference), orNA(facts.SessionMarker),
			)
		}
		if err := ValidateTmuxContainer(tmux, paneID, launcher, sessionID, windowID); err != nil {
			return false, err
		}
		remaining = deadline.Sub(nowFn())
		if remaining > 0 {
			d := pollInterval
			if remaining < d {
				d = remaining
			}
			sleepFn(d)
		}
	}
}
