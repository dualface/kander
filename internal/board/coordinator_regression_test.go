package board

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The advisory guard can still race a later move. The controlled write must
// reject the old revision, preserve the new author record and create no copy.
func TestAuditGuardCheckDoesNotCoverLaterMove(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "guard-race")
	v, e := GuardWrite(root, s.Entry.Document)
	if e != nil || !v.Allowed {
		t.Fatal(v, e)
	}
	if _, e = MoveEntry(s.Entry, root, "working"); e != nil {
		t.Fatal(e)
	}
	current := transactionSnapshot(t, root, s.Entry.TaskID)
	text := current.Text + "\n新作者记录保留。\n"
	if e = UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: "spec.md", Text: text, ExpectedRevision: current.Revision}); e != nil {
		t.Fatal(e)
	}
	if e = UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: "spec.md", Text: s.Text, ExpectedRevision: s.Revision}); e == nil {
		t.Fatal("old guarded write accepted")
	}
	b, e := Scan(root)
	if e != nil || len(b.Problems) != 0 || len(b.Entries) != 1 {
		t.Fatal("duplicate entry", e, b.Problems)
	}
	after := transactionSnapshot(t, root, s.Entry.TaskID)
	if after.Text != text {
		t.Fatal("new author record lost")
	}
	if _, e = os.Stat(s.Entry.Document); !os.IsNotExist(e) {
		t.Fatal("old path resurrected", e)
	}
}

func TestCoordinatorAllFailedRolesRemainPending(t *testing.T) {
	root := tempBoard(t)
	s := coordinatorCard(t, root, "failed-roles")
	c := coordinatorClaim(t, root, s.Entry.TaskID)
	gatePlan(t, root, []string{s.Entry.TaskID}, archiveRequirements())
	for _, role := range []string{"PM", "QA"} {
		in := archiveInput([]string{s.Entry.TaskID}, "failed-"+strings.ToLower(role), role)
		run, _, e := PrepareReviewRun(root, in, nil, nil, archiveOriginals(), "test")
		if e != nil {
			t.Fatal(e)
		}
		run.ExecutionStatus = "failed"
		run.LaunchStatus = "failed"
		run.FailureReason = "测试退出"
		run.ExitCode = 1
		run, e = FinalizeReviewRun(root, run, nil)
		if e != nil {
			t.Fatal(e)
		}
		publishRun(t, root, run.RunID)
	}
	c = coordinatorReconcile(t, root, c)
	m := c.Members[s.Entry.TaskID]
	if m.Review.Status != "pending" || len(m.Review.Runs) != 2 {
		t.Fatal(m)
	}
	for _, ref := range m.Review.Runs {
		if !strings.HasSuffix(ref.Path, "output.raw") {
			t.Fatal("failed run invented report", ref)
		}
		if _, e := os.Stat(filepath.Join(s.Entry.Path, filepath.FromSlash(ref.Path))); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := PrepareDispatch(root, coordinatorWrapInput(t, root, s.Entry.TaskID, "false-wrap")); e == nil {
		t.Fatal("failed roles authorized wrap-up")
	}
	if _, e := ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), nil); e != nil {
		t.Fatal("legitimate pending evidence treated as damage", e)
	}
}

func coordinatorAdvancedFix(t *testing.T) (string, CoordinatorCheckpoint, DispatchInput, ReviewRun, ReviewDisposition) {
	t.Helper()
	root := tempBoard(t)
	s := coordinatorCard(t, root, "completed-fix")
	text, _ := setMetadata(s.Text, FieldOwner, "codex")
	if e := WithTransaction(root, LockScope{Tasks: []string{s.Entry.TaskID}}, func(tx *Transaction) error { return tx.Put(s.Entry.TaskID, "spec.md", text) }); e != nil {
		t.Fatal(e)
	}
	s = transactionSnapshot(t, root, s.Entry.TaskID)
	if _, e := MoveEntry(s.Entry, root, "working"); e != nil {
		t.Fatal(e)
	}
	gatePlan(t, root, []string{s.Entry.TaskID}, archiveRequirements())
	finding := ReviewFinding{ID: "PM-01", Tier: "medium", Text: "需要修复", Evidence: "file.go:1"}
	findings := emptyFindings()
	findings.Findings = []ReviewFinding{finding}
	run := gateRun(t, root, archiveInput([]string{s.Entry.TaskID}, "completed-fix-pm", "PM"), findings)
	assignGate(t, root, run, map[string][]string{finding.ID: {s.Entry.TaskID}})
	s = transactionSnapshot(t, root, s.Entry.TaskID)
	if _, e := MoveEntry(s.Entry, root, "review"); e != nil {
		t.Fatal(e)
	}
	in := dispatchInput(s, "completed-fix")
	in.Kind, in.Base = "fix", run.Commit
	in.Evidence.Fix = &DispatchFixBinding{BatchID: run.BatchID, Findings: []DispatchFindingReference{{FindingRef: FindingRef{run.RunID, finding.ID}}}}
	c := coordinatorClaim(t, root, in.TaskID)
	d := prepareTestDispatch(t, root, in)
	if _, e := dispatchMove(t, root, d, "working"); e != nil {
		t.Fatal(e)
	}
	s = transactionSnapshot(t, root, in.TaskID)
	commit := strings.Repeat("c", 40)
	record := ReviewDisposition{RecordID: "coordinator-fixed", RunID: run.RunID, FindingID: finding.ID, BatchID: run.BatchID, TaskID: in.TaskID, Author: "codex", ReportHash: run.Hashes["report.md"], Original: finding.Text, Status: "fixed", Basis: "验证本卡修复", FixCommit: commit, Verification: "测试验证", Authorization: &d.Authorization}
	if e := SubmitReviewDisposition(root, record, s.Revision); e != nil {
		t.Fatal(e)
	}
	s = transactionSnapshot(t, root, in.TaskID)
	if _, e := MoveWithOptions(s.Entry, root, "review", MoveOptions{Authorization: d.Authorization, DeliveryCommit: commit}); e != nil {
		t.Fatal(e)
	}
	c = coordinatorReconcile(t, root, c)
	batch, e := ReadReviewBatch(root, run.BatchID)
	if e != nil {
		t.Fatal(e)
	}
	if e = AdvanceReviewBatch(root, ReviewBatchAdvance{BatchID: run.BatchID, ExpectedRevision: batch.Revision, Advance: ReviewAdvance{PreviousTarget: batch.TargetCommit, Target: commit, Reason: "接收同批修复", Deliveries: map[string]string{commit: in.TaskID}}}); e != nil {
		t.Fatal(e)
	}
	return root, c, in, run, record
}

func TestCoordinatorCompletedFixSurvivesBatchAdvance(t *testing.T) {
	root, c, in, _, record := coordinatorAdvancedFix(t)
	commit := record.FixCommit
	// The immutable completed fix is historical evidence, even though its base
	// can no longer be used to send another fix against the advanced batch.
	if e := ValidateDispatchEvidence(root, in.TaskID, in.ID); e == nil {
		t.Fatal("old fix still sendable after target advance")
	}
	c = coordinatorReconcile(t, root, c)
	if c.Members[in.TaskID].DeliveryCommit != commit || c.Members[in.TaskID].Dispatch.PendingDelivery {
		t.Fatal("completed fix lost at new target")
	}
	s := transactionSnapshot(t, root, in.TaskID)
	if e := os.Remove(filepath.Join(s.Entry.Path, dispositionPath(record))); e != nil {
		t.Fatal(e)
	}
	if _, e := ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), nil); e == nil {
		t.Fatal("lost author original accepted")
	}
}
