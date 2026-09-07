package board

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func wrapUpGrantFixture(t *testing.T, expired bool) (string, Dispatch, WrapUpExitEvidence) {
	t.Helper()
	root := tempBoard(t)
	s := dispatchCard(t, root, "wrap-authority")
	text, _ := setMetadata(s.Text, FieldSession, "original-session")
	text, _ = setMetadata(text, FieldOwner, "codex")
	text, _ = setMetadata(text, FieldWindow, "herdr:t1:p1")
	if err := WriteManagedDocument(root, s.Entry, text); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, s.Entry.TaskID)
	in := dispatchInput(s, "wrap-authority")
	bindWrapUpFixture(t, root, &in)
	if expired {
		in.CreatedAt = time.Now().Add(-time.Hour)
		in.ConfirmBy = time.Now().Add(-time.Minute)
	}
	d := prepareTestDispatch(t, root, in)
	s = transactionSnapshot(t, root, in.TaskID)
	exit := WrapUpExitEvidence{Outcome: "stopped", CardRevision: s.Revision, Session: MetadataFrom(s.Text, FieldSession), Window: MetadataFrom(s.Text, FieldWindow), Owner: MetadataFrom(s.Text, FieldOwner), StartedAt: MetadataFrom(s.Text, FieldStartedAt), ObservedAt: time.Now().UTC()}
	return root, d, exit
}

func TestDispatchWrapUpRejectsInsufficientExitFacts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*WrapUpExitEvidence)
	}{
		{"no session", func(e *WrapUpExitEvidence) { e.Session = "" }},
		{"unknown delivery", func(e *WrapUpExitEvidence) { e.Outcome = "delivery-unknown" }},
		{"still alive", func(e *WrapUpExitEvidence) { e.Outcome = "alive" }},
		{"nonzero", func(e *WrapUpExitEvidence) { e.Outcome = "failed" }},
		{"timeout", func(e *WrapUpExitEvidence) { e.Outcome = "timed-out" }},
		{"lease expiry", func(e *WrapUpExitEvidence) { e.Outcome = "expired" }},
		{"reclaim without decision", func(e *WrapUpExitEvidence) { e.Outcome = "reclaimed" }},
		{"stale observation", func(e *WrapUpExitEvidence) { e.ObservedAt = time.Now().Add(-time.Minute) }},
		{"wrong revision", func(e *WrapUpExitEvidence) { e.CardRevision-- }},
		{"wrong session", func(e *WrapUpExitEvidence) { e.Session = "other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, d, exit := wrapUpGrantFixture(t, true)
			tc.change(&exit)
			if _, err := AuthorizeDispatchWrapUp(root, d.Input.TaskID, d.Input.ID, d.Revision, "coordinator", "原执行者退出", exit); err == nil {
				t.Fatal("insufficient fact authorized wrap-up")
			}
			got, err := ReadDispatch(root, d.Input.TaskID, d.Input.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Authorization != d.Authorization || got.WrapUpAuthority != nil {
				t.Fatal("failed grant changed epoch")
			}
		})
	}
}

func TestDispatchWrapUpGrantFencesAndRestrictsWrites(t *testing.T) {
	root, d, exit := wrapUpGrantFixture(t, true)
	next, err := AuthorizeDispatchWrapUp(root, d.Input.TaskID, d.Input.ID, d.Revision, "coordinator", "原执行者退出，仅代收尾", exit)
	if err != nil {
		t.Fatal(err)
	}
	if next.Authorization.Epoch != d.Authorization.Epoch+1 || next.WrapUpAuthority.Author != "coordinator" || !next.Input.ConfirmBy.Equal(d.Input.ConfirmBy) {
		t.Fatal("grant lost intent or provenance")
	}
	if _, err = dispatchMove(t, root, d, "working"); err == nil {
		t.Fatal("old epoch accepted")
	}
	if _, err = dispatchMove(t, root, next, "working"); err != nil {
		t.Fatal(err)
	}
	s := transactionSnapshot(t, root, d.Input.TaskID)
	if err = UpdateDocument(root, d.Input.TaskID, UpdateOptions{Document: "notes.md", Text: "code edits", ExpectedRevision: s.Revision, Authorization: next.Authorization}); err == nil {
		t.Fatal("generic document write accepted")
	}
	if err = UpdateDocument(root, d.Input.TaskID, UpdateOptions{Document: "spec.md", Text: strings.ReplaceAll(s.Text, "fixture", "rewritten conclusion"), ExpectedRevision: s.Revision, Authorization: next.Authorization}); err == nil {
		t.Fatal("predecessor text overwritten")
	}
	if err = WriteManagedDocument(root, s.Entry, s.Text+"\nnew runtime identity\n"); err == nil {
		t.Fatal("managed runtime write accepted")
	}
	if _, err = BeginDispatchAttempt(root, d.Input.TaskID, d.Input.ID, next.Revision); err == nil {
		t.Fatal("wrap-up-only grant delivered to agent")
	}
	if _, err = ReauthorizeDispatch(root, d.Input.TaskID, d.Input.ID, next.Revision); err == nil {
		t.Fatal("wrap-up-only grant upgraded to full execution")
	}
	if err = WithTransaction(root, LockScope{Tasks: []string{d.Input.TaskID}, ReadOnly: true}, func(tx *Transaction) error { return tx.requireFullExecution(s) }); err == nil {
		t.Fatal("original-author conclusions allowed")
	}
	record := strings.TrimRight(s.Text, "\n") + "\n\n## WRAP_UP_RECORDS\n\n作者：coordinator。已确认集成；只清理任务资源。\n"
	if err = UpdateDocument(root, d.Input.TaskID, UpdateOptions{Document: "spec.md", Text: record, ExpectedRevision: s.Revision, Authorization: next.Authorization}); err != nil {
		t.Fatal(err)
	}
	if _, err = dispatchMove(t, root, next, "done"); err != nil {
		t.Fatal(err)
	}
	got, err := ReadDispatch(root, d.Input.TaskID, d.Input.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != DispatchCompleted || got.Completed.DeliveryCommit != d.Input.Base {
		t.Fatal(got)
	}
	s = transactionSnapshot(t, root, d.Input.TaskID)
	if _, err = os.Stat(filepath.Join(s.Entry.Path, d.Input.Evidence.WrapUp.Artifact.Path)); err != nil {
		t.Fatal("move lost relative integration", err)
	}
	if err = UpdateDocument(root, d.Input.TaskID, UpdateOptions{Document: "spec.md", Text: s.Text + "late", ExpectedRevision: s.Revision, Authorization: d.Authorization}); err == nil {
		t.Fatal("old executor wrote using fresh revision")
	}
}

func TestDispatchWrapUpUnknownReconcilesBeforeFencing(t *testing.T) {
	root, d, exit := wrapUpGrantFixture(t, false)
	unknown, err := BeginDispatchAttempt(root, d.Input.TaskID, d.Input.ID, d.Revision)
	if err != nil {
		t.Fatal(err)
	}
	exit.CardRevision = transactionSnapshot(t, root, d.Input.TaskID).Revision
	if _, err = AuthorizeDispatchWrapUp(root, d.Input.TaskID, d.Input.ID, d.Revision, "coordinator", "旧读取", exit); err == nil {
		t.Fatal("stale dispatch revision granted")
	}
	if _, err = dispatchMove(t, root, unknown, "working"); err != nil {
		t.Fatal(err)
	}
	exit.CardRevision = transactionSnapshot(t, root, d.Input.TaskID).Revision
	if _, err = AuthorizeDispatchWrapUp(root, d.Input.TaskID, d.Input.ID, unknown.Revision, "coordinator", "忽略新接受回执", exit); err == nil {
		t.Fatal("concurrent receipt overwritten")
	}
	current, err := ReadDispatch(root, d.Input.TaskID, d.Input.ID)
	if err != nil {
		t.Fatal(err)
	}
	exit.Outcome = "reclaimed"
	exit.Decision = "此前用户已授权回收，进程已退出"
	exit.ObservedAt = time.Now().UTC()
	grant, err := AuthorizeDispatchWrapUp(root, d.Input.TaskID, d.Input.ID, current.Revision, "coordinator", "合法回收后代收尾", exit)
	if err != nil {
		t.Fatal(err)
	}
	if grant.Authorization.Epoch != current.Authorization.Epoch+1 {
		t.Fatal("did not isolate accepted executor")
	}
	if _, err = dispatchMove(t, root, current, "done"); err == nil {
		t.Fatal("isolated executor completed")
	}
}

func TestDispatchWrapUpIntegrationCannotBeSubstituted(t *testing.T) {
	root, d, _ := wrapUpGrantFixture(t, false)
	s := transactionSnapshot(t, root, d.Input.TaskID)
	if err := os.WriteFile(filepath.Join(s.Entry.Path, d.Input.Evidence.WrapUp.Artifact.Path), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := BeginDispatchAttempt(root, d.Input.TaskID, d.Input.ID, d.Revision); err == nil {
		t.Fatal("forged integration sent")
	}
	if err := ValidateDispatchEvidence(root, d.Input.TaskID, d.Input.ID); err == nil {
		t.Fatal("forged integration read as valid")
	}
}

func TestDispatchWrapUpRecordAndCompletionKeepOriginalEvidence(t *testing.T) {
	root, d, exit := wrapUpGrantFixture(t, false)
	grant, err := AuthorizeDispatchWrapUp(root, d.Input.TaskID, d.Input.ID, d.Revision, "coordinator", "已退出，只收尾", exit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = dispatchMove(t, root, grant, "working"); err != nil {
		t.Fatal(err)
	}
	s := transactionSnapshot(t, root, d.Input.TaskID)
	record := "wrap-up/" + d.Input.ID + "-2.md"
	options := UpdateOptions{Document: record, Text: "作者：coordinator。完成受授权清理。\n", ExpectedRevision: s.Revision, Authorization: grant.Authorization}
	if err = UpdateDocument(root, d.Input.TaskID, options); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, d.Input.TaskID)
	options.ExpectedRevision = s.Revision
	if err = UpdateDocument(root, d.Input.TaskID, options); err != nil {
		t.Fatal("identical record retry", err)
	}
	s = transactionSnapshot(t, root, d.Input.TaskID)
	options.ExpectedRevision = s.Revision
	options.Text = "替换先前结论"
	if err = UpdateDocument(root, d.Input.TaskID, options); err == nil {
		t.Fatal("on-behalf record overwritten")
	}
	if _, err = MoveWithOptions(s.Entry, root, "done", MoveOptions{Result: "completed", Authorization: grant.Authorization, DeliveryCommit: strings.Repeat("c", 40)}); err == nil {
		t.Fatal("unbound completion commit accepted")
	}
	if err = os.Remove(filepath.Join(s.Entry.Path, d.Input.Evidence.WrapUp.Artifact.Path)); err != nil {
		t.Fatal(err)
	}
	if _, err = dispatchMove(t, root, grant, "done"); err == nil {
		t.Fatal("missing integration evidence completed")
	}
	current, err := ReadDispatch(root, d.Input.TaskID, d.Input.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != DispatchAccepted || current.Completed != nil {
		t.Fatal("invalid completion persisted")
	}
}

func TestDispatchWrapUpSnapshotUsesCurrentGrantDeadline(t *testing.T) {
	root, d, exit := wrapUpGrantFixture(t, true)
	grant, err := AuthorizeDispatchWrapUp(root, d.Input.TaskID, d.Input.ID, d.Revision, "coordinator", "确认退出", exit)
	if err != nil {
		t.Fatal(err)
	}
	view, err := ScanDispatchesContext(context.Background(), root, []string{d.Input.TaskID})
	if err != nil {
		t.Fatal(err)
	}
	fact, err := view.CurrentDispatch(d.Input.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if fact.Epoch != grant.Authorization.Epoch || !fact.ConfirmBy.Equal(grant.WrapUpAuthority.ConfirmBy) || !fact.ConfirmBy.After(time.Now()) || !grant.Input.ConfirmBy.Equal(d.Input.ConfirmBy) {
		t.Fatal("snapshot used expired original deadline", fact)
	}
}
