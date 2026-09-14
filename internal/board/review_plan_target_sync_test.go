package board

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func readPlanCopy(t *testing.T, root, id string) ReviewPlan {
	t.Helper()
	var p ReviewPlan
	err := WithTransaction(root, reviewScope([]string{id}, true), func(tx *Transaction) error {
		text, e := tx.Read(id, "reviews/plan.json")
		if e != nil {
			return e
		}
		return json.Unmarshal([]byte(text), &p)
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func driftBatchTarget(t *testing.T, root, batchID, target string) {
	t.Helper()
	err := WithTransaction(root, reviewScope(nil, false), func(tx *Transaction) error {
		var b ReviewBatch
		ok, e := readReviewJSON(tx, reviewBatchName(batchID), &b)
		if e != nil {
			return e
		}
		if !ok {
			return reviewError("missing batch")
		}
		b.TargetCommit = target
		b.Revision++
		return tx.PutGroup(reviewControlGroup, reviewBatchName(batchID), reviewJSON(b))
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAdvanceReviewBatchSyncsPlanTarget(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "advance-sync")
	gatePlan(t, root, []string{id}, noReviewRequirements())
	registered := strings.Repeat("b", 40)
	final := strings.Repeat("c", 40)
	if err := AdvanceReviewBatch(root, ReviewBatchAdvance{BatchID: "batch", ExpectedRevision: 1, Advance: ReviewAdvance{PreviousTarget: registered, Target: final, Reason: "member fix", Deliveries: map[string]string{final: id}}}); err != nil {
		t.Fatal(err)
	}
	batch, err := ReadReviewBatch(root, "batch")
	if err != nil || batch.TargetCommit != final {
		t.Fatalf("%+v %v", batch, err)
	}
	plan := readPlanCopy(t, root, id)
	if plan.Batches[0].TargetCommit != final || plan.Revision != 2 {
		t.Fatalf("plan not synced: %+v", plan)
	}
	var history struct {
		Previous ReviewPlan `json:"previous"`
	}
	err = WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
		raw, ok, e := tx.ReadGroup(reviewControlGroup, fmt.Sprintf("plan-history/%s/%d.json", plan.PlanID, plan.Revision))
		if e != nil {
			return e
		}
		if !ok {
			return reviewError("missing plan history")
		}
		return json.Unmarshal(raw, &history)
	})
	if err != nil {
		t.Fatal(err)
	}
	if history.Previous.Batches[0].TargetCommit != registered {
		t.Fatalf("history lost old target: %+v", history.Previous.Batches[0])
	}
	progress, err := ReviewTaskProgress(root, id)
	if err != nil || progress.Status != "pending" {
		t.Fatalf("%+v %v", progress, err)
	}
	if problems := CheckReviewGate(root, []string{id}); len(problems) > 0 {
		t.Fatalf("%v", problems)
	}
}

func TestAdvanceFileSyncsPlannedBatchAndLeavesUnplannedAlone(t *testing.T) {
	t.Run("planned", func(t *testing.T) {
		root := tempBoard(t)
		id := gateCard(t, root, "advance-file-planned")
		gatePlan(t, root, []string{id}, archiveRequirements())
		first := gateRun(t, root, archiveInput([]string{id}, "first", "PMQA"), emptyFindings())
		next := archiveInput([]string{id}, "second", "PMQA")
		next.Commit = strings.Repeat("c", 40)
		next.ReviewedCommit = first.Commit
		next.PreviousRunID = first.RunID
		advance := &ReviewAdvance{PreviousTarget: first.Commit, Target: next.Commit, Reason: "member fix", Deliveries: map[string]string{next.Commit: id}}
		if _, _, err := PrepareReviewRun(root, next, nil, advance, archiveOriginals(), "test"); err != nil {
			t.Fatal(err)
		}
		plan := readPlanCopy(t, root, id)
		if plan.Batches[0].TargetCommit != next.Commit || plan.Revision < 2 {
			t.Fatalf("planned advance-file did not sync: %+v", plan)
		}
		if _, err := ReviewTaskProgress(root, id); err != nil {
			t.Fatal(err)
		}
		if problems := CheckReviewGate(root, []string{id}); len(problems) > 0 {
			t.Fatalf("%v", problems)
		}
	})
	t.Run("unplanned", func(t *testing.T) {
		root := tempBoard(t)
		id := archiveCard(t, root, "advance-file-unplanned")
		first := finalizedRun(t, root, archiveInput([]string{id}, "u1", "PMQA"))
		publishRun(t, root, first.RunID)
		next := archiveInput([]string{id}, "u2", "PMQA")
		next.Commit = strings.Repeat("c", 40)
		next.ReviewedCommit = first.Commit
		next.PreviousRunID = first.RunID
		advance := &ReviewAdvance{PreviousTarget: first.Commit, Target: next.Commit, Reason: "member fix", Deliveries: map[string]string{next.Commit: id}}
		if _, _, err := PrepareReviewRun(root, next, nil, advance, archiveOriginals(), "test"); err != nil {
			t.Fatal(err)
		}
		batch, err := ReadReviewBatch(root, "batch")
		if err != nil || batch.PlanID != "" || batch.TargetCommit != next.Commit {
			t.Fatalf("%+v %v", batch, err)
		}
	})
}

func TestPlanTargetMismatchProgressCheckAndExtendSync(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "target-drift")
	gatePlan(t, root, []string{id}, noReviewRequirements())
	registered := strings.Repeat("b", 40)
	final := strings.Repeat("c", 40)
	driftBatchTarget(t, root, "batch", final)
	progress, err := ReviewTaskProgress(root, id)
	if err == nil || progress.PlanTarget != registered || progress.BatchTarget != final {
		t.Fatalf("%+v %v", progress, err)
	}
	if !strings.Contains(err.Error(), registered) || !strings.Contains(err.Error(), final) {
		t.Fatal(err)
	}
	problems := CheckReviewGate(root, []string{id})
	if len(problems) != 1 || !strings.Contains(problems[0].Message, registered) || !strings.Contains(problems[0].Message, final) {
		t.Fatalf("%v", problems)
	}
	s := transactionSnapshot(t, root, id)
	summary := strings.Replace(s.Text, "## SUMMARY\n\n<FILL_IN>", "## SUMMARY\n\n完成", 1)
	updateSnapshot(t, root, s, summary)
	s = transactionSnapshot(t, root, id)
	if _, err = MoveWithOptions(s.Entry, root, "done", MoveOptions{Result: "completed"}); err == nil || !strings.Contains(err.Error(), registered) {
		t.Fatalf("move done ignored drift: %v", err)
	}
	next := strings.Repeat("d", 40)
	if err = AdvanceReviewBatch(root, ReviewBatchAdvance{BatchID: "batch", ExpectedRevision: 2, Advance: ReviewAdvance{PreviousTarget: final, Target: next, Reason: "silent repair attempt", Deliveries: map[string]string{next: id}}}); err == nil || !strings.Contains(err.Error(), registered) {
		t.Fatalf("drifted advance silently repaired plan: %v", err)
	}
	plan := readPlanCopy(t, root, id)
	if err = ExtendReviewPlan(root, ReviewPlanExtension{PlanID: plan.PlanID, ExpectedRevision: plan.Revision, SyncTargets: map[string]string{"batch": final}, Author: "coordinator", Basis: "align legacy plan target to runtime batch"}); err != nil {
		t.Fatal(err)
	}
	plan = readPlanCopy(t, root, id)
	if plan.Batches[0].TargetCommit != final {
		t.Fatalf("%+v", plan)
	}
	var history struct {
		Previous ReviewPlan `json:"previous"`
	}
	err = WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
		raw, ok, e := tx.ReadGroup(reviewControlGroup, fmt.Sprintf("plan-history/%s/%d.json", plan.PlanID, plan.Revision))
		if e != nil {
			return e
		}
		if !ok {
			return reviewError("missing plan history")
		}
		return json.Unmarshal(raw, &history)
	})
	if err != nil {
		t.Fatal(err)
	}
	if history.Previous.Batches[0].TargetCommit != registered {
		t.Fatalf("history lost old target: %+v", history.Previous.Batches[0])
	}
	progress, err = ReviewTaskProgress(root, id)
	if err != nil || progress.Status != "pending" {
		t.Fatalf("%+v %v", progress, err)
	}
	if problems = CheckReviewGate(root, []string{id}); len(problems) > 0 {
		t.Fatalf("%v", problems)
	}
}
