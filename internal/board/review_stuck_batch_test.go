package board

import (
	"os"
	"strings"
	"testing"
)

// recoveryMember leaves a group member owned and in working, the state in which
// the plan, the review run, and the assignment are created.
func recoveryMember(t *testing.T, root, slug string) string {
	t.Helper()
	s := coordinatorCard(t, root, slug)
	if _, err := MoveWithOptions(s.Entry, root, "working", MoveOptions{Owner: "codex", Decision: "测试决定"}); err != nil {
		t.Fatal(err)
	}
	return s.Entry.TaskID
}

func moveMember(t *testing.T, root, id, state string) {
	t.Helper()
	s := transactionSnapshot(t, root, id)
	if _, err := MoveEntry(s.Entry, root, state); err != nil {
		t.Fatal(err)
	}
}

func fixInput(t *testing.T, root, id, dispatch, base string, run ReviewRun, f ReviewFinding) DispatchInput {
	t.Helper()
	in := dispatchInput(transactionSnapshot(t, root, id), dispatch)
	in.Kind, in.Base = "fix", base
	in.Evidence.Fix = &DispatchFixBinding{BatchID: run.BatchID, Findings: []DispatchFindingReference{{FindingRef: FindingRef{run.RunID, f.ID}}}}
	return in
}

// fixRound completes one member's fix dispatch with an author record.
func fixRound(t *testing.T, root string, in DispatchInput, run ReviewRun, f ReviewFinding, delivery string) Dispatch {
	t.Helper()
	d := prepareTestDispatch(t, root, in)
	if _, err := dispatchMove(t, root, d, "working"); err != nil {
		t.Fatal(err)
	}
	s := transactionSnapshot(t, root, in.TaskID)
	record := ReviewDisposition{RecordID: "fixed-" + strings.ToLower(f.ID), RunID: run.RunID, FindingID: f.ID, BatchID: run.BatchID, TaskID: in.TaskID, Author: "codex", ReportHash: run.Hashes["report.md"], Original: f.Text, Status: "fixed", Basis: "验证本卡修复", FixCommit: delivery, Verification: "测试验证", Authorization: &d.Authorization}
	if err := SubmitReviewDisposition(root, record, s.Revision); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, in.TaskID)
	if _, err := MoveWithOptions(s.Entry, root, "review", MoveOptions{Authorization: d.Authorization, DeliveryCommit: delivery}); err != nil {
		t.Fatal(err)
	}
	return d
}

func batchAdvance(t *testing.T, root, batchID, target, task string) ReviewBatchAdvance {
	t.Helper()
	b, err := ReadReviewBatch(root, batchID)
	if err != nil {
		t.Fatal(err)
	}
	return ReviewBatchAdvance{BatchID: batchID, ExpectedRevision: b.Revision, Advance: ReviewAdvance{PreviousTarget: b.TargetCommit, Target: target, Reason: "接收同批修复", Deliveries: map[string]string{target: task}}}
}

// forceAdvance records an advance the way older binaries did, without the
// disposition gate, to build a batch that was stuck before the gate existed.
func forceAdvance(t *testing.T, root string, x ReviewBatchAdvance, tasks []string) {
	t.Helper()
	if err := WithTransaction(root, reviewScope(tasks, false), func(tx *Transaction) error {
		var b ReviewBatch
		if _, err := readReviewJSON(tx, reviewBatchName(x.BatchID), &b); err != nil {
			return err
		}
		b.Advances = append(b.Advances, x.Advance)
		b.TargetCommit = x.Advance.Target
		b.Revision++
		if err := tx.PutGroup(reviewControlGroup, reviewBatchName(b.BatchID), reviewJSON(b)); err != nil {
			return err
		}
		return syncPlannedBatchTarget(tx, b, reviewPlanTargetSyncRequest{Kind: "advance", PreviousTarget: x.Advance.PreviousTarget})
	}); err != nil {
		t.Fatal(err)
	}
}

type stuckBatch struct {
	root     string
	ids      []string
	run      ReviewRun
	findings []ReviewFinding
	target   string
	c        CoordinatorCheckpoint
}

// stuckRecoveryBatch has three members with findings at one run. Two members
// finish their fix rounds, and the batch advances past the third member's run.
func stuckRecoveryBatch(t *testing.T) stuckBatch {
	t.Helper()
	root := tempBoard(t)
	ids := []string{recoveryMember(t, root, "first"), recoveryMember(t, root, "second"), recoveryMember(t, root, "third")}
	gatePlan(t, root, ids, archiveRequirements())
	findings := emptyFindings()
	items := map[string][]string{}
	for i, id := range ids {
		f := ReviewFinding{ID: []string{"PM-01", "PM-02", "PM-03"}[i], Tier: "medium", Text: "需要修复", Evidence: "file.go:1"}
		findings.Findings = append(findings.Findings, f)
		items[f.ID] = []string{id}
	}
	run := gateRun(t, root, archiveInput(ids, "recovery-pmqa", "PMQA"), findings)
	assignGate(t, root, run, items)
	for _, id := range ids {
		moveMember(t, root, id, "review")
	}
	c := coordinatorClaim(t, root, ids...)
	targets := []string{strings.Repeat("c", 40), strings.Repeat("d", 40)}
	for i, target := range targets {
		fixRound(t, root, fixInput(t, root, ids[i], "fix-"+ids[i], run.Commit, run, findings.Findings[i]), run, findings.Findings[i], target)
		c = coordinatorReconcile(t, root, c)
	}
	for i, target := range targets {
		forceAdvance(t, root, batchAdvance(t, root, run.BatchID, target, ids[i]), ids)
	}
	return stuckBatch{root, ids, run, findings.Findings, targets[1], c}
}

func TestReviewAdvanceRefusesMissingAuthorDisposition(t *testing.T) {
	root, in, run, f := fixBindingFixture(t)
	beforeBatch, err := ReadReviewBatch(root, run.BatchID)
	if err != nil {
		t.Fatal(err)
	}
	beforePlan, err := ReadReviewPlan(root, beforeBatch.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	err = AdvanceReviewBatch(root, batchAdvance(t, root, run.BatchID, strings.Repeat("c", 40), in.TaskID))
	if err == nil || !strings.Contains(err.Error(), in.TaskID) || !strings.Contains(err.Error(), f.ID) {
		t.Fatalf("advance past an undispositioned finding: %v", err)
	}
	batch, err := ReadReviewBatch(root, run.BatchID)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ReadReviewPlan(root, beforeBatch.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Revision != beforeBatch.Revision || batch.TargetCommit != beforeBatch.TargetCommit || plan.Revision != beforePlan.Revision {
		t.Fatal("refused advance changed the batch or the plan")
	}
}

func TestReviewAdvanceFileRefusesMissingAuthorDisposition(t *testing.T) {
	root, in, run, f := fixBindingFixture(t)
	input := archiveInput([]string{in.TaskID}, "incremental", "PMQA")
	input.Commit = strings.Repeat("c", 40)
	input.PreviousRunID, input.ReviewedCommit = run.RunID, run.Commit
	x := batchAdvance(t, root, run.BatchID, input.Commit, in.TaskID)
	_, _, err := PrepareReviewRun(root, input, nil, &x.Advance, archiveOriginals(), "test")
	if err == nil || !strings.Contains(err.Error(), in.TaskID) || !strings.Contains(err.Error(), f.ID) {
		t.Fatalf("advance file past an undispositioned finding: %v", err)
	}
	batch, err := ReadReviewBatch(root, run.BatchID)
	if err != nil {
		t.Fatal(err)
	}
	if batch.TargetCommit != run.Commit {
		t.Fatal("refused advance file moved the target")
	}
}

func TestReviewAdvanceAcceptsDispositionedMechanicalFinding(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "mechanical-advance")
	gatePlan(t, root, []string{id}, archiveRequirements())
	f := ReviewFinding{ID: "PM-01", Tier: "medium", Text: "修正文案", Evidence: "file.go:1"}
	findings := emptyFindings()
	findings.Findings = []ReviewFinding{f}
	run := gateRun(t, root, archiveInput([]string{id}, "mechanical-pmqa", "PMQA"), findings)
	assignGate(t, root, run, map[string][]string{f.ID: {id}})
	next := strings.Repeat("c", 40)
	record := ReviewDisposition{RecordID: "mechanical-fix", RunID: run.RunID, FindingID: f.ID, BatchID: run.BatchID, TaskID: id, Author: "codex", ReportHash: run.Hashes["report.md"], Original: f.Text, Status: "fixed", Basis: "实际修复", FixCommit: next, Verification: "测试验证", Mechanical: "documentation"}
	if err := SubmitReviewDisposition(root, record, transactionSnapshot(t, root, id).Revision); err != nil {
		t.Fatal(err)
	}
	if err := AdvanceReviewBatch(root, batchAdvance(t, root, run.BatchID, next, id)); err != nil {
		t.Fatal(err)
	}
}

func TestFixDispatchBindsRunOnAdvanceChain(t *testing.T) {
	s := stuckRecoveryBatch(t)
	in := fixInput(t, s.root, s.ids[2], "fix-third", s.target, s.run, s.findings[2])
	if _, err := PrepareDispatch(s.root, in); err != nil {
		t.Fatalf("third member's fix dispatch: %v", err)
	}
}

func TestFixDispatchStillRejectsForeignRuns(t *testing.T) {
	advanced := func(t *testing.T) (string, DispatchInput, ReviewRun, ReviewFinding) {
		t.Helper()
		root, in, run, f := fixBindingFixture(t)
		next := strings.Repeat("c", 40)
		forceAdvance(t, root, batchAdvance(t, root, run.BatchID, next, in.TaskID), []string{in.TaskID})
		in.Base = next
		return root, in, run, f
	}
	t.Run("off the chain", func(t *testing.T) {
		root, in, run, _ := advanced(t)
		if err := WithTransaction(root, reviewScope([]string{in.TaskID}, false), func(tx *Transaction) error {
			var b ReviewBatch
			if _, err := readReviewJSON(tx, reviewBatchName(run.BatchID), &b); err != nil {
				return err
			}
			b.Advances[0].PreviousTarget = strings.Repeat("e", 40)
			return tx.PutGroup(reviewControlGroup, reviewBatchName(run.BatchID), reviewJSON(b))
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := PrepareDispatch(root, in); err == nil {
			t.Fatal("run outside the advance chain accepted")
		}
	})
	t.Run("other batch", func(t *testing.T) {
		root, in, _, f := advanced(t)
		findings := emptyFindings()
		findings.Findings = []ReviewFinding{f}
		input := archiveInput([]string{in.TaskID}, "stray-pmqa", "PMQA")
		input.BatchID = "stray"
		// Only a binary from before the stray-batch refusal could add this run.
		pointer := control(root, "groups", reviewControlGroup, "task-plans", in.TaskID+".json")
		saved, err := os.ReadFile(pointer)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.Remove(pointer); err != nil {
			t.Fatal(err)
		}
		prepared, _, err := PrepareReviewRun(root, input, archiveRequirements(), nil, archiveOriginals(), "test")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(pointer, saved, 0o600); err != nil {
			t.Fatal(err)
		}
		prepared.LaunchStatus, prepared.ExecutionStatus = "started", "ok"
		if prepared, err = FinalizeReviewRun(root, prepared, structuredReport(findings)); err != nil {
			t.Fatal(err)
		}
		stray := publishRun(t, root, prepared.RunID)
		assignGate(t, root, stray, map[string][]string{f.ID: {in.TaskID}})
		in.Evidence.Fix.Findings[0].RunID = stray.RunID
		if _, err := PrepareDispatch(root, in); err == nil || !strings.Contains(err.Error(), "run, predecessor or task binding") {
			t.Fatalf("run of another batch: %v", err)
		}
	})
	t.Run("superseded", func(t *testing.T) {
		root, in, run, _ := advanced(t)
		input := archiveInput([]string{in.TaskID}, "successor", "PMQA")
		input.Commit = in.Base
		input.PreviousRunID, input.ReviewedCommit = run.RunID, run.Commit
		gateRun(t, root, input, emptyFindings())
		if _, err := PrepareDispatch(root, in); err == nil || !strings.Contains(err.Error(), "superseded review round") {
			t.Fatalf("superseded run: %v", err)
		}
	})
	t.Run("task outside the run", func(t *testing.T) {
		root := tempBoard(t)
		ids := []string{recoveryMember(t, root, "member"), recoveryMember(t, root, "other")}
		gatePlan(t, root, ids, archiveRequirements())
		f := ReviewFinding{ID: "PM-01", Tier: "medium", Text: "需要修复", Evidence: "file.go:1"}
		findings := emptyFindings()
		findings.Findings = []ReviewFinding{f}
		run := gateRun(t, root, archiveInput(ids, "member-pmqa", "PMQA"), findings)
		assignGate(t, root, run, map[string][]string{f.ID: {ids[0]}})
		for _, id := range ids {
			moveMember(t, root, id, "review")
		}
		next := strings.Repeat("c", 40)
		forceAdvance(t, root, batchAdvance(t, root, run.BatchID, next, ids[0]), ids)
		if _, err := PrepareDispatch(root, fixInput(t, root, ids[1], "foreign-task", next, run, f)); err == nil {
			t.Fatal("finding bound to a member it is not assigned to")
		}
	})
}

// The recovery path uses only supported commands after the stuck state: the
// third member's fix round, a real advance, an incremental run, and closure.
func TestStuckBatchRecoversThroughAdvanceChain(t *testing.T) {
	s := stuckRecoveryBatch(t)
	delivery := strings.Repeat("e", 40)
	in := fixInput(t, s.root, s.ids[2], "fix-third", s.target, s.run, s.findings[2])
	fixRound(t, s.root, in, s.run, s.findings[2], delivery)
	c := coordinatorReconcile(t, s.root, s.c)
	if err := AdvanceReviewBatch(s.root, batchAdvance(t, s.root, s.run.BatchID, delivery, s.ids[2])); err != nil {
		t.Fatal(err)
	}
	// The completed dispatch stays valid history although its base is no longer
	// the current target, and it is no longer sendable.
	if err := ValidateDispatchEvidence(s.root, in.TaskID, in.ID); err == nil {
		t.Fatal("old fix still sendable after the target moved")
	}
	c = coordinatorReconcile(t, s.root, c)
	input := archiveInput(s.ids, "recovery-incremental", "PMQA")
	input.Commit = delivery
	input.PreviousRunID, input.ReviewedCommit = s.run.RunID, s.run.Commit
	passing := gateRun(t, s.root, input, emptyFindings())
	assignGate(t, s.root, passing, map[string][]string{})
	if _, err := gateClose(t, s.root, map[string]ReviewRoleConclusion{"PMQA": passRole(passing)}); err != nil {
		t.Fatal(err)
	}
	c = coordinatorReconcile(t, s.root, c)
	for _, id := range s.ids {
		if c.Members[id].Review == nil || c.Members[id].Review.Status != "closed" {
			t.Fatalf("member %s not closed: %+v", id, c.Members[id].Review)
		}
	}
}
