package board

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/statemachine/reviewrun"
)

// runStates is the kernel state of one run as each member sees it, with the
// input artifacts the run holds.
func runStates(t *testing.T, root, runID string, ids []string) map[string]reviewrun.State {
	t.Helper()
	run, exists, err := LookupReviewRun(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]reviewrun.State{}
	err = WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
		for _, id := range ids {
			st := projectReviewRun(run, exists, id)
			if exists {
				var e error
				if st, e = projectReviewArtifacts(tx, runID, st); e != nil {
					return e
				}
			}
			states[id] = st
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return states
}

// refineRun runs action and asserts that the run now projects to the kernel step
// of its projection from before, per member. events names the kernel event of
// each member.
func refineRun(t *testing.T, root, runID string, ids []string, events map[string]reviewrun.Event, action func() error) {
	t.Helper()
	before := runStates(t, root, runID, ids)
	if err := action(); err != nil {
		t.Fatal(err)
	}
	after := runStates(t, root, runID, ids)
	for _, id := range ids {
		want, ok := reviewrun.Step(before[id], events[id], reviewrun.Params{})
		if !ok {
			t.Fatalf("%s: the kernel refuses %+v on %+v", id, events[id], before[id])
		}
		if after[id] != want {
			t.Fatalf("%s: projection after %+v, kernel step %+v", id, after[id], want)
		}
	}
}

// refuseRun runs action, which must fail, and asserts that the kernel refuses the
// event on every projection and that the projection did not change.
func refuseRun(t *testing.T, root, runID string, ids []string, event reviewrun.Event, action func() error) {
	t.Helper()
	before := runStates(t, root, runID, ids)
	if err := action(); err == nil {
		t.Fatal("the entrance accepted a request the kernel refuses")
	}
	for _, id := range ids {
		if next, ok := reviewrun.Step(before[id], event, reviewrun.Params{}); ok || next != before[id] {
			t.Fatalf("%s: the kernel accepts %+v on %+v", id, event, before[id])
		}
	}
	if after := runStates(t, root, runID, ids); !reflect.DeepEqual(after, before) {
		t.Fatalf("a refused entrance changed the run: %+v -> %+v", before, after)
	}
}

func allMembers(ids []string, e reviewrun.Event) map[string]reviewrun.Event {
	events := map[string]reviewrun.Event{}
	for _, id := range ids {
		events[id] = e
	}
	return events
}

func okFinalize() reviewrun.Finalize {
	return reviewrun.Finalize{Launch: reviewrun.LaunchStarted, Exec: reviewrun.ExecOk, ExitZero: true, ReportOk: true, Complete: true, SameInput: true}
}

// The run entrances refine the kernel: each accepted entrance leaves every
// member's projection equal to the kernel step of the projection before, and each
// refused one changes nothing, for the preparation (new and same-ID retry), the
// phase update, the input artifacts, and the finalization.
func TestReviewRunEntrancesRefineKernel(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "run-kernel-a"), gateCard(t, root, "run-kernel-b")}
	gatePlan(t, root, ids, archiveRequirements())
	input := archiveInput(ids, "kernel-run", "PMQA")
	prepare := func(in ReviewInput, files map[string][]byte) func() error {
		return func() error { _, _, err := PrepareReviewRun(root, in, nil, nil, files, "test"); return err }
	}
	refineRun(t, root, "kernel-run", ids, allMembers(ids, reviewrun.Create{Ids: true, Cards: true, Batch: true, Role: true}), prepare(input, archiveOriginals()))

	// A retry of the same ID writes nothing; a changed input is refused.
	retry := reviewrun.Retry{Ids: true, Cards: true, SameInput: true, BatchKnown: true, ReqOk: true}
	refineRun(t, root, "kernel-run", ids, allMembers(ids, retry), prepare(input, archiveOriginals()))
	changed := input
	changed.Commit = archiveInput(nil, "", "").Base
	retry.SameInput = false
	refuseRun(t, root, "kernel-run", ids, retry, prepare(changed, archiveOriginals()))

	// An input artifact is stored while the run is prepared; the same bytes again
	// pass and other bytes are refused.
	store := func(data string) func() error {
		return func() error { return StoreReviewArtifact(root, "kernel-run", "prompt.txt", []byte(data)) }
	}
	refineRun(t, root, "kernel-run", ids, allMembers(ids, reviewrun.StorePrompt{Same: false}), store("prompt"))
	refineRun(t, root, "kernel-run", ids, allMembers(ids, reviewrun.StorePrompt{Same: true}), store("prompt"))
	refuseRun(t, root, "kernel-run", ids, reviewrun.StorePrompt{Same: false}, store("other"))
	storeNamed := func(name, data string) func() error {
		return func() error { return StoreReviewArtifact(root, "kernel-run", name, []byte(data)) }
	}
	refineRun(t, root, "kernel-run", ids, allMembers(ids, reviewrun.StoreContract{Same: false}), storeNamed("review-contract.md", "contract"))
	refineRun(t, root, "kernel-run", ids, allMembers(ids, reviewrun.StoreContract{Same: true}), storeNamed("review-contract.md", "contract"))
	refuseRun(t, root, "kernel-run", ids, reviewrun.StoreContract{Same: false}, storeNamed("review-contract.md", "other"))
	refineRun(t, root, "kernel-run", ids, allMembers(ids, reviewrun.StoreEvidence{Same: false}), storeNamed("evidence.txt", "evidence"))
	refineRun(t, root, "kernel-run", ids, allMembers(ids, reviewrun.StoreEvidence{Same: true}), storeNamed("evidence.txt", "evidence"))
	refuseRun(t, root, "kernel-run", ids, reviewrun.StoreEvidence{Same: false}, storeNamed("evidence.txt", "other"))

	// The phase updates of the driver, and a roll back to prepared.
	update := func(phase, launch string) func() error {
		return func() error {
			run, err := ReadReviewRun(root, "kernel-run")
			if err != nil {
				return err
			}
			run.Phase, run.LaunchStatus = phase, launch
			return UpdateReviewRun(root, run)
		}
	}
	step := func(p reviewrun.Phase, l reviewrun.Launch) reviewrun.Update {
		return reviewrun.Update{ToPhase: p, Launch: l, Exec: reviewrun.ExecIncomplete, ExitZero: true, SameInput: true}
	}
	refineRun(t, root, "kernel-run", ids, allMembers(ids, step(reviewrun.PhaseLaunching, reviewrun.LaunchUnknown)), update("launching", "unknown"))
	refineRun(t, root, "kernel-run", ids, allMembers(ids, step(reviewrun.PhaseRunning, reviewrun.LaunchStarted)), update("running", "started"))
	refineRun(t, root, "kernel-run", ids, allMembers(ids, step(reviewrun.PhaseCollecting, reviewrun.LaunchStarted)), update("collecting", "started"))
	refineRun(t, root, "kernel-run", ids, allMembers(ids, step(reviewrun.PhasePrepared, reviewrun.LaunchNotStarted)), update("prepared", "not_started"))
	refineRun(t, root, "kernel-run", ids, allMembers(ids, step(reviewrun.PhaseOther, reviewrun.LaunchOther)), update("bogus", "bogus"))
	refineRun(t, root, "kernel-run", ids, allMembers(ids, step(reviewrun.PhaseRunning, reviewrun.LaunchStarted)), update("running", "started"))
	refuseRun(t, root, "kernel-run", ids, reviewrun.StorePrompt{Same: true}, store("prompt"))

	// A finalization that is ok without a report, or failed without a reason, is
	// refused; an ok one writes the originals.
	finalize := func(report string, mutate func(*ReviewRun)) func() error {
		return func() error {
			run, err := ReadReviewRun(root, "kernel-run")
			if err != nil {
				return err
			}
			run.LaunchStatus, run.ExecutionStatus, run.ExitCode = "started", "ok", 0
			if mutate != nil {
				mutate(&run)
			}
			_, err = FinalizeReviewRun(root, run, []byte(report))
			return err
		}
	}
	noReport := okFinalize()
	noReport.ReportOk = false
	refuseRun(t, root, "kernel-run", ids, noReport, finalize("", nil))
	notStarted := okFinalize()
	notStarted.Launch = reviewrun.LaunchNotStarted
	refuseRun(t, root, "kernel-run", ids, notStarted, finalize("report", func(r *ReviewRun) { r.LaunchStatus = "not_started" }))
	noReason := reviewrun.Finalize{Launch: reviewrun.LaunchStarted, Exec: reviewrun.ExecFailed, ReportOk: true, Complete: true, SameInput: true}
	refuseRun(t, root, "kernel-run", ids, noReason, finalize("report", func(r *ReviewRun) { r.ExecutionStatus, r.ExitCode = "failed", 1 }))
	refineRun(t, root, "kernel-run", ids, allMembers(ids, okFinalize()), finalize("PASS\n", nil))

	// A finalized run is frozen: neither an update nor a second finalization passes.
	frozen := step(reviewrun.PhaseRunning, reviewrun.LaunchStarted)
	refuseRun(t, root, "kernel-run", ids, frozen, update("running", "started"))
	refuseRun(t, root, "kernel-run", ids, okFinalize(), finalize("PASS\n", nil))
}

// An ok run whose structured report fails is finalized as failed with a non-zero
// exit code, as the kernel's invalid fact says.
func TestReviewRunStructuredReportFailureRefinesKernel(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "run-kernel-bad")}
	gatePlan(t, root, ids, archiveRequirements())
	input := archiveInput(ids, "bad-run", "PMQA")
	input.FindingsSchema = 1
	if _, _, err := PrepareReviewRun(root, input, nil, nil, archiveOriginals(), "test"); err != nil {
		t.Fatal(err)
	}
	invalid := okFinalize()
	invalid.Invalid = true
	refineRun(t, root, "bad-run", ids, allMembers(ids, invalid), func() error {
		run, err := ReadReviewRun(root, "bad-run")
		if err != nil {
			return err
		}
		run.LaunchStatus, run.ExecutionStatus = "started", "ok"
		run, err = FinalizeReviewRun(root, run, []byte("not a structured report"))
		if err == nil && (run.ExecutionStatus != "failed" || run.ExitCode != 1) {
			t.Fatalf("%+v", run)
		}
		return err
	})
}

// The publication entrances refine the kernel per member: a member takes a copy
// and is published, a member that left is published and skipped without a copy,
// and a retry only verifies; a run that is not finalized publishes nothing.
func TestReviewRunPublicationRefinesKernel(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "pub-kernel-stays"), gateCard(t, root, "pub-kernel-leaves")}
	gatePlan(t, root, ids, archiveRequirements())
	if _, _, err := PrepareReviewRun(root, archiveInput(ids, "pub-kernel", "PMQA"), nil, nil, archiveOriginals(), "test"); err != nil {
		t.Fatal(err)
	}
	stageFacts := reviewrun.Stage{SameInput: true, GroupOk: true, MemberOk: true, Integrity: true}
	refuseRun(t, root, "pub-kernel", ids, stageFacts, func() error {
		_, _, err := PublishReviewRun(root, "pub-kernel")
		return err
	})
	refuseRun(t, root, "pub-kernel", ids, stageFacts, func() error {
		_, err := PublishReviewRunAtomic(root, "pub-kernel")
		return err
	})
	run, err := ReadReviewRun(root, "pub-kernel")
	if err != nil {
		t.Fatal(err)
	}
	run.LaunchStatus, run.ExecutionStatus = "started", "ok"
	if _, err = FinalizeReviewRun(root, run, structuredReport(emptyFindings())); err != nil {
		t.Fatal(err)
	}
	deadEndExit(t, root, ids[1], "archived")
	events := map[string]reviewrun.Event{
		ids[0]: stageFacts,
		ids[1]: reviewrun.Skip{SameInput: true, GroupOk: true, Excluded: true},
	}
	refineRun(t, root, "pub-kernel", ids, events, func() error {
		_, failures, err := PublishReviewRun(root, "pub-kernel")
		if len(failures) > 0 {
			t.Fatal(failures)
		}
		return err
	})
	states := runStates(t, root, "pub-kernel", ids)
	if !states[ids[0]].Published || states[ids[0]].Skipped || !states[ids[1]].Published || !states[ids[1]].Skipped {
		t.Fatalf("%+v", states)
	}
	// A published member is frozen: another publication verifies and a skipped
	// member passes, in both entrances.
	again := map[string]reviewrun.Event{
		ids[0]: reviewrun.Verify{SameInput: true, GroupOk: true, Integrity: true, Verified: true},
		ids[1]: reviewrun.PassSkipped{SameInput: true, GroupOk: true},
	}
	refineRun(t, root, "pub-kernel", ids, again, func() error {
		_, failures, err := PublishReviewRun(root, "pub-kernel")
		if len(failures) > 0 {
			t.Fatal(failures)
		}
		return err
	})
	refineRun(t, root, "pub-kernel", ids, again, func() error {
		_, err := PublishReviewRunAtomic(root, "pub-kernel")
		return err
	})
	if err := ReviewPublicationComplete(root, "pub-kernel"); err != nil {
		t.Fatal(err)
	}
}

// The atomic publication is the first publication of a finalized run: every
// member stages, or skips when it left, in one transaction, and a refused
// atomic publication writes nothing.
func TestReviewRunAtomicPublicationRefinesKernel(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "atomic-kernel-stays"), gateCard(t, root, "atomic-kernel-leaves")}
	gatePlan(t, root, ids, archiveRequirements())
	if _, _, err := PrepareReviewRun(root, archiveInput(ids, "atomic-kernel", "PMQA"), nil, nil, archiveOriginals(), "test"); err != nil {
		t.Fatal(err)
	}
	run, err := ReadReviewRun(root, "atomic-kernel")
	if err != nil {
		t.Fatal(err)
	}
	run.LaunchStatus, run.ExecutionStatus = "started", "ok"
	if _, err = FinalizeReviewRun(root, run, structuredReport(emptyFindings())); err != nil {
		t.Fatal(err)
	}
	deadEndExit(t, root, ids[1], "archived")
	events := map[string]reviewrun.Event{
		ids[0]: reviewrun.Stage{SameInput: true, GroupOk: true, MemberOk: true, Integrity: true},
		ids[1]: reviewrun.Skip{SameInput: true, GroupOk: true, Excluded: true},
	}
	refineRun(t, root, "atomic-kernel", ids, events, func() error {
		_, err := PublishReviewRunAtomic(root, "atomic-kernel")
		return err
	})
	if err = ReviewPublicationComplete(root, "atomic-kernel"); err != nil {
		t.Fatal(err)
	}
}

// The uploaded finalization of a service run shares the transaction of the
// finalization, so it refines the same event; a second one is refused. The
// originals of a run are readable only once it is finalized.
func TestFinalizeUploadedReviewAndOriginalsRefineKernel(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "upload-kernel")}
	gatePlan(t, root, ids, archiveRequirements())
	if _, _, err := PrepareReviewRun(root, archiveInput(ids, "upload-kernel", "PMQA"), nil, nil, archiveOriginals(), "test"); err != nil {
		t.Fatal(err)
	}
	read := func() error { _, err := ReadReviewOriginal(root, "upload-kernel", "report.md"); return err }
	// The refusal is the finalization guard, not a missing hash: without the guard
	// the same call fails with os.ErrNotExist, so the error text names the guard.
	before := runStates(t, root, "upload-kernel", ids)
	if err := read(); err == nil || !strings.Contains(err.Error(), "run not finalized") || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an original of a run that is not finalized: %v", err)
	}
	if reviewrun.Final(before[ids[0]], reviewrun.Params{}) {
		t.Fatal("the kernel reads the originals of a run that is not finalized")
	}
	if after := runStates(t, root, "upload-kernel", ids); !reflect.DeepEqual(after, before) {
		t.Fatalf("a refused read changed the run: %+v -> %+v", before, after)
	}
	files := map[string][]byte{"output.raw": []byte("raw"), "stdout.log": []byte("out"), "error.log": []byte("err")}
	upload := func() error {
		run, err := ReadReviewRun(root, "upload-kernel")
		if err != nil {
			return err
		}
		run.LaunchStatus, run.ExecutionStatus = "started", "ok"
		_, err = FinalizeUploadedReview(root, run, []byte("PASS\n"), files)
		return err
	}
	refineRun(t, root, "upload-kernel", ids, allMembers(ids, okFinalize()), upload)
	refuseRun(t, root, "upload-kernel", ids, okFinalize(), upload)
	if err := read(); err != nil {
		t.Fatal(err)
	}
	if !reviewrun.Final(runStates(t, root, "upload-kernel", ids)[ids[0]], reviewrun.Params{}) {
		t.Fatal("the kernel refuses to read the originals of a finalized run")
	}
}

// UpdateReviewRun and FinalizeReviewRun replace the run with the one the caller
// presents, including its Published and SkippedMembers: the kernel models only
// the fields the entrances advance, and no production caller changes the others.
// This test pins the known difference recorded in docs/formal-verification.md
// (M6).
func TestReviewRunEntrancesReplacePublicationFields(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "run-kernel-forged")}
	gatePlan(t, root, ids, archiveRequirements())
	run, _, err := PrepareReviewRun(root, archiveInput(ids, "forged-run", "PMQA"), nil, nil, archiveOriginals(), "test")
	if err != nil {
		t.Fatal(err)
	}
	run.Published = map[string]bool{ids[0]: true}
	run.SkippedMembers = []string{ids[0]}
	if err = UpdateReviewRun(root, run); err != nil {
		t.Fatal(err)
	}
	got, err := ReadReviewRun(root, "forged-run")
	if err != nil || got.Phase != "prepared" || !got.Published[ids[0]] || len(got.SkippedMembers) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	// A finalization stores the publication fields it is given too.
	run.Published, run.SkippedMembers = map[string]bool{}, nil
	if err = UpdateReviewRun(root, run); err != nil {
		t.Fatal(err)
	}
	run.LaunchStatus, run.ExecutionStatus = "started", "ok"
	run.Published, run.SkippedMembers = map[string]bool{ids[0]: true}, []string{ids[0]}
	if run, err = FinalizeReviewRun(root, run, []byte("PASS\n")); err != nil {
		t.Fatal(err)
	}
	got, err = ReadReviewRun(root, "forged-run")
	if err != nil || got.Phase != "finalized" || !got.Published[ids[0]] || len(got.SkippedMembers) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
}
