package board

import (
	"reflect"
	"strings"
	"testing"
)

func TestIncrementalSupplementCannotReplaceAutomaticSource(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "source")
	gatePlan(t, root, []string{id}, archiveRequirements())
	input := archiveInput([]string{id}, "pm", "PM")
	input.FindingsSchema = 1
	run := gateRun(t, root, input, emptyFindings())
	assignGate(t, root, run, map[string][]string{})
	source, err := ReviewIncrementalContext(root, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	supplement := "Caller context\r\n多字节\n" + contextPrefix + "0\n" + contextSeparator
	merged := MergeReviewContext(source, supplement)
	actual, extra, err := SplitReviewContext(merged)
	if err != nil || string(actual) != string(source) || extra != supplement {
		t.Fatal("source/supplement bytes lost")
	}
	input.RunID, input.PreviousRunID, input.ReviewedCommit, input.Commit = "next", run.RunID, run.Commit, strings.Repeat("c", 40)
	advance := &ReviewAdvance{PreviousTarget: run.Commit, Target: input.Commit, Reason: "member fix", Deliveries: map[string]string{input.Commit: id}}
	originals := archiveOriginals()
	originals["review-context.md"] = MergeReviewContext([]byte("replacement source"), supplement)
	if _, _, err = PrepareReviewRun(root, input, nil, advance, originals, "test"); err == nil || !strings.Contains(err.Error(), "incremental source changed") {
		t.Fatalf("source substitution accepted: %v", err)
	}
	originals["review-context.md"] = merged
	if _, _, err = PrepareReviewRun(root, input, nil, advance, originals, "test"); err != nil {
		t.Fatal(err)
	}
}

func TestRebindPreservesClosedEvidenceAndTerminalMember(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "active"), gateCard(t, root, "complete")}
	for _, id := range ids {
		s := transactionSnapshot(t, root, id)
		text, err := setMetadata(s.Text, FieldStartedAt, "2026-01-01 00:00")
		if err != nil {
			t.Fatal(err)
		}
		text = strings.Replace(text, "## SUMMARY\n\n<FILL_IN>", "## SUMMARY\n\nFixture completion.", 1)
		if err = WithTransaction(root, LockScope{Tasks: []string{id}}, func(tx *Transaction) error { return tx.Put(id, "spec.md", text) }); err != nil {
			t.Fatal(err)
		}
	}
	p := gatePlan(t, root, ids, noReviewRequirements())
	closed, err := gateClose(t, root, map[string]ReviewRoleConclusion{})
	if err != nil {
		t.Fatal(err)
	}
	s := transactionSnapshot(t, root, ids[1])
	if _, err = MoveWithOptions(s.Entry, root, "done", MoveOptions{Result: "completed"}); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, ids[0])
	moved, err := MoveWithOptions(s.Entry, root, "review", MoveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = MoveWithOptions(moved, root, "working", MoveOptions{Owner: "claude"}); err != nil {
		t.Fatal(err)
	}
	progress, err := ReviewTaskProgress(root, ids[0])
	if err != nil || progress.Status != "requirements-needed" {
		t.Fatalf("%+v %v", progress, err)
	}
	x := ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: 1, RebindCycles: progress.RebindCycles, Author: "main", Basis: "reclaim keeps closed evidence"}
	if err = ExtendReviewPlan(root, x); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if progress, err = ReviewTaskProgress(root, id); err != nil || progress.Status != "closed" {
			t.Fatalf("%+v %v", progress, err)
		}
	}
	if transactionSnapshot(t, root, ids[1]).Entry.State != "done" {
		t.Fatal("terminal member moved during rebind")
	}
	if err = WithTransaction(root, reviewScope(ids, true), func(tx *Transaction) error {
		var after ReviewClosure
		ok, err := readReviewJSON(tx, closureName("batch"), &after)
		if err != nil {
			return err
		}
		if !ok || !reflect.DeepEqual(after, closed) {
			t.Fatal("closed evidence rewritten")
		}
		var history struct {
			Previous   ReviewPlan          `json:"previous"`
			Request    ReviewPlanExtension `json:"request"`
			RecordedAt string              `json:"recorded_at"`
		}
		ok, err = readReviewJSON(tx, "plan-history/"+p.PlanID+"/2.json", &history)
		if err != nil {
			return err
		}
		if !ok || history.Previous.Revision != 1 || !reflect.DeepEqual(history.Previous.Batches, p.Batches) {
			t.Fatal("old plan missing from recovery history")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTrackedCycleCorruptionCannotBeRebound(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "tracked")
	p := gatePlan(t, root, []string{id}, noReviewRequirements())
	if err := WithTransaction(root, reviewScope([]string{id}, false), func(tx *Transaction) error {
		return tx.PutGroup(reviewControlGroup, "tracked-cycles/"+id+".json", reviewJSON(map[string]string{"cycle": "wrong"}))
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ReviewTaskProgress(root, id); err == nil || !strings.Contains(err.Error(), "tracked cycle/plan mismatch") {
		t.Fatalf("corruption treated as pending: %v", err)
	}
	if err := ExtendReviewPlan(root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: 1, RebindCycles: map[string]string{id: "wrong"}, Author: "main", Basis: "cannot repair by claiming a cycle"}); err == nil {
		t.Fatal("rebind masked corrupted tracker")
	}
}
