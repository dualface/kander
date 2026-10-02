package board

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// archiveSnapshot terminates a card through the ordinary controlled move.
func archiveSnapshot(t *testing.T, root string, s Snapshot) Snapshot {
	t.Helper()
	if _, err := MoveWithOptions(s.Entry, root, "archived", MoveOptions{Result: "cancelled", Reason: "用户取消", Decision: "取消决定"}); err != nil {
		t.Fatal(err)
	}
	return transactionSnapshot(t, root, s.Entry.TaskID)
}

func requireArchivedFinal(t *testing.T, err error) {
	t.Helper()
	var ke *Error
	if !errors.As(err, &ke) || ke.Code != "board.archived_final" {
		t.Fatalf("want board.archived_final, got %v", err)
	}
}

// requireUnchanged proves a rejected write left no side effect on the card.
func requireUnchanged(t *testing.T, root string, before Snapshot) {
	t.Helper()
	after := transactionSnapshot(t, root, before.Entry.TaskID)
	if after.Revision != before.Revision || after.Text != before.Text || after.Entry.Path != before.Entry.Path || after.Entry.State != "archived" {
		t.Fatalf("archived card changed: %+v -> %+v", before.Entry, after.Entry)
	}
	pending, err := os.ReadDir(control(root, "operations", "pending"))
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending operations left: %v %v", pending, err)
	}
}

func TestArchivedCardRejectsEveryMove(t *testing.T) {
	root := tempBoard(t)
	s := archiveSnapshot(t, root, transactionCard(t, root, "archived-moves", false))
	options := map[string]MoveOptions{
		"done":     {Result: "completed"},
		"archived": {Result: "duplicate", Reason: "重复", Decision: "重复决定", DuplicateOf: "20260101-other-task"},
		"trash":    {Result: "trashed", Reason: "删除", Decision: "删除决定"},
	}
	for _, target := range States {
		_, err := MoveWithOptions(s.Entry, root, target, options[target])
		requireArchivedFinal(t, err)
		requireUnchanged(t, root, s)
	}
	if allowedMove("archived", "trash") {
		t.Fatal("archived still has an outgoing transition")
	}
}

func TestArchivedCardRejectsDocumentUpdates(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "archived-documents", true)
	if err := UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: "plan.md", Text: "计划\n", ExpectedRevision: s.Revision}); err != nil {
		t.Fatal(err)
	}
	s = archiveSnapshot(t, root, transactionSnapshot(t, root, s.Entry.TaskID))
	for _, document := range []string{"spec.md", "plan.md", "report.md", "notes/extra.md"} {
		text := "改写\n"
		if document == "spec.md" {
			text = s.Text + "\n## NOTES\n\n后续说明\n"
		}
		err := UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: document, Text: text, ExpectedRevision: s.Revision})
		requireArchivedFinal(t, err)
		requireUnchanged(t, root, s)
	}
	if plan, err := os.ReadFile(filepath.Join(s.Entry.Path, "plan.md")); err != nil || string(plan) != "计划\n" {
		t.Fatalf("plan changed: %q %v", plan, err)
	}
	for _, name := range []string{"report.md", "notes"} {
		if _, err := os.Stat(filepath.Join(s.Entry.Path, name)); !os.IsNotExist(err) {
			t.Fatalf("%s created: %v", name, err)
		}
	}
}

// Producers (review, disposition, dispatch, window and session write-back)
// all stage card files through these primitives.
func TestArchivedCardRejectsProducerStaging(t *testing.T) {
	root := tempBoard(t)
	s := archiveSnapshot(t, root, transactionCard(t, root, "archived-producers", true))
	id := s.Entry.TaskID
	scope := LockScope{Tasks: []string{id}, ExclusiveBoard: true}
	for name, stage := range map[string]func(*Transaction) error{
		"put":      func(tx *Transaction) error { return tx.Put(id, "reviews/run/report.md", "报告\n") },
		"bytes":    func(tx *Transaction) error { return tx.PutBytes(id, "dispatches/d/state.json", []byte("{}\n")) },
		"relocate": func(tx *Transaction) error { return tx.Relocate(id, "trash") },
	} {
		requireArchivedFinal(t, WithTransaction(root, scope, stage))
		requireUnchanged(t, root, s)
		if name == "put" {
			if _, err := os.Stat(filepath.Join(s.Entry.Path, "reviews")); !os.IsNotExist(err) {
				t.Fatalf("reviews created: %v", err)
			}
		}
	}
	text := strings.Replace(s.Text, "- WINDOW:", "- WINDOW: herdr:pane", 1)
	requireArchivedFinal(t, WriteManagedDocument(root, s.Entry, text))
	requireUnchanged(t, root, s)
}

func TestArchivedCardRejectsReviewWrites(t *testing.T) {
	root := tempBoard(t)
	unpublished := archiveCard(t, root, "archived-publish")
	run := finalizedRun(t, root, archiveInput([]string{unpublished}, "archived-publish-run", "PMQA"))
	s := archiveSnapshot(t, root, transactionSnapshot(t, root, unpublished))
	if _, failures, err := PublishReviewRun(root, run.RunID); err == nil && len(failures) == 0 {
		t.Fatal("published to an archived card")
	}
	requireUnchanged(t, root, s)

	root = tempBoard(t)
	ids := []string{gateCard(t, root, "archived-receiver"), gateCard(t, root, "archived-peer")}
	gatePlan(t, root, ids, archiveRequirements())
	report := "Review complete.\n```kander-findings\n{\"FINDINGS\":[],\"NON_BLOCKING\":[]}\n```No findings in either section.\n"
	interpreted := receiverRun(t, root, archiveInput(ids, "archived-receiver-run", "PMQA"), report)
	archived := archiveSnapshot(t, root, transactionSnapshot(t, root, ids[0]))
	peer := transactionSnapshot(t, root, ids[1])
	quote := ReviewReportQuote{StartLine: 3, EndLine: 4, Quote: "{\"FINDINGS\":[],\"NON_BLOCKING\":[]}\n```No findings in either section."}
	requireArchivedFinal(t, InterpretReview(root, zeroInterpretation(interpreted, quote)))
	requireUnchanged(t, root, archived)
	if after := transactionSnapshot(t, root, ids[1]); after.Revision != peer.Revision {
		t.Fatal("interpretation reached the live peer")
	}
}

func TestArchivedCardRejectsDispositionsAndDispatch(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "archived-disposition")
	gatePlan(t, root, []string{id}, archiveRequirements())
	finding := ReviewFinding{ID: "QA-1", Tier: "suggest", Text: "建议补充说明", Evidence: "spec.md:3"}
	run := gateRun(t, root, archiveInput([]string{id}, "archived-disposition-run", "PMQA"), ReviewFindings{Findings: []ReviewFinding{}, NonBlocking: []ReviewFinding{finding}})
	s := archiveSnapshot(t, root, transactionSnapshot(t, root, id))
	d := ReviewDisposition{RecordID: "record-" + id, RunID: run.RunID, FindingID: finding.ID, BatchID: run.BatchID, TaskID: id, Author: "codex", ReportHash: run.Hashes["report.md"], Original: finding.Text, Status: "rejected", Basis: "原目标提交源代码与测试记录已逐项验证"}
	if err := SubmitReviewDisposition(root, d, s.Revision); err == nil {
		t.Fatal("disposition written to an archived card")
	}
	requireUnchanged(t, root, s)

	dispatched := archiveSnapshot(t, root, dispatchCard(t, root, "archived-dispatch"))
	if _, err := PrepareDispatch(root, dispatchInput(dispatched, "archived-dispatch-one")); err == nil {
		t.Fatal("dispatch prepared for an archived card")
	}
	requireUnchanged(t, root, dispatched)
}

// The coordinator writes only its group checkpoint; observing an archived
// member must never stage a card write.
func TestCoordinatorNeverWritesArchivedMember(t *testing.T) {
	root := tempBoard(t)
	s := coordinatorCard(t, root, "archived-member")
	c := coordinatorClaim(t, root, s.Entry.TaskID)
	s = archiveSnapshot(t, root, transactionSnapshot(t, root, s.Entry.TaskID))
	_, _ = ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), nil)
	requireUnchanged(t, root, s)
}

func TestInitRecoversInterruptedArchiveMove(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "archived-recovery", false)
	id := s.Entry.TaskID
	text, err := moveMetadata(s.Text, "backlog", "archived", MoveOptions{Result: "cancelled", Reason: "取消", Decision: "取消决定"})
	if err != nil {
		t.Fatal(err)
	}
	stop := errors.New("interrupted")
	err = withTransactionCheckpoint(nil, root, LockScope{Tasks: []string{id}, ExclusiveBoard: true}, func(at string) error {
		if at == "prepared" {
			return stop
		}
		return nil
	}, func(tx *Transaction) error {
		if e := tx.Put(id, "spec.md", text); e != nil {
			return e
		}
		return tx.Relocate(id, "archived")
	})
	if !errors.Is(err, stop) {
		t.Fatalf("interruption: %v", err)
	}
	if _, err = ReadSnapshot(root, id); err == nil {
		t.Fatal("reader ignored the pending archive move")
	}
	if err = RecoverTransactions(root); err != nil {
		t.Fatal(err)
	}
	recovered := transactionSnapshot(t, root, id)
	if recovered.Entry.State != "archived" || MetadataFrom(recovered.Text, FieldResult) != "cancelled" {
		t.Fatalf("recovery result: %s %q", recovered.Entry.State, MetadataFrom(recovered.Text, FieldResult))
	}
	_, err = MoveWithOptions(recovered.Entry, root, "trash", MoveOptions{Result: "trashed", Reason: "删除", Decision: "删除决定"})
	requireArchivedFinal(t, err)
}
