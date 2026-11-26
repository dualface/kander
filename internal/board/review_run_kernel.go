package board

import (
	"reflect"
	"slices"
	"strings"

	"github.com/dualface/kander/v2/internal/statemachine/reviewrun"
)

// The accepted and refused decisions of a review run (its preparation, phase
// updates, input artifacts, finalization, and per-card publication) are made by
// the generated kernel in internal/statemachine/reviewrun
// (formal/KanderFormal/Machines/ReviewRun.lean). This file projects a persisted
// run into a kernel state and runs kernel events. See docs/formal-verification.md.
//
// One kernel state describes one run as seen by one member: the run-level fields
// are the same for every member, and Published and Skipped are the member's own.
// Projection rules:
//   - A run that does not exist projects to the initial state.
//   - Phase, LaunchStatus, and ExecutionStatus project to the persisted values of
//     the kernel enums; any other string projects to Other, because
//     UpdateReviewRun stores the values it is given.
//   - ExitZero is whether ExitCode is 0, and Originals whether the run holds
//     original hashes.
//   - Prompt, Contract, and Evidence are whether the input artifact exists; only
//     the entrance that needs them reads them.
//   - Published and Skipped are the member's entries in Published and
//     SkippedMembers.
//   - Abandoned is whether the run records an Abandonment. An abandoned run is no
//     run of its batch: review_abandon.go lists what filters it out.
//
// Integrity facts (IDs, cards, batch binding, hashes, manifest, index lines)
// stay hand-written: an entrance computes them in the order it always has and
// returns the first failure, and the kernel receives them as true. The kernel
// decides what depends on the state of the run: its phase, whether the run
// exists, whether an artifact exists, and whether a member is published or
// skipped.
//
// UpdateReviewRun and FinalizeReviewRun write the run the caller presents. The
// kernel models the fields they advance (phase, launch status, execution result,
// exit code); the entrances still replace Published, SkippedMembers, Hashes,
// MemberCycles, and Abandonment with the caller's values, which no production
// caller changes.

var kernelRunPhases = map[string]reviewrun.Phase{
	"prepared": reviewrun.PhasePrepared, "launching": reviewrun.PhaseLaunching, "running": reviewrun.PhaseRunning,
	"collecting": reviewrun.PhaseCollecting, "cleaning": reviewrun.PhaseCleaning, "finalized": reviewrun.PhaseFinalized,
}

var kernelRunLaunches = map[string]reviewrun.Launch{
	"not_started": reviewrun.LaunchNotStarted, "unknown": reviewrun.LaunchUnknown, "started": reviewrun.LaunchStarted,
}

var kernelRunExecs = map[string]reviewrun.Exec{
	"incomplete": reviewrun.ExecIncomplete, "ok": reviewrun.ExecOk, "not_started": reviewrun.ExecNotStarted,
	"failed": reviewrun.ExecFailed, "interrupted": reviewrun.ExecInterrupted,
}

func kernelRunPhase(phase string) reviewrun.Phase {
	if p, ok := kernelRunPhases[phase]; ok {
		return p
	}
	return reviewrun.PhaseOther
}

func kernelRunLaunch(launch string) reviewrun.Launch {
	if l, ok := kernelRunLaunches[launch]; ok {
		return l
	}
	return reviewrun.LaunchOther
}

func kernelRunExec(exec string) reviewrun.Exec {
	if e, ok := kernelRunExecs[exec]; ok {
		return e
	}
	return reviewrun.ExecOther
}

// reviewRunPhaseName is the persisted phase of a kernel phase that production
// writes; a kernel phase production never writes names no run.
func reviewRunPhaseName(phase reviewrun.Phase) string {
	for name, p := range kernelRunPhases {
		if p == phase {
			return name
		}
	}
	return ""
}

func reviewRunLaunchName(launch reviewrun.Launch) string {
	for name, l := range kernelRunLaunches {
		if l == launch {
			return name
		}
	}
	return ""
}

func reviewRunExecName(exec reviewrun.Exec) string {
	for name, e := range kernelRunExecs {
		if e == exec {
			return name
		}
	}
	return ""
}

// projectReviewRun is the kernel state of run as member sees it. With exists
// false the run is absent and run is ignored.
func projectReviewRun(run ReviewRun, exists bool, member string) reviewrun.State {
	st := reviewrun.Initial(reviewrun.Params{})
	if !exists {
		return st
	}
	st.Phase, st.Launch, st.Exec = kernelRunPhase(run.Phase), kernelRunLaunch(run.LaunchStatus), kernelRunExec(run.ExecutionStatus)
	st.ExitZero, st.Originals = run.ExitCode == 0, len(run.Hashes) > 0
	st.Published, st.Skipped = run.Published[member], slices.Contains(run.SkippedMembers, member)
	st.Abandoned = run.Abandonment != nil
	return st
}

// reviewArtifactNames are the input artifacts a run stores before launch.
var reviewArtifactNames = []string{"prompt.txt", "review-contract.md", "evidence.txt"}

// projectReviewArtifacts adds the input artifacts the run holds to st.
func projectReviewArtifacts(tx *Transaction, runID string, st reviewrun.State) (reviewrun.State, error) {
	for _, name := range reviewArtifactNames {
		_, exists, err := tx.ReadGroup(reviewControlGroup, "runs/"+runID+"/inputs/"+name)
		if err != nil {
			return st, err
		}
		switch name {
		case "prompt.txt":
			st.Prompt = exists
		case "review-contract.md":
			st.Contract = exists
		default:
			st.Evidence = exists
		}
	}
	return st, nil
}

func stepReviewRun(st reviewrun.State, e reviewrun.Event) (reviewrun.State, bool) {
	return reviewrun.Step(st, e, reviewrun.Params{})
}

// reviewRunComplete reports that the run is finalized and every member is
// published or skipped.
func reviewRunComplete(run ReviewRun) bool {
	for _, id := range run.TaskIDs {
		if !reviewrun.Complete(projectReviewRun(run, true, id), reviewrun.Params{}) {
			return false
		}
	}
	return len(run.TaskIDs) > 0
}

// reviewRunFinal reports that the run exists and is finalized.
func reviewRunFinal(run ReviewRun, exists bool) bool {
	return reviewrun.Final(projectReviewRun(run, exists, ""), reviewrun.Params{})
}

// retryReviewRun is the decision of a same-ID call of Prepare on an existing
// run: nothing is written and no reviewer starts. The kernel accepts the retry
// when the frozen input, the batch, and the requirements match; the first fact
// that fails names the error.
func retryReviewRun(tx *Transaction, run ReviewRun, input ReviewInput, requirements map[string]string) error {
	same := run.Schema == 1 && reflect.DeepEqual(input, run.ReviewInput)
	known, requirementsOk := false, true
	if same {
		var batch ReviewBatch
		ok, e := readReviewJSON(tx, reviewBatchName(input.BatchID), &batch)
		if e != nil {
			return e
		}
		known = ok
		requirementsOk = !ok || len(requirements) == 0 || reflect.DeepEqual(requirements, batch.Requirements)
	}
	retry := reviewrun.Retry{Ids: true, Cards: true, SameInput: same, BatchKnown: known, ReqOk: requirementsOk}
	if _, ok := stepReviewRun(projectReviewRun(run, true, ""), retry); ok {
		return nil
	}
	switch {
	case !same:
		return reviewError(input.RunID + ": input conflict: " + t("board.review_run_input_conflict"))
	case reviewRunAbandoned(run):
		return abandonedRunError(run.RunID)
	case !known:
		return reviewError("missing batch")
	}
	return reviewError("requirements conflict")
}

// createReviewRun is the state of a run that Prepare creates.
func createReviewRun(input ReviewInput, version, createdAt string, cycles map[string]string) (ReviewRun, error) {
	next, ok := stepReviewRun(projectReviewRun(ReviewRun{}, false, ""), reviewrun.Create{Ids: true, Cards: true, Batch: true, Role: true})
	if !ok {
		return ReviewRun{}, reviewError(input.RunID + ": run exists")
	}
	return ReviewRun{Schema: 1, ReviewInput: input, KanderVersion: version, Phase: reviewRunPhaseName(next.Phase),
		LaunchStatus: reviewRunLaunchName(next.Launch), ExecutionStatus: reviewRunExecName(next.Exec),
		SemanticStatus: "unassessed", CreatedAt: createdAt, Hashes: map[string]string{}, Published: map[string]bool{},
		MemberCycles: cycles}, nil
}

// updateReviewRunAllowed is the decision of UpdateReviewRun.
func updateReviewRunAllowed(old ReviewRun, exists bool, run ReviewRun) bool {
	_, ok := stepReviewRun(projectReviewRun(old, exists, ""), reviewrun.Update{
		ToPhase: kernelRunPhase(run.Phase), Launch: kernelRunLaunch(run.LaunchStatus), Exec: kernelRunExec(run.ExecutionStatus),
		ExitZero: run.ExitCode == 0, SameInput: exists && reflect.DeepEqual(old.ReviewInput, run.ReviewInput)})
	return ok
}

// storeReviewArtifactEvent is the kernel event of storing the input artifact
// name when the stored bytes are the same.
func storeReviewArtifactEvent(name string, same bool) reviewrun.Event {
	switch name {
	case "prompt.txt":
		return reviewrun.StorePrompt{Same: same}
	case "review-contract.md":
		return reviewrun.StoreContract{Same: same}
	}
	return reviewrun.StoreEvidence{Same: same}
}

// finalizeReviewRun decides the finalization of old into *run. It checks the
// facts in the order the entrance always has and returns the first failure; the
// kernel accepts a finalization only when every fact holds. report is the
// semantic report, nil when there is none. rewrite turns an ok run whose
// structured report fails into a failed one and reports whether it did;
// originals reads the originals and returns the error of a missing one apart
// from a read error.
func finalizeReviewRun(old ReviewRun, exists bool, run *ReviewRun, report []byte, rewrite func() bool,
	originals func() (missing, err error)) (reviewrun.State, error) {
	st := projectReviewRun(old, exists, "")
	facts := reviewrun.Finalize{
		Launch: kernelRunLaunch(run.LaunchStatus), Exec: kernelRunExec(run.ExecutionStatus), ExitZero: run.ExitCode == 0,
		ReportOk: len(report) > 0 && !(run.FindingsSchema == FindingsSchemaReceiver && strings.TrimSpace(string(report)) == ""),
		Reason:   run.FailureReason != "", Complete: true,
		SameInput: exists && reflect.DeepEqual(old.ReviewInput, run.ReviewInput),
	}
	// A probe holds every fact the checked one does not name, so one fact at a
	// time can refuse it.
	probe := func(f reviewrun.Finalize) bool { _, ok := stepReviewRun(st, f); return ok }
	if !probe(reviewrun.Finalize{Launch: reviewrun.LaunchStarted, Exec: reviewrun.ExecIncomplete, ReportOk: true,
		Reason: true, Complete: true, SameInput: facts.SameInput}) {
		return st, reviewError("finalize conflict")
	}
	okFacts := facts
	okFacts.Reason = true
	if okFacts.Exec != reviewrun.ExecOk {
		okFacts.ExitZero = false
	}
	if !probe(okFacts) {
		return st, reviewError("invalid ok finalization")
	}
	facts.Invalid = rewrite()
	if !probe(facts) {
		return st, reviewError("missing failure facts")
	}
	missing, err := originals()
	if err != nil {
		return st, err
	}
	facts.Complete = missing == nil
	next, ok := stepReviewRun(st, facts)
	if !ok {
		return st, firstError(missing, reviewError("finalize conflict"))
	}
	return next, nil
}
