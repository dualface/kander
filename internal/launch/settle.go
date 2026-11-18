package launch

import (
	"errors"
	"fmt"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/issue"
)

// exitUnresolved separates an unresolved start from a failed one (1) and a usage
// error (2), so a script can tell the three results of a start apart.
const exitUnresolved = 3

// launchVerdict says what a failed launcher may destroy.
type launchVerdict int

const (
	// verdictCleanup: no executor evidence; close the container and roll back.
	verdictCleanup launchVerdict = iota
	// verdictKeep: an executor may be working; keep the container and the card.
	verdictKeep
	// verdictSuperseded: another execution owns the card; touch nothing.
	verdictSuperseded
)

// lateFailureJudge reads the facts of a failed launch. Every cleanup asks it first,
// because a closed container cannot come back. A failed read proves nothing, so it
// keeps the container.
func lateFailureJudge(root string, entry board.Entry) func() launchVerdict {
	return func() launchVerdict {
		standing, err := readStandingFn(root, entry)
		switch {
		case err != nil:
			return verdictKeep
		case standing.Superseded:
			return verdictSuperseded
		case standing.Evidence:
			return verdictKeep
		}
		return verdictCleanup
	}
}

// UnresolvedError is the third result of a start: the launch failed late and an
// executor may be working, so nothing was closed or rolled back.
type UnresolvedError struct {
	TaskID  string
	Window  string
	Reason  string
	Command string
	// Unmarked: the pane at Window carries no session marker, so no probe can
	// prove its executor alive and a takeover leaves that executor running.
	Unmarked bool
	// Repairable: adoption can write the missing marker first (markerRepairable).
	Repairable bool
	// hold keeps a foreground executor attached to this terminal.
	hold func()
}

// Error is the fixed line scripts match on.
func (e *UnresolvedError) Error() string {
	return fmt.Sprintf("started-unresolved: %s window=%s reason=%s", e.TaskID, orNA(e.Window), e.Reason)
}

// adoptBlocked reports a pane without a session marker that adoption cannot
// mark either.
func (e *UnresolvedError) adoptBlocked() bool {
	return e.Unmarked && !e.Repairable
}

// NextSteps names the ways out that can work. A retried start is not one of
// them, and adoption is not one for a pane it can neither prove nor mark.
func (e *UnresolvedError) NextSteps() []string {
	takeover := fmt.Sprintf("%s resume %s --agent <agent> --decision <reference> --message <message>", e.Command, e.TaskID)
	if e.adoptBlocked() {
		return []string{t("launch.unmarked_executor_stop_first", e.Window), takeover}
	}
	return []string{fmt.Sprintf("%s resume %s --adopt --decision <reference>", e.Command, e.TaskID), takeover}
}

// recordUnresolved ends a start attempt as unresolved and builds its result. A
// launcher overtaken at this last step steps aside like any other.
func recordUnresolved(root string, plan LaunchPlan, entry board.Entry, outcome LaunchOutcome, cause error) error {
	window := ""
	if plan.capabilities().Container && outcome.Container != "" {
		window = locationOf(plan, outcome)
	}
	reason := issue.Sanitize(cause.Error())
	if _, err := board.UnresolveExecution(root, entry, window, reason); err != nil {
		if errors.Is(err, board.ErrExecutionSuperseded) {
			return err
		}
		return launchError("launch.value", cause.Error(), err.Error())
	}
	record, _, err := board.CurrentExecution(root, entry.TaskID)
	if err != nil {
		return launchError("launch.value", cause.Error(), err.Error())
	}
	unresolved := &UnresolvedError{TaskID: entry.TaskID, Window: record.Window, Reason: reason, Command: "kander"}
	unresolved.Unmarked = plan.capabilities().PaneMetadata && outcome.Container != "" && !outcome.SessionMarked
	unresolved.Repairable = unresolved.Unmarked && markerRepairable(record.Session)
	if paths, err := currentInstallPaths(); err == nil {
		unresolved.Command = commandName(paths)
	}
	if plan.OccupiesTerminal() && outcome.Wait != nil {
		unresolved.hold = func() { _, _ = outcome.Wait() }
	}
	return unresolved
}

// settleFailedLaunch ends the start attempt of a failed launch. It returns
// failure.Err only when the card is back where it was. A failed close leaves an
// executor that may run on, so it never rolls back (a later start would run a
// second one). A rollback that loses to a late write keeps that write.
func settleFailedLaunch(root string, plan LaunchPlan, entry board.Entry, state string, failure *LaunchFailure, original string) error {
	cause := failure.Err
	switch {
	case failure.Verdict == verdictSuperseded:
		return board.ErrExecutionSuperseded
	case failure.Verdict == verdictKeep:
	case failure.CloseError != "":
		cause = launchError("launch.value", cause.Error(), failure.CloseError)
	default:
		err := board.RollbackDocument(root, entry, original, state)
		if err == nil {
			return failure.Err
		}
		if errors.Is(err, board.ErrExecutionSuperseded) {
			return err
		}
		cause = launchError("launch.value", cause.Error(), t("launch.failed_to_restore_document", err.Error()))
	}
	return recordUnresolved(root, plan, entry, failure.Outcome, cause)
}

// settleAfterLaunch handles a step that fails after launchAgent returned. It keeps
// the order of launchAgent: judge first, clean up only without evidence.
func settleAfterLaunch(root string, plan LaunchPlan, entry board.Entry, state string, outcome LaunchOutcome, cause error, original string) error {
	failure := &LaunchFailure{Err: cause, Outcome: outcome, Verdict: plan.judgeFailure()}
	if failure.Verdict == verdictCleanup {
		if err := cleanupFailedResume(plan, outcome); err != nil {
			failure.CloseError = err.Error()
		}
	}
	return settleFailedLaunch(root, plan, entry, state, failure, original)
}

// stepAside reports a launcher a takeover or an adoption overtook. That is a normal
// exit. A foreground executor keeps the terminal until it ends.
func stepAside(task string, plan LaunchPlan, outcome LaunchOutcome) {
	fmt.Println(t("launch.superseded", task))
	if plan.OccupiesTerminal() && outcome.Wait != nil {
		_, _ = outcome.Wait()
	}
}

// exitLaunch turns the result of a start or a takeover into its exit status.
func exitLaunch(err error) int {
	var unresolved *UnresolvedError
	if !errors.As(err, &unresolved) {
		return fail(err)
	}
	fmt.Println(unresolved.Error())
	header := "launch.unresolved_next_steps"
	if unresolved.adoptBlocked() {
		header = "launch.unresolved_unmarked_next_steps"
	}
	fmt.Println(t(header))
	for _, step := range unresolved.NextSteps() {
		fmt.Println("  " + step)
	}
	if unresolved.hold != nil {
		unresolved.hold()
	}
	return exitUnresolved
}
