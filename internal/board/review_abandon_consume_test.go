package board

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
)

// An unpublished run is never consumed: nothing reads its findings, so an abandon
// retires a run no record depends on.
func TestUnpublishedRunIsRefusedByEveryConsumer(t *testing.T) {
	b := newAbandonBoard(t)
	run := b.unpublished(t, "never-consumed")
	const want = "incomplete publication"
	if err := AssignReviewFindings(b.root, ReviewAssignment{RunID: run.RunID, BatchID: run.BatchID, Author: "coordinator", Basis: "x", Items: map[string][]string{}}); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("assign: %v", err)
	}
	if err := InterpretReview(b.root, ReviewInterpretation{RunID: run.RunID, ReportHash: run.Hashes["report.md"], Author: "coordinator", Basis: "x", Complete: true}); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("interpret: %v", err)
	}
	d := ReviewDisposition{RecordID: "record-x", RunID: run.RunID, FindingID: "PMQA-001", BatchID: run.BatchID, TaskID: b.id, Author: "codex", ReportHash: run.Hashes["report.md"], Status: "fixed", Basis: "x"}
	if err := SubmitReviewDisposition(b.root, d, transactionSnapshot(t, b.root, b.id).Revision); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("disposition: %v", err)
	}
}

// plainRun is a successful run record with only the identity fields set.
func plainRun(id, role string) ReviewRun {
	var run ReviewRun
	run.RunID, run.Role, run.ExecutionStatus = id, role, "ok"
	return run
}

func abandonedRun(id, role string) ReviewRun {
	run := plainRun(id, role)
	run.Abandonment = &ReviewAbandonment{Decision: "d", Reason: "r"}
	return run
}

func TestChainHeadsIgnoreAbandonedRuns(t *testing.T) {
	root := plainRun("root", "PMQA")
	dead := abandonedRun("dead", "PMQA")
	if heads := reviewChainHeads([]ReviewRun{root, dead}, "PMQA"); !slices.Equal(heads, []string{"root"}) {
		t.Fatalf("an abandoned ok run is a chain head: %v", heads)
	}
	dead.PreviousRunID = "root"
	if heads := reviewChainHeads([]ReviewRun{root, dead}, "PMQA"); !slices.Equal(heads, []string{"root"}) {
		t.Fatalf("an abandoned run made its predecessor referenced: %v", heads)
	}
	batch := ReviewBatch{BatchID: "b", Requirements: archiveRequirements()}
	if actions := duplicateRunActions(batch, []ReviewRun{root, abandonedRun("twin", "PMQA")}); len(actions) != 0 {
		t.Fatalf("an abandoned run is a duplicate head: %+v", actions)
	}
	if actions := duplicateRunActions(batch, []ReviewRun{root, plainRun("twin", "PMQA")}); len(actions) != 1 {
		t.Fatalf("two live heads are no duplicate: %+v", actions)
	}
}

func TestUnlinkedRunsIgnoreAbandonedRuns(t *testing.T) {
	b := newAbandonBoard(t)
	good := b.published(t, "linked-run")
	lost := b.unpublished(t, "lost-twin")
	if err := AbandonReviewRunWithTransaction(b.root, abandonRequest(lost.RunID)); err != nil {
		t.Fatal(err)
	}
	unlinked, err := UnlinkedReviewRuns(b.root, good)
	if err != nil || len(unlinked) != 0 {
		t.Fatalf("an abandoned run is unlinked: %v %v", unlinked, err)
	}
}

func TestFixEvidenceIgnoresAnAbandonedSuccessor(t *testing.T) {
	run := plainRun("fixed-run", "PMQA")
	run.TaskIDs = []string{"task"}
	ref := DispatchFindingReference{FindingRef: FindingRef{RunID: run.RunID, FindingID: "PMQA-001"}}
	check := func(successor ReviewRun) []string {
		c := fixRunCheck{in: DispatchInput{TaskID: "task"}, runs: []ReviewRun{run, successor}}
		return c.problems(run, ref)
	}
	live := plainRun("later-run", "PMQA")
	live.PreviousRunID = run.RunID
	if problems := check(live); len(problems) != 1 || !strings.Contains(problems[0], "later-run") {
		t.Fatalf("a live successor does not supersede the round: %v", problems)
	}
	dead := live
	dead.Abandonment = &ReviewAbandonment{Decision: "d", Reason: "r"}
	if problems := check(dead); len(problems) != 0 {
		t.Fatalf("an abandoned successor superseded the round: %v", problems)
	}
}

func TestProgressAndPendingDispositionsSkipAnAbandonedRun(t *testing.T) {
	b := newAbandonBoard(t)
	b.published(t, "kept-run")
	lost := b.unpublished(t, "lost-run")
	if err := AbandonReviewRunWithTransaction(b.root, abandonRequest(lost.RunID)); err != nil {
		t.Fatal(err)
	}
	progress, err := ReviewTaskProgress(b.root, b.id)
	if err != nil {
		t.Fatalf("progress fails on an abandoned run: %v", err)
	}
	for _, item := range progress.Pending {
		if strings.Contains(item, lost.RunID) {
			t.Fatalf("progress still waits on the abandoned run: %v", progress.Pending)
		}
	}
	for _, action := range progress.NextActions {
		if slices.Contains(action.RunIDs, lost.RunID) {
			t.Fatalf("a next action names the abandoned run: %+v", action)
		}
	}
	var batch ReviewBatch
	err = WithTransaction(b.root, reviewScope([]string{b.id}, true), func(tx *Transaction) error {
		_, e := readReviewJSON(tx, reviewBatchName("batch"), &batch)
		if e != nil {
			return e
		}
		pending, e := checkPendingDispositions(tx, batch)
		if len(pending) != 0 {
			t.Fatalf("pending dispositions: %v", pending)
		}
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The readers the plan expects to stay as they are: an abandoned run counts as
// settled, has no assignment ledger, and still names its members.
func TestSettledReadersTreatAnAbandonedRunAsSettled(t *testing.T) {
	b := newAbandonBoard(t)
	lost := b.unpublished(t, "settled-run")
	if err := AbandonReviewRunWithTransaction(b.root, abandonRequest(lost.RunID)); err != nil {
		t.Fatal(err)
	}
	err := WithTransaction(b.root, reviewScope([]string{b.id}, true), func(tx *Transaction) error {
		if err := settledReviewBatch(tx, "batch"); err != nil {
			t.Errorf("settledReviewBatch: %v", err)
		}
		var batch ReviewBatch
		if _, err := readReviewJSON(tx, reviewBatchName("batch"), &batch); err != nil {
			return err
		}
		if err := requireAuthorDispositions(tx, batch); err != nil {
			t.Errorf("requireAuthorDispositions: %v", err)
		}
		owed, err := withdrawalsOwed(tx, batch)
		if err != nil || len(owed) != 0 {
			t.Errorf("withdrawalsOwed: %v %v", owed, err)
		}
		if !reviewRunComplete(transactionRun(t, tx, lost.RunID)) {
			t.Error("an abandoned run is not complete")
		}
		batchID, listed, err := unplannedReviewRun(tx, ReviewPlan{Batches: []ReviewPlanBatch{{BatchID: "batch"}}}, []string{b.id}, true)
		if err != nil || batchID != "" || !listed {
			t.Errorf("unplannedReviewRun: %q %v %v", batchID, listed, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func transactionRun(t *testing.T, tx *Transaction, runID string) ReviewRun {
	t.Helper()
	var run ReviewRun
	if ok, err := readReviewJSON(tx, reviewRunName(runID), &run); err != nil || !ok {
		t.Fatalf("run %s: %v %v", runID, ok, err)
	}
	return run
}

func TestFrozenTaskContextSnapshotSkipsAnAbandonedRun(t *testing.T) {
	b := newAbandonBoard(t)
	lost := b.unpublished(t, "context-run")
	snapshot := func() string {
		var path string
		err := WithTransaction(b.root, reviewScope([]string{b.id}, true), func(tx *Transaction) error {
			var batch ReviewBatch
			if _, err := readReviewJSON(tx, reviewBatchName("batch"), &batch); err != nil {
				return err
			}
			var err error
			path, err = frozenTaskContextSnapshot(tx, batch, "")
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return path
	}
	if snapshot() == "" {
		t.Fatal("the unpublished run is not a snapshot source")
	}
	if err := AbandonReviewRunWithTransaction(b.root, abandonRequest(lost.RunID)); err != nil {
		t.Fatal(err)
	}
	if path := snapshot(); path != "" {
		t.Fatalf("an abandoned run is a snapshot source: %s", path)
	}
}

func TestCoordinatorMirrorSkipsAnAbandonedRun(t *testing.T) {
	root := tempBoard(t)
	s := coordinatorCard(t, root, "abandoned-mirror")
	c := coordinatorClaim(t, root, s.Entry.TaskID)
	gatePlan(t, root, []string{s.Entry.TaskID}, archiveRequirements())
	run, _, err := PrepareReviewRun(root, archiveInput([]string{s.Entry.TaskID}, "mirror-run", "PMQA"), nil, nil, archiveOriginals(), "test")
	if err != nil {
		t.Fatal(err)
	}
	run.LaunchStatus, run.ExecutionStatus = "started", "ok"
	if run, err = FinalizeReviewRun(root, run, structuredReport(emptyFindings())); err != nil {
		t.Fatal(err)
	}
	if err = MoveEntryTo(t, root, s.Entry.TaskID, "working"); err != nil {
		t.Fatal(err)
	}
	if err = AbandonReviewRunWithTransaction(root, abandonRequest(run.RunID)); err != nil {
		t.Fatal(err)
	}
	c = coordinatorReconcile(t, root, c)
	if m := c.Members[s.Entry.TaskID]; m.Review == nil || len(m.Review.Runs) != 0 {
		t.Fatalf("the mirror kept the abandoned run: %+v", m.Review)
	}
}

// MoveEntryTo moves a card to state with the plain entry move.
func MoveEntryTo(t *testing.T, root, id, state string) error {
	t.Helper()
	s := transactionSnapshot(t, root, id)
	if s.Entry.State == state {
		return nil
	}
	_, err := MoveEntry(s.Entry, root, state)
	return err
}

func TestAbandonedStrayRunIsNotBoundByAResolution(t *testing.T) {
	s := defaultStrayBoard(t, "abandoned-stray")
	var lost ReviewRun
	withoutPlanPointer(t, s.root, s.ids, func() {
		run, _, err := PrepareReviewRun(s.root, strayRunInput(s.ids, strayBatchID, "stray-lost"), nil, nil, archiveOriginals(), "test")
		if err != nil {
			t.Fatal(err)
		}
		run.LaunchStatus, run.ExecutionStatus = "started", "ok"
		if lost, err = FinalizeReviewRun(s.root, run, structuredReport(emptyFindings())); err != nil {
			t.Fatal(err)
		}
	})
	if err := s.resolve(s.request(t, s.fixed())); err == nil {
		t.Fatal("the unpublished stray run did not block the resolution")
	}
	if err := AbandonReviewRunWithTransaction(s.root, ReviewAbandonRequest{RunID: lost.RunID, Decision: "d", Reason: "r"}); err != nil {
		t.Fatal(err)
	}
	s.mustResolve(t)
	data, err := os.ReadFile(control(s.root, "groups", reviewControlGroup, "plan-history", s.plan, fmt.Sprintf("%d.json", s.revision(t))))
	if err != nil {
		t.Fatal(err)
	}
	var history struct {
		Resolution ReviewBatchResolution `json:"resolution"`
	}
	if err = json.Unmarshal(data, &history); err != nil {
		t.Fatal(err)
	}
	if len(history.Resolution.Runs) != 1 || history.Resolution.Runs[0].RunID != s.run.RunID {
		t.Fatalf("the resolution bound the abandoned run: %+v", history.Resolution.Runs)
	}
}
