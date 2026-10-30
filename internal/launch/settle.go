package launch

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/liveness"
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
	// hold keeps a foreground executor attached to this terminal.
	hold func()
}

// Error is the fixed line scripts match on.
func (e *UnresolvedError) Error() string {
	return fmt.Sprintf("started-unresolved: %s window=%s reason=%s", e.TaskID, orNA(e.Window), e.Reason)
}

// NextSteps names the two ways out. A retried start is not one of them.
func (e *UnresolvedError) NextSteps() []string {
	return []string{
		fmt.Sprintf("%s resume %s --adopt --decision <reference>", e.Command, e.TaskID),
		fmt.Sprintf("%s resume %s --agent <agent> --decision <reference> --message <message>", e.Command, e.TaskID),
	}
}

func isUnresolved(err error) bool {
	var unresolved *UnresolvedError
	return errors.As(err, &unresolved)
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
	fmt.Println(t("launch.unresolved_next_steps"))
	for _, step := range unresolved.NextSteps() {
		fmt.Println("  " + step)
	}
	if unresolved.hold != nil {
		unresolved.hold()
	}
	return exitUnresolved
}

// requireTakeoverDecision refuses a takeover of an unfinished start without a user
// decision, before a dispatch binds. board.BeginTakeover enforces the same rule.
func requireTakeoverDecision(root, task, decision string) error {
	record, exists, err := board.CurrentExecution(root, task)
	if err != nil {
		return err
	}
	if exists && record.StartUnfinished() && strings.TrimSpace(decision) == "" {
		return launchError("board.takeover_decision_required", task, record.Status)
	}
	return nil
}

// adoptLivenessFn observes the recorded address only: a pane found elsewhere by
// reverse lookup is not the executor this record started.
var adoptLivenessFn = func(s board.Snapshot) liveness.Report {
	return liveness.ClassifyTaskLookupContext(context.Background(), s.Entry, s.Text, false)
}

// commandAdopt keeps the running executor of an unfinished start. Alive is the only
// accepted observation: an unknown executor is never adopted, and foreground or
// console cards have no probeable address yet, so they are refused.
func commandAdopt(root, task, decision string) error {
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		return err
	}
	if s.Entry.State != "review" && s.Entry.State != "working" {
		return launchError("launch.only_tasks_in_review_or_working_can_be_resumed", s.Entry.TaskID, s.Entry.State)
	}
	cfg, err := loadEffective()
	if err != nil {
		return err
	}
	if err = cfg.Rules.CheckTaskGroup(board.TaskGroupFrom(s.Text)); err != nil {
		return err
	}
	record, err := board.BindExecutionCursor(root, s.Entry)
	if err != nil {
		return err
	}
	if !record.StartUnfinished() {
		return launchError("board.adopt_needs_unfinished_start", s.Entry.TaskID, record.Status)
	}
	if report := adoptLivenessFn(s); report.Status != liveness.Alive {
		return launchError("launch.adopt_liveness_unconfirmed", s.Entry.TaskID, report.Status, report.Detail)
	}
	if _, err = board.AdoptExecution(root, s, decision); err != nil {
		return err
	}
	fmt.Println(t("launch.adopted", s.Entry.TaskID, orNA(board.MetadataFrom(s.Text, board.FieldWindow))))
	return nil
}
