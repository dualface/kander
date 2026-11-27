package launch

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/process"
	"github.com/dualface/kander/v2/internal/terminal"
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
	var running LaunchOutcome
	stop := func() string { return "" }
	sendAttempted := false
	fail := func(err error) error {
		// Pane delivery fails before the prompt is sent (blocked or ready
		// timeout). That outcome is known, so close the container even on a
		// durable dispatch instead of leaving a stuck tab and an unrolled card.
		if sendAttempted && len(durable) > 0 && durable[0] && !plan.usesPanePrompt() {
			return &LaunchFailure{Err: err, DeliveryUnknown: true, Outcome: running}
		}
		// This is the last moment the judgement can change anything: past it the
		// executor is gone.
		if verdict := plan.judgeFailure(); verdict != verdictCleanup {
			return &LaunchFailure{Err: err, Verdict: verdict, Outcome: running}
		}
		return &LaunchFailure{Err: err, CloseError: stop(), Outcome: running}
	}
	if !caps.Container {
		handle, err := startProcessFn(invocation.Argv, invocation.Env, filepath.Dir(root), caps.Detached)
		if err != nil {
			return LaunchOutcome{}, fail(launchError("launch.failed_to_start_agent", err.Error()))
		}
		running = LaunchOutcome{Process: handle.proc, Wait: handle.Wait, Poll: handle.Poll}
		if plan.processRecord != nil && handle.proc != nil {
			running.IdentityDone = plan.processRecord(handle.proc.Pid, func() bool { return handle.Poll() == nil })
		}
		stop = func() string {
			err := handle.proc.Kill()
			_, _ = handle.Wait()
			if err != nil && !errors.Is(err, os.ErrProcessDone) {
				return err.Error()
			}
			return ""
		}
		if paneSession != nil {
			sess, err := paneSession()
			if err != nil {
				return LaunchOutcome{}, fail(err)
			}
			if agentSession != nil {
				*agentSession = sess
			}
			if plan.sessionFinalize != nil {
				if err := plan.sessionFinalize(sess); err != nil {
					return LaunchOutcome{}, fail(err)
				}
			}
		}
		if plan.rename.command != "" {
			plan.warn(t("launch.rename_no_pane"))
		}
		return running, nil
	}
	startupDir := filepath.Dir(root)
	if err := validateStartupDirectory(startupDir); err != nil {
		return LaunchOutcome{}, fail(err)
	}
	command, err := paneCommand(invocation)
	if err != nil {
		return LaunchOutcome{}, fail(err)
	}
	paneDelivery := plan.usesPanePrompt()
	locateNow := location
	if paneDelivery {
		locateNow = nil
	}
	conn := spawnConn(plan)
	address, err := backend.CreateContainer(conn, plan.Target, startupDir, name)
	if err != nil {
		return LaunchOutcome{}, fail(err)
	}
	created = address
	running = LaunchOutcome{Container: address.Container, Pane: address.Pane}
	stop = func() string {
		if err := backend.CloseContainer(context.Background(), boundedSpawnConn(plan), created); err != nil {
			return err.Error()
		}
		return ""
	}
	// Placement only warns: a terminal that sits in no tab, or in a tab with the
	// default name, still runs the agent.
	if placer, ok := backend.(terminal.ContainerPlacer); ok {
		if note := placer.PlaceContainer(conn, address, plan.Target, name); note != "" {
			plan.warn(t("launch.warning_container_placement", note))
		}
	}
	readyTimeout := plan.launchTimeout(terminal.DefaultReadyTimeout)
	if err := backend.WaitReady(conn, address.Pane, int(readyTimeout/time.Millisecond)); err != nil {
		return LaunchOutcome{}, fail(err)
	}
	outcome := LaunchOutcome{Container: address.Container, Pane: address.Pane}
	if locateNow != nil {
		if err := locateNow(outcome); err != nil {
			return LaunchOutcome{}, fail(err)
		}
	}
	sendAttempted = true
	if err := backend.RunCommand(conn, address.Pane, quotedPaneCwd(startupDir), command, !runtimeWindows()); err != nil {
		return LaunchOutcome{}, fail(err)
	}
	switch watchPaneEntry(plan, address.Pane, startupDir) {
	case paneUnconfirmed:
		// The agent may be running, so a durable dispatch stays DeliveryUnknown.
		return LaunchOutcome{}, fail(launchError("launch.agent_start_unconfirmed", startupDir))
	case paneGone:
		// The window went with its only pane, which leaves nothing to close.
		stop = func() string { return "" }
		fallthrough
	case paneDead:
		// No agent is left in the pane to reach, so the delivery is not open:
		// fail applies the judgement, and the dispatch receipts cover an agent
		// that started and exited between two polls.
		sendAttempted = false
		return LaunchOutcome{}, fail(launchError("launch.pane_ended_before_agent_started", startupDir))
	}
	if paneDelivery {
		if err := completePaneDelivery(plan, outcome, name); err != nil {
			return LaunchOutcome{}, fail(err)
		}
		if location != nil {
			if err := location(outcome); err != nil {
				return LaunchOutcome{}, fail(err)
			}
		}
	} else if plan.rename.command != "" {
		if err := renameAgentSession(plan, outcome, name); err != nil {
			return LaunchOutcome{}, fail(err)
		}
	}
	if paneSession != nil {
		sess, err := paneSession()
		if err != nil {
			return LaunchOutcome{}, fail(err)
		}
		if agentSession != nil {
			*agentSession = sess
		}
		if caps.PaneMetadata {
			if err := backend.SetSessionMarker(conn, address.Pane, sess.Reference); err != nil {
				return LaunchOutcome{}, fail(err)
			}
			running.SessionMarked, outcome.SessionMarked = true, true
		}
		if plan.sessionFinalize != nil {
			if err := plan.sessionFinalize(sess); err != nil {
				return LaunchOutcome{}, fail(err)
			}
		}
	}
	if caps.SessionReport && agentSession != nil {
		reportAgentSession(plan, address.Pane, *agentSession, plan.warn)
	}
	return outcome, nil
}

// reportAgentSession reports the session identity out of band within a short
// budget. A failure only warns: it never fails the launch or closes the pane.
func reportAgentSession(plan LaunchPlan, pane string, session AgentSession, warn func(string)) {
	// An agent declaring session.file reports a path-kind identity through
	// its own integration; an early id-kind report races that native report
	// and reads back as a false mismatch, so it is skipped entirely.
	if session.Reference == "" || plan.sessionFileDeclared {
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

func reportLaunch(verb string, entry board.Entry, agentName string, plan LaunchPlan, outcome LaunchOutcome) error {
	if outcome.Replayed {
		fmt.Println(t("remote.launch_already_confirmed", entry.TaskID))
		return nil
	}
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
