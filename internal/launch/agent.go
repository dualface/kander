package launch

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/process"
)

func launchAgent(
	plan LaunchPlan,
	root string,
	name string,
	invocation process.ProcessInvocation,
	location func(LaunchOutcome) error,
	paneSession func() (AgentSession, error),
	agentSession *AgentSession,
) (LaunchOutcome, error) {
	var createdTab, createdWindow string
	fail := func(err error) error {
		var closeErr string
		if createdTab != "" {
			closeErr = herdrCloseTab(plan.HerdrBin, createdTab)
		} else if plan.Tmux != "" {
			closeErr = tmuxCloseWindow(plan.Tmux, createdWindow)
		}
		return &LaunchFailure{Err: err, CloseError: closeErr}
	}
	if plan.Launcher == "foreground" || plan.Launcher == "console" {
		handle, err := startProcessFn(invocation.Argv, invocation.Env, filepath.Dir(root), plan.Launcher == "console")
		if err != nil {
			return LaunchOutcome{}, fail(launchError("launch.failed_to_start_agent", err.Error()))
		}
		return LaunchOutcome{Process: handle.proc, Wait: handle.Wait, Poll: handle.Poll}, nil
	}
	command := posixJoin(invocation.Argv)
	if plan.Launcher == "herdr" {
		tab, pane, err := herdrCreateTab(plan.HerdrBin, plan.HerdrWorkspace, filepath.Dir(root), name)
		if err != nil {
			return LaunchOutcome{}, fail(err)
		}
		createdTab = tab
		if err := herdrWaitPaneReady(plan.HerdrBin, pane); err != nil {
			return LaunchOutcome{}, fail(err)
		}
		outcome := LaunchOutcome{Tab: tab, Pane: pane}
		if location != nil {
			if err := location(outcome); err != nil {
				return LaunchOutcome{}, fail(err)
			}
		}
		if err := herdrPaneRun(plan.HerdrBin, pane, command); err != nil {
			return LaunchOutcome{}, fail(err)
		}
		if agentSession != nil {
			reportHerdrAgentSession(plan.HerdrBin, pane, *agentSession)
		}
		return outcome, nil
	}
	create := plan.Launcher == "tmux-session" && !plan.SessionExists
	result := tmuxLaunch(plan.Tmux, plan.Session, create, filepath.Dir(root), name)
	if create && result.Code != 0 {
		owner, exists := sessionOwner(plan.Tmux, plan.Session)
		if exists && (owner == "" || owner == plan.Project) {
			create = false
			result = tmuxLaunch(plan.Tmux, plan.Session, false, filepath.Dir(root), name)
		}
	}
	if result.Code != 0 {
		sub := "new-window"
		if create {
			sub = "new-session"
		}
		detail := orExit(trimNL(result.Stderr), result.Code)
		return LaunchOutcome{}, fail(launchError("launch.tmux_failed", sub, detail))
	}
	locationParts := splitTab(trimNL(result.Stdout))
	if len(locationParts) != 2 || locationParts[0] == "" || locationParts[1] == "" {
		return LaunchOutcome{}, fail(launchError("launch.tmux_launch_failed_window_pane_id_was_not_returned"))
	}
	window, pane := locationParts[0], locationParts[1]
	createdWindow = window
	if create {
		_ = tmuxCapture(plan.Tmux, "set-option", "-t", plan.Session, projectSessionOpt, plan.Project)
	}
	outcome := LaunchOutcome{Window: window, Pane: pane}
	if location != nil {
		if err := location(outcome); err != nil {
			return LaunchOutcome{}, fail(err)
		}
	}
	if err := tmuxStartPane(plan.Tmux, pane, command); err != nil {
		return LaunchOutcome{}, fail(err)
	}
	if paneSession != nil {
		sess, err := paneSession()
		if err != nil {
			return LaunchOutcome{}, fail(err)
		}
		if err := tmuxSetPaneSession(plan.Tmux, pane, sess.Reference); err != nil {
			return LaunchOutcome{}, fail(err)
		}
	}
	return outcome, nil
}

func trimNL(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func splitTab(s string) []string {
	out := []string{}
	cur := ""
	for i := 0; i < len(s); i++ {
		if s[i] == '\t' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(s[i])
	}
	out = append(out, cur)
	return out
}

func rollbackLaunch(root string, moved board.Entry, originalState string, failure *LaunchFailure, originalText *string) error {
	var rollbackErrors []string
	restored := true
	if originalText != nil {
		if err := writeDocumentFn(root, moved, *originalText); err != nil {
			restored = false
			rollbackErrors = append(rollbackErrors, t("launch.failed_to_restore_document", err.Error()))
		}
	}
	if restored && moved.State != originalState {
		if _, err := renameEntry(root, moved, originalState); err != nil {
			rollbackErrors = append(rollbackErrors, t(
				"launch.failed_to_move_back_to", originalState, err.Error(),
			))
		}
	}
	primary := failure.Err
	if len(rollbackErrors) > 0 {
		msg := t(
			"launch.start_and_rollback_both_failed_task_remains_in", moved.State, primary.Error(),
		)
		extras := []string{}
		if failure.CloseError != "" {
			extras = append(extras, failure.CloseError)
		}
		extras = append(extras, rollbackErrors...)
		return launchError(msg+joinSemi(extras), msg+joinSemi(extras))
	}
	if failure.CloseError != "" {
		return launchError("launch.value", primary.Error(), failure.CloseError)
	}
	return primary
}

func joinSemi(items []string) string {
	out := ""
	for i, item := range items {
		if i > 0 {
			out += "; "
		}
		out += item
	}
	return out
}

func reportLaunch(verb string, entry board.Entry, agentName string, plan LaunchPlan, outcome LaunchOutcome) error {
	scale := entry.Kind
	if id, ok := map[string]string{"large": "launch.large_task", "small": "launch.small_task"}[entry.Kind]; ok {
		scale = t(id)
	}
	head := t(
		"launch.scale_agent", verb, entry.TaskID, scale, agentName,
	)
	switch plan.Launcher {
	case "foreground":
		fmt.Println(t("launch.launcher_foreground", head))
		code, err := outcome.Wait()
		if err != nil {
			return launchError("launch.failed_to_start_agent", err.Error())
		}
		if code != 0 {
			return launchError(
				"launch.agent_started_but_exited_with_status_task_remains_in", itoa(code),
			)
		}
		return nil
	case "console":
		pid := 0
		if outcome.Process != nil {
			pid = outcome.Process.Pid
		}
		fmt.Println(t(
			"launch.launcher_console_pid", head, itoa(pid),
		))
		return nil
	case "herdr":
		fmt.Println(t(
			"launch.launcher_herdr_tab_pane", head, outcome.Tab, outcome.Pane,
		))
		return nil
	case "tmux-session":
		fmt.Println(t(
			"launch.session_window", head, plan.Session, outcome.Window,
		))
		hint := "tmux attach -t " + plan.Session
		if os.Getenv("TMUX") != "" {
			hint = "tmux switch-client -t " + plan.Session
		}
		fmt.Println(t("launch.view", hint))
		return nil
	default:
		fmt.Println(t(
			"launch.launcher_tmux_window", head, outcome.Window,
		))
		return nil
	}
}

func asLaunchFailure(err error) *LaunchFailure {
	if err == nil {
		return nil
	}
	if f, ok := err.(*LaunchFailure); ok {
		return f
	}
	return &LaunchFailure{Err: err}
}

func validateLivenessTimeout(timeout float64, command string) error {
	if math.IsNaN(timeout) || math.IsInf(timeout, 0) || timeout <= 60 {
		return launchError(
			"launch.timeout_must_be_a_finite_number_greater_than_60", command,
		)
	}
	return nil
}

func validateResumedAgent(plan LaunchPlan, outcome LaunchOutcome, session AgentSession, timeout float64) error {
	deadline := nowFn().Add(time.Duration(timeout * float64(time.Second)))
	for {
		if plan.Launcher == "foreground" || plan.Launcher == "console" {
			if outcome.Poll != nil {
				if code := outcome.Poll(); code != nil {
					return launchError(
						"launch.resumed_agent_exited_during_the_liveness_observation_period_exit", itoa(*code),
					)
				}
			}
			if !nowFn().Before(deadline) {
				return nil
			}
			remaining := deadline.Sub(nowFn())
			d := notifyPollInterval
			if remaining < d {
				d = remaining
			}
			sleepFn(d)
			continue
		}
		remaining := deadline.Sub(nowFn())
		if remaining <= 0 {
			return launchError("launch.resumed_agent_liveness_check_timed_out")
		}
		ok := false
		if plan.Launcher == "herdr" {
			pane, err := probe.HerdrProbePane(plan.HerdrBin, outcome.Pane, remaining)
			if err == nil {
				ref := herdrSessionReference(pane)
				agent, _ := pane["agent"].(string)
				status, _ := pane["agent_status"].(string)
				if agent == session.Agent &&
					(session.Reference == "" || ref == "" || ref == session.Reference) &&
					(status == "idle" || status == "working" || status == "blocked") {
					ok = true
				}
			}
		} else if tmuxNotifyTarget(plan.Tmux, outcome.Pane, session, remaining) == nil {
			ok = true
		}
		if ok {
			return nil
		}
		if !nowFn().Before(deadline) {
			return launchError("launch.resumed_agent_liveness_check_timed_out")
		}
		remaining = deadline.Sub(nowFn())
		d := notifyPollInterval
		if remaining < d {
			d = remaining
		}
		sleepFn(d)
	}
}

func resumedAgentFailureOutput(plan LaunchPlan, outcome LaunchOutcome) string {
	var res cmdResult
	var err error
	if plan.Launcher == "herdr" && outcome.Pane != "" {
		res, err = herdrCapture(plan.HerdrBin, []string{"pane", "read", outcome.Pane}, probe.DefaultCommandTimeout)
	} else if (plan.Launcher == "tmux" || plan.Launcher == "tmux-session") && outcome.Pane != "" {
		res, err = runCaptured(plan.Tmux, []string{"capture-pane", "-p", "-t", outcome.Pane}, probe.DefaultCommandTimeout)
	} else {
		return ""
	}
	if err != nil || res.Code != 0 {
		return ""
	}
	output := trimNL(res.Stdout)
	if len(output) > resumeOutputLimit {
		output = output[len(output)-resumeOutputLimit:]
	}
	return output
}

func cleanupFailedResume(plan LaunchPlan, outcome LaunchOutcome) error {
	var cleanup string
	if plan.Launcher == "herdr" && outcome.Tab != "" {
		cleanup = herdrCloseTab(plan.HerdrBin, outcome.Tab)
	} else if (plan.Launcher == "tmux" || plan.Launcher == "tmux-session") && outcome.Window != "" {
		cleanup = tmuxCloseWindow(plan.Tmux, outcome.Window)
	} else if outcome.Process != nil && outcome.Poll != nil && outcome.Poll() == nil {
		_ = terminateProcess(outcome.Process)
		waited := make(chan struct{})
		go func() {
			if outcome.Wait != nil {
				_, _ = outcome.Wait()
			}
			close(waited)
		}()
		select {
		case <-waited:
		case <-time.After(5 * time.Second):
			_ = killProcess(outcome.Process)
		}
	}
	if cleanup != "" {
		return launchError(
			"launch.cleanup_after_failed_resume_failed_the_new_agent_may", cleanup,
		)
	}
	return nil
}

func naCleanup() CleanupResult {
	return CleanupResult{Cleaned: true, OldWindow: "N/A", Channel: "N/A", Container: "N/A"}
}
