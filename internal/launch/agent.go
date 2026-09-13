package launch

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/terminal"
)

func launchAgent(
	plan LaunchPlan,
	root string,
	name string,
	invocation process.ProcessInvocation,
	location func(LaunchOutcome) error,
	paneSession func() (AgentSession, error),
	agentSession *AgentSession,
	durable ...bool,
) (LaunchOutcome, error) {
	backend := plan.backend()
	caps := backend.Capabilities()
	var created terminal.Address
	sendAttempted := false
	fail := func(err error) error {
		// Pane delivery fails before the prompt is sent (blocked or ready
		// timeout). That outcome is known, so close the container even on a
		// durable dispatch instead of leaving a stuck tab and an unrolled card.
		if sendAttempted && len(durable) > 0 && durable[0] && plan.PromptDelivery.Mode != "pane" {
			return &LaunchFailure{Err: err, DeliveryUnknown: true}
		}
		var closeErr string
		if created.Container != "" {
			if err := backend.CloseContainer(context.Background(), boundedSpawnConn(plan), created); err != nil {
				closeErr = err.Error()
			}
		}
		return &LaunchFailure{Err: err, CloseError: closeErr}
	}
	if !caps.Container {
		handle, err := startProcessFn(invocation.Argv, invocation.Env, filepath.Dir(root), caps.Detached)
		if err != nil {
			return LaunchOutcome{}, fail(launchError("launch.failed_to_start_agent", err.Error()))
		}
		return LaunchOutcome{Process: handle.proc, Wait: handle.Wait, Poll: handle.Poll}, nil
	}
	command, err := paneCommand(invocation)
	if err != nil {
		return LaunchOutcome{}, fail(err)
	}
	paneDelivery := plan.PromptDelivery.Mode == "pane"
	locateNow := location
	if paneDelivery {
		locateNow = nil
	}
	conn := spawnConn(plan)
	address, err := backend.CreateContainer(conn, plan.Target, filepath.Dir(root), name)
	if err != nil {
		return LaunchOutcome{}, fail(err)
	}
	created = address
	if err := backend.WaitReady(conn, address.Pane); err != nil {
		return LaunchOutcome{}, fail(err)
	}
	outcome := LaunchOutcome{Container: address.Container, Pane: address.Pane}
	if locateNow != nil {
		if err := locateNow(outcome); err != nil {
			return LaunchOutcome{}, fail(err)
		}
	}
	sendAttempted = true
	if err := backend.RunCommand(conn, address.Pane, command, !runtimeWindows()); err != nil {
		return LaunchOutcome{}, fail(err)
	}
	if paneDelivery {
		if err := completePaneDelivery(plan, outcome); err != nil {
			return LaunchOutcome{}, fail(err)
		}
		if location != nil {
			if err := location(outcome); err != nil {
				return LaunchOutcome{}, fail(err)
			}
		}
	}
	if caps.SessionReport && agentSession != nil {
		warn := plan.warning
		if warn == nil {
			warn = func(message string) { fmt.Fprint(os.Stderr, message) }
		}
		reportAgentSession(plan, address.Pane, *agentSession, warn)
	}
	if caps.PaneMetadata && paneSession != nil {
		sess, err := paneSession()
		if err != nil {
			return LaunchOutcome{}, fail(err)
		}
		if err := backend.SetSessionMarker(conn, address.Pane, sess.Reference); err != nil {
			return LaunchOutcome{}, fail(err)
		}
	}
	return outcome, nil
}

// reportAgentSession reports the session identity out of band within a short
// budget. A failure only warns: it never fails the launch or closes the pane.
func reportAgentSession(plan LaunchPlan, pane string, session AgentSession, warn func(string)) {
	if session.Reference == "" {
		return
	}
	report := terminal.SessionReport{Conn: probeConn(plan), Pane: pane, Agent: session.Agent, Reference: session.Reference, Now: nowFn}
	report.Deadline = nowFn().Add(sessionReportBudget)
	var last error = launchError("launch.herdr_session_identity_report_budget_exhausted")
	for nowFn().Before(report.Deadline) {
		err := plan.backend().ReportSession(report)
		if errors.Is(err, terminal.ErrNoReportChannel) {
			warn(t(
				"launch.warning_failed_to_report_the_herdr_session_identity_herdr",
			))
			return
		}
		if err == nil {
			return
		}
		last = err
		remaining := report.Deadline.Sub(nowFn())
		if remaining > 0 {
			d := notifyPollInterval
			if remaining < d {
				d = remaining
			}
			sleepFn(d)
		}
	}
	warn(t(
		"launch.warning_failed_to_report_the_herdr_session_identity", last.Error(),
	))
}

func trimNL(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func rollbackLaunch(root string, moved board.Entry, originalState string, failure *LaunchFailure, originalText *string) error {
	var rollbackErrors []string
	if originalText != nil {
		if err := board.RollbackDocument(root, moved, *originalText, originalState); err != nil {
			rollbackErrors = append(rollbackErrors, t("launch.failed_to_restore_document", err.Error()))
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
	caps := plan.capabilities()
	switch {
	case !caps.Container && !caps.Detached:
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
	case caps.Detached:
		pid := 0
		if outcome.Process != nil {
			pid = outcome.Process.Pid
		}
		fmt.Println(t(
			"launch.launcher_console_pid", head, itoa(pid),
		))
		return nil
	default:
		for _, line := range plan.backend().StartedLines(head, plan.Target, plan.address(outcome), os.Getenv) {
			fmt.Println(line)
		}
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
	return validateResumedAgentReceipt(plan, outcome, session, timeout, nil)
}

func validateResumedAgentReceipt(plan LaunchPlan, outcome LaunchOutcome, session AgentSession, timeout float64, receipt func() (bool, error)) error {
	deadline := nowFn().Add(time.Duration(timeout * float64(time.Second)))
	for {
		if receipt != nil {
			consumed, err := receipt()
			if err != nil {
				return err
			}
			if consumed {
				return nil
			}
		}
		if !plan.capabilities().Container {
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
		if plan.capabilities().AgentIdentity {
			ctx, cancel := probe.TimeoutContext(remaining)
			pane, err := plan.backend().PaneFacts(ctx, probeConn(plan), outcome.Pane)
			cancel()
			if err == nil && !pane.Gone {
				ref := pane.AgentSession
				status := pane.AgentStatus
				if pane.Agent == session.Agent &&
					(session.Reference == "" || ref == "" || ref == session.Reference) &&
					(status == "idle" || status == "working" || status == "blocked") {
					ok = true
				}
			}
		} else if processNotifyTarget(plan, outcome.Pane, session, remaining) == nil {
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
	if !plan.capabilities().Container || outcome.Pane == "" {
		return ""
	}
	output, err := plan.backend().ReadOutput(context.Background(), boundedSpawnConn(plan), outcome.Pane)
	if err != nil {
		return ""
	}
	output = trimNL(output)
	if len(output) > resumeOutputLimit {
		output = output[len(output)-resumeOutputLimit:]
	}
	return output
}

func cleanupFailedResume(plan LaunchPlan, outcome LaunchOutcome) error {
	var cleanup string
	if plan.capabilities().Container {
		if outcome.Container != "" {
			if err := plan.backend().CloseContainer(context.Background(), boundedSpawnConn(plan), plan.address(outcome)); err != nil {
				cleanup = err.Error()
			}
		}
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
