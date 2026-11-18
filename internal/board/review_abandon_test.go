package board

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// abandonBoard is a one-card board with a sealed plan that requires PMQA. Its
// runs are finalized without being published, the state an abandon retires.
type abandonBoard struct {
	root string
	id   string
}

func newAbandonBoard(t *testing.T) abandonBoard {
	t.Helper()
	root := tempBoard(t)
	id := gateCard(t, root, "abandon-card")
	gatePlan(t, root, []string{id}, archiveRequirements())
	return abandonBoard{root: root, id: id}
}

// unpublished finalizes a successful run of the batch without publishing it.
func (b abandonBoard) unpublished(t *testing.T, runID string) ReviewRun {
	t.Helper()
	input := archiveInput([]string{b.id}, runID, "PMQA")
	run, _, err := PrepareReviewRun(b.root, input, nil, nil, archiveOriginals(), "test")
	if err != nil {
		t.Fatal(err)
	}
	run.LaunchStatus, run.ExecutionStatus = "started", "ok"
	run, err = FinalizeReviewRun(b.root, run, structuredReport(emptyFindings()))
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func (b abandonBoard) published(t *testing.T, runID string) ReviewRun {
	t.Helper()
	run := b.unpublished(t, runID)
	run = publishRun(t, b.root, run.RunID)
	assignGate(t, b.root, run, map[string][]string{})
	return run
}

func abandonRequest(runID string) ReviewAbandonRequest {
	return ReviewAbandonRequest{RunID: runID, Decision: "user decision 2026-11-19", Reason: "the writer secret was lost with the client host"}
}

// snapshotFiles lists every file of the card directory with its bytes.
func snapshotFiles(t *testing.T, root, id string) map[string]string {
	t.Helper()
	files := map[string]string{}
	dir := transactionSnapshot(t, root, id).Entry.Path
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		rel, _ := filepath.Rel(dir, path)
		files[rel] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// runRecord is the stored run.json of runID.
func runRecord(t *testing.T, root, runID string) string {
	t.Helper()
	data, err := os.ReadFile(control(root, "groups", reviewControlGroup, "runs", runID, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// abandonTouched is what an abandon that was refused must leave alone.
type abandonTouched struct {
	run   string
	card  Snapshot
	files map[string]string
}

func (b abandonBoard) touched(t *testing.T, runID string) abandonTouched {
	t.Helper()
	return abandonTouched{run: runRecord(t, b.root, runID), card: transactionSnapshot(t, b.root, b.id), files: snapshotFiles(t, b.root, b.id)}
}

func (b abandonBoard) requireUntouched(t *testing.T, runID string, before abandonTouched) {
	t.Helper()
	after := b.touched(t, runID)
	if after.run != before.run || after.card.Revision != before.card.Revision || !reflect.DeepEqual(after.files, before.files) {
		t.Fatalf("a refused abandon wrote: run %v, card revision %d to %d", after.run != before.run, before.card.Revision, after.card.Revision)
	}
}

func TestAbandonRetiresARunThatWasNeverPublished(t *testing.T) {
	b := newAbandonBoard(t)
	run := b.unpublished(t, "lost-writer")
	if _, err := PublishReviewDisposition(b.root, "batch"); err == nil || !strings.Contains(err.Error(), "lost-writer: incomplete publication") {
		t.Fatalf("an unpublished run did not block the aggregation: %v", err)
	}
	before := b.touched(t, run.RunID)
	if err := AbandonReviewRunWithTransaction(b.root, abandonRequest(run.RunID)); err != nil {
		t.Fatal(err)
	}
	stored, err := ReadReviewRun(b.root, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Abandonment == nil || stored.Abandonment.Decision != "user decision 2026-11-19" || stored.Abandonment.Reason == "" || stored.Abandonment.AbandonedAt == "" {
		t.Fatalf("the abandonment is not recorded: %+v", stored.Abandonment)
	}
	if !stored.Published[b.id] || !containsID(stored.SkippedMembers, b.id) {
		t.Fatalf("the member is not settled: %+v", stored)
	}
	after := b.touched(t, run.RunID)
	if after.card.Revision != before.card.Revision || !reflect.DeepEqual(after.files, before.files) {
		t.Fatal("the abandon wrote into the member card")
	}
	if indexes, err := ParseReviewIndexes(after.card.Text); err != nil || len(indexes) != 0 {
		t.Fatalf("REVIEWS has rows after an abandon: %v %v", indexes, err)
	}
}

func TestAbandonRefusalsWriteNothing(t *testing.T) {
	tests := map[string]struct {
		setup   func(t *testing.T, b abandonBoard) string
		request func(runID string) ReviewAbandonRequest
		want    string
	}{
		"blank decision": {
			request: func(id string) ReviewAbandonRequest { r := abandonRequest(id); r.Decision = " \t"; return r },
			want:    "non-empty decision",
		},
		"blank reason": {
			request: func(id string) ReviewAbandonRequest { r := abandonRequest(id); r.Reason = "\n"; return r },
			want:    "non-empty decision",
		},
		"run not finalized": {
			setup: func(t *testing.T, b abandonBoard) string {
				run, _, err := PrepareReviewRun(b.root, archiveInput([]string{b.id}, "open-run", "PMQA"), nil, nil, archiveOriginals(), "test")
				if err != nil {
					t.Fatal(err)
				}
				return run.RunID
			},
			want: "only a finalized run",
		},
		"a member holds a copy": {
			setup: func(t *testing.T, b abandonBoard) string { return b.published(t, "published-run").RunID },
			want:  "already holds a copy",
		},
		"batch is closed": {
			setup: func(t *testing.T, b abandonBoard) string {
				run := b.unpublished(t, "late-run")
				err := WithTransaction(b.root, reviewScope(run.TaskIDs, false), func(tx *Transaction) error {
					return tx.PutGroup(reviewControlGroup, closureName(run.BatchID), reviewJSON(ReviewClosure{Schema: 1, BatchID: run.BatchID}))
				})
				if err != nil {
					t.Fatal(err)
				}
				return run.RunID
			},
			want: "is closed",
		},
		"another run names it as predecessor": {
			setup: func(t *testing.T, b abandonBoard) string {
				run := b.unpublished(t, "head-run")
				successor := run
				successor.RunID, successor.PreviousRunID, successor.ReviewedCommit = "tail-run", run.RunID, run.Commit
				err := WithTransaction(b.root, reviewScope(run.TaskIDs, false), func(tx *Transaction) error {
					return tx.PutGroup(reviewControlGroup, reviewRunName(successor.RunID), reviewJSON(successor))
				})
				if err != nil {
					t.Fatal(err)
				}
				return run.RunID
			},
			want: "tail-run",
		},
		"no live member": {
			setup: func(t *testing.T, b abandonBoard) string {
				run := b.unpublished(t, "no-live")
				err := WithTransaction(b.root, LockScope{Tasks: []string{b.id}, ExclusiveBoard: true}, func(tx *Transaction) error {
					return tx.Relocate(b.id, "done")
				})
				if err != nil {
					t.Fatal(err)
				}
				return run.RunID
			},
			want: "no member of this run is in working or review",
		},
		"run does not exist": {
			setup: func(t *testing.T, b abandonBoard) string { return "never-prepared" },
			want:  "missing run",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			b := newAbandonBoard(t)
			runID := "decision-run"
			if test.setup != nil {
				runID = test.setup(t, b)
			} else {
				runID = b.unpublished(t, runID).RunID
			}
			request := abandonRequest(runID)
			if test.request != nil {
				request = test.request(runID)
			}
			var before abandonTouched
			if name != "run does not exist" {
				before = b.touched(t, runID)
			}
			err := AbandonReviewRunWithTransaction(b.root, request)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want a refusal with %q, got %v", test.want, err)
			}
			if name != "run does not exist" {
				b.requireUntouched(t, runID, before)
			}
		})
	}
}

func TestAbandonReplayReturnsTheSameResultAndWritesNothing(t *testing.T) {
	b := newAbandonBoard(t)
	run := b.unpublished(t, "replay-run")
	if err := AbandonReviewRunWithTransaction(b.root, abandonRequest(run.RunID)); err != nil {
		t.Fatal(err)
	}
	before := b.touched(t, run.RunID)
	var first, second any
	capture := func(into *any) TransactionExtension {
		return TransactionExtension{After: func(tx *Transaction) error { *into = tx.OperationResult(); return nil }}
	}
	if err := AbandonReviewRunWithTransaction(b.root, abandonRequest(run.RunID), capture(&first)); err != nil {
		t.Fatal(err)
	}
	if err := AbandonReviewRunWithTransaction(b.root, abandonRequest(run.RunID), capture(&second)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, map[string]string{"run_id": run.RunID}) || !reflect.DeepEqual(first, second) {
		t.Fatalf("replay results differ: %v %v", first, second)
	}
	b.requireUntouched(t, run.RunID, before)

	other := abandonRequest(run.RunID)
	other.Decision = "another decision"
	if err := AbandonReviewRunWithTransaction(b.root, other); err == nil || !strings.Contains(err.Error(), "already abandoned with another decision") {
		t.Fatalf("a different decision did not conflict: %v", err)
	}
	other = abandonRequest(run.RunID)
	other.Reason = "another reason"
	if err := AbandonReviewRunWithTransaction(b.root, other); err == nil || !strings.Contains(err.Error(), "already abandoned with another decision") {
		t.Fatalf("a different reason did not conflict: %v", err)
	}
	b.requireUntouched(t, run.RunID, before)
}

// closeBatchWith closes the batch with run as the PMQA conclusion.
func (b abandonBoard) closeBatchWith(t *testing.T, run ReviewRun) (ReviewClosure, error) {
	t.Helper()
	view, request := supersedeRequest(t, b.root, run, nil)
	closure, _, err := closeWith(t, b.root, view, request)
	return closure, err
}

func TestAbandonedRunNoLongerBlocksTheBatch(t *testing.T) {
	b := newAbandonBoard(t)
	lost := b.unpublished(t, "lost-run")
	good := b.published(t, "good-run")
	if _, err := PublishReviewDisposition(b.root, "batch"); err == nil || !strings.Contains(err.Error(), "lost-run: incomplete publication") {
		t.Fatalf("the unpublished run did not block the aggregation: %v", err)
	}
	if problems, err := CheckReviewEvidence(b.root, []string{b.id}); err != nil || len(problems) == 0 {
		t.Fatalf("check did not report the unpublished run: %v %v", problems, err)
	}
	goodBefore := runRecord(t, b.root, good.RunID)
	if err := AbandonReviewRunWithTransaction(b.root, abandonRequest(lost.RunID)); err != nil {
		t.Fatal(err)
	}
	if runRecord(t, b.root, good.RunID) != goodBefore {
		t.Fatal("the abandon rewrote another run of the batch")
	}
	view, err := PublishReviewDisposition(b.root, "batch")
	if err != nil {
		t.Fatalf("the aggregation still fails after the abandon: %v", err)
	}
	if len(view.Runs) != 1 || view.Runs[0].Run.RunID != good.RunID || view.Runs[0].Findings == nil {
		t.Fatalf("the view lost the findings of the other run or kept the abandoned one: %+v", view.Runs)
	}
	want := []ReviewAbandonedRun{{RunID: lost.RunID, Decision: "user decision 2026-11-19", Reason: abandonRequest("").Reason}}
	if !reflect.DeepEqual(view.AbandonedRuns, want) {
		t.Fatalf("abandoned_runs = %+v", view.AbandonedRuns)
	}
	if problems, err := CheckReviewEvidence(b.root, []string{b.id}); err != nil || len(problems) != 0 {
		t.Fatalf("check reports the abandoned run: %v %v", problems, err)
	}
	closure, err := b.closeBatchWith(t, good)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(closure.View.AbandonedRuns, want) {
		t.Fatalf("the closure lost the audit line: %+v", closure.View.AbandonedRuns)
	}

	// The batch is closed and the card could be gone: the replay still answers.
	before := b.touched(t, lost.RunID)
	if err := AbandonReviewRunWithTransaction(b.root, abandonRequest(lost.RunID)); err != nil {
		t.Fatalf("replay after the close: %v", err)
	}
	b.requireUntouched(t, lost.RunID, before)
}

func TestBatchViewWithoutAnAbandonedRunKeepsItsBytes(t *testing.T) {
	b := newAbandonBoard(t)
	b.published(t, "plain-run")
	view, err := ReadReviewBatchView(b.root, "batch")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(reviewJSON(view), `"abandoned_runs"`) {
		t.Fatalf("a view without an abandoned run carries the field: %s", reviewJSON(view))
	}
	run, err := ReadReviewRun(b.root, "plain-run")
	if err != nil {
		t.Fatal(err)
	}
	encoded := reviewJSON(run)
	if strings.Contains(encoded, `"abandonment"`) {
		t.Fatalf("a run that was never abandoned carries the field: %s", encoded)
	}
	var decoded ReviewRun
	if err := DecodeReviewJSON([]byte(encoded), &decoded); err != nil || reviewJSON(decoded) != encoded {
		t.Fatalf("re-encoding changed the run: %v", err)
	}
}

func TestClosingNeedsANewRunWhenTheOnlyRunWasAbandoned(t *testing.T) {
	b := newAbandonBoard(t)
	run := b.unpublished(t, "only-run")
	if err := AbandonReviewRunWithTransaction(b.root, abandonRequest(run.RunID)); err != nil {
		t.Fatal(err)
	}
	view, err := ReadReviewBatchView(b.root, "batch")
	if err != nil {
		t.Fatal(err)
	}
	request := ReviewCloseRequest{BatchID: "batch", ExpectedRevision: view.Batch.Revision, ViewHash: ReviewViewDigest(view), Author: "coordinator", Roles: map[string]ReviewRoleConclusion{"PMQA": passRole(run)}}
	if _, _, err := ReviewClosureEdges(view, request); err == nil || !strings.Contains(err.Error(), "required successful role conclusion") {
		t.Fatalf("an abandoned run satisfied the role: %v", err)
	}
}

func TestAbandonedRunCannotBePreparedPublishedOrUsedAsPredecessor(t *testing.T) {
	b := newAbandonBoard(t)
	run := b.unpublished(t, "dead-run")
	if err := AbandonReviewRunWithTransaction(b.root, abandonRequest(run.RunID)); err != nil {
		t.Fatal(err)
	}
	const abandoned = "abandoned"

	input := archiveInput([]string{b.id}, "dead-run", "PMQA")
	if _, _, err := PrepareReviewRun(b.root, input, nil, nil, archiveOriginals(), "test"); err == nil || !strings.Contains(err.Error(), abandoned) {
		t.Fatalf("the same run ID was prepared again: %v", err)
	}
	next := archiveInput([]string{b.id}, "next-run", "PMQA")
	next.PreviousRunID, next.ReviewedCommit = run.RunID, run.Commit
	if _, _, err := PrepareReviewRun(b.root, next, nil, nil, archiveOriginals(), "test"); err == nil || !strings.Contains(err.Error(), abandoned) {
		t.Fatalf("an abandoned run became a predecessor: %v", err)
	}
	if _, err := PublishReviewRunAtomic(b.root, run.RunID); err == nil || !strings.Contains(err.Error(), abandoned) {
		t.Fatalf("an abandoned run was published: %v", err)
	}
	if _, failures, err := PublishReviewRun(b.root, run.RunID); err != nil || len(failures) != 1 || !strings.Contains(failures[b.id].Error(), abandoned) {
		t.Fatalf("per-card publication of an abandoned run: %v %v", failures, err)
	}
	stored, err := ReadReviewRun(b.root, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	err = WithTransaction(b.root, reviewScope(stored.TaskIDs, true), func(tx *Transaction) error { return verifyPublishedReview(tx, stored) })
	if err == nil || !strings.Contains(err.Error(), abandoned) {
		t.Fatalf("verifyPublishedReview accepted an abandoned run: %v", err)
	}
	if err := ReviewPublicationComplete(b.root, run.RunID); err == nil || !strings.Contains(err.Error(), abandoned) {
		t.Fatalf("ReviewPublicationComplete accepted an abandoned run: %v", err)
	}
	if err := AssignReviewFindings(b.root, ReviewAssignment{RunID: run.RunID, BatchID: run.BatchID, Author: "coordinator", Basis: "x", Items: map[string][]string{}}); err == nil || !strings.Contains(err.Error(), abandoned) {
		t.Fatalf("an abandoned run was consumed: %v", err)
	}
}

func TestAbandonAfterAConfirmedPublishIsRefusedByTheKernel(t *testing.T) {
	b := newAbandonBoard(t)
	run := b.unpublished(t, "raced-run")
	// A publish that was confirmed before the abandon: it runs on the abandoned run.
	if err := AbandonReviewRunWithTransaction(b.root, abandonRequest(run.RunID)); err != nil {
		t.Fatal(err)
	}
	before := b.touched(t, run.RunID)
	if _, err := PublishReviewRunAtomic(b.root, run.RunID); err == nil || !strings.Contains(err.Error(), "abandoned") {
		t.Fatalf("the confirmed publication ran after the abandon: %v", err)
	}
	b.requireUntouched(t, run.RunID, before)
}

func TestAbandonAfterPublishIsRefused(t *testing.T) {
	b := newAbandonBoard(t)
	run := b.published(t, "published-first")
	before := b.touched(t, run.RunID)
	if err := AbandonReviewRunWithTransaction(b.root, abandonRequest(run.RunID)); err == nil {
		t.Fatal("a published run was abandoned")
	}
	b.requireUntouched(t, run.RunID, before)
}

func TestHintOnlyForFinalizedIncompletePublication(t *testing.T) {
	b := newAbandonBoard(t)
	b.unpublished(t, "hint-final")
	_, _, err := PrepareReviewRun(b.root, archiveInput([]string{b.id}, "hint-open", "PMQA"), nil, nil, archiveOriginals(), "test")
	if err != nil {
		t.Fatal(err)
	}
	problems, err := CheckReviewEvidence(b.root, []string{b.id})
	if err != nil {
		t.Fatal(err)
	}
	var final, unfinished string
	for _, p := range problems {
		switch {
		case strings.Contains(p.Message, "hint-final: incomplete publication"):
			final = p.Message
		case strings.Contains(p.Message, "hint-open: incomplete publication"):
			unfinished = p.Message
		}
	}
	if !strings.Contains(final, "kander review abandon") || !strings.Contains(final, "same run ID") {
		t.Fatalf("check gives no next step for a finalized run: %q", final)
	}
	if unfinished == "" || strings.Contains(unfinished, "kander review abandon") {
		t.Fatalf("check offers an abandon for a run that is not finalized: %q", unfinished)
	}
	if _, err := PublishReviewDisposition(b.root, "batch"); err == nil || !strings.Contains(err.Error(), "incomplete publication") || !strings.Contains(err.Error(), "kander review abandon") {
		t.Fatalf("aggregation gives no next step: %v", err)
	}
}
