package board

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func stoppedDispatchFixture(s Snapshot) WrapUpExitEvidence {
	return WrapUpExitEvidence{Outcome: "stopped", CardRevision: s.Revision,
		Session: MetadataFrom(s.Text, FieldSession), Window: MetadataFrom(s.Text, FieldWindow),
		Owner: MetadataFrom(s.Text, FieldOwner), StartedAt: MetadataFrom(s.Text, FieldStartedAt), ObservedAt: time.Now().UTC()}
}

// Backdate a fully accepted fixture to exercise recovery ten minutes later
// without a ten-minute wall-clock delay. Production never rewrites intent.
func expiredAcceptedFixture(t *testing.T) (string, Dispatch, Snapshot) {
	t.Helper()
	root := tempBoard(t)
	s := dispatchCard(t, root, "accepted-recovery")
	text, _ := setMetadata(s.Text, FieldSession, "codex original-session")
	text, _ = setMetadata(text, FieldWindow, "herdr:t1:p1")
	text, _ = setMetadata(text, FieldOwner, "codex")
	if err := WriteManagedDocument(root, s.Entry, text); err != nil {
		t.Fatal(err)
	}
	d := prepareTestDispatch(t, root, dispatchInput(s, "accepted-recovery"))
	if _, err := dispatchMove(t, root, d, "working"); err != nil {
		t.Fatal(err)
	}
	d, err := ReadDispatch(root, s.Entry.TaskID, d.Input.ID)
	if err != nil {
		t.Fatal(err)
	}
	d.Input.CreatedAt = time.Now().UTC().Add(-12 * time.Minute)
	d.Input.ConfirmBy = d.Input.CreatedAt.Add(120 * time.Second)
	d.Accepted.At = d.Input.CreatedAt.Add(time.Second)
	err = WithTransaction(root, LockScope{Tasks: []string{s.Entry.TaskID}}, func(tx *Transaction) error {
		for name, value := range map[string]any{"intent": d, "state": d, "accepted-1": d.Accepted} {
			if err := tx.Put(s.Entry.TaskID, dispatchPath(d.Input.ID, name), reviewJSON(value)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return root, d, transactionSnapshot(t, root, s.Entry.TaskID)
}

func TestAcceptedRecoveryRejectsUnprovenExit(t *testing.T) {
	root, d, s := expiredAcceptedFixture(t)
	for _, tc := range []struct {
		name   string
		change func(*WrapUpExitEvidence)
	}{
		{"alive", func(e *WrapUpExitEvidence) { e.Outcome = "alive" }},
		{"unknown", func(e *WrapUpExitEvidence) { e.Outcome = "unknown" }},
		{"expired", func(e *WrapUpExitEvidence) { e.ObservedAt = time.Now().Add(-31 * time.Second) }},
		{"future", func(e *WrapUpExitEvidence) { e.ObservedAt = time.Now().Add(time.Minute) }},
		{"missing-session", func(e *WrapUpExitEvidence) { e.Session = "" }},
		{"session", func(e *WrapUpExitEvidence) { e.Session += "other" }},
		{"window", func(e *WrapUpExitEvidence) { e.Window += "other" }},
		{"owner", func(e *WrapUpExitEvidence) { e.Owner += "other" }},
		{"started-at", func(e *WrapUpExitEvidence) { e.StartedAt += "other" }},
		{"revision", func(e *WrapUpExitEvidence) { e.CardRevision-- }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exit := stoppedDispatchFixture(s)
			tc.change(&exit)
			if _, err := ReauthorizeDispatch(root, d.Input.TaskID, d.Input.ID, d.Revision, exit); err == nil {
				t.Fatal("unproven observation rotated epoch")
			}
		})
	}
	if _, err := ReauthorizeDispatch(root, d.Input.TaskID, d.Input.ID, d.Revision); err == nil {
		t.Fatal("accepted takeover without stopped evidence")
	}
	got, err := ReadDispatch(root, d.Input.TaskID, d.Input.ID)
	if err != nil || !reflect.DeepEqual(got, d) {
		t.Fatalf("rejection changed dispatch: %+v %v", got, err)
	}
}

func TestAcceptedRecoveryPersistsDeadlineAndFencesWrites(t *testing.T) {
	root, d, stale := expiredAcceptedFixture(t)
	next, err := ReauthorizeDispatch(root, d.Input.TaskID, d.Input.ID, d.Revision, stoppedDispatchFixture(stale))
	if err != nil {
		t.Fatal(err)
	}
	if next.Execution.ConfirmBy.Sub(next.Execution.IssuedAt) != 120*time.Second || !reflect.DeepEqual(next.Input, d.Input) || next.Authorization.Epoch != 2 {
		t.Fatalf("deadline or intent: %+v", next)
	}
	if _, err = ReauthorizeDispatch(root, d.Input.TaskID, d.Input.ID, d.Revision, stoppedDispatchFixture(stale)); err == nil {
		t.Fatal("stale dispatch CAS rotated twice")
	}
	current := transactionSnapshot(t, root, d.Input.TaskID)
	for _, state := range []string{"working", "review", "done"} {
		if _, err = dispatchMove(t, root, d, state); err == nil {
			t.Fatal("old epoch moved", state)
		}
	}
	for _, document := range []string{"spec.md", "notes.md"} {
		if err = UpdateDocument(root, d.Input.TaskID, UpdateOptions{Document: document, Text: current.Text + "\nstale body\n", ExpectedRevision: current.Revision, Authorization: d.Authorization}); err == nil {
			t.Fatal("old epoch updated", document)
		}
	}
	// Even knowledge of the latest revision cannot rescue an old epoch.
	stale.Entry.Version.revision = current.Revision
	windowText, _ := setMetadata(stale.Text, FieldWindow, "herdr:t2:p2")
	if err = WriteManagedDocument(root, stale.Entry, windowText); err == nil {
		t.Fatal("old runtime writer updated WINDOW")
	}
	next, err = BeginDispatchAttempt(root, d.Input.TaskID, d.Input.ID, next.Revision, current.Revision)
	if err != nil {
		t.Fatal("new epoch could not send after old deadline", err)
	}
	if _, err = dispatchMove(t, root, next, "working"); err != nil {
		t.Fatal("new epoch could not accept", err)
	}
	if _, err = dispatchMove(t, root, next, "review"); err != nil {
		t.Fatal("new epoch could not complete", err)
	}
	current = transactionSnapshot(t, root, d.Input.TaskID)
	raw, err := os.ReadFile(filepath.Join(current.Entry.Path, dispatchPath(d.Input.ID, "execution-1")))
	var archived Dispatch
	if err != nil || json.Unmarshal(raw, &archived) != nil || !reflect.DeepEqual(archived, d) {
		t.Fatalf("lost retired execution: %s %v", raw, err)
	}
	code, out, stderr := capture(t, func() int { return RunDispatch([]string{"show", d.Input.TaskID, d.Input.ID}) })
	var shown Dispatch
	if code != 0 || json.Unmarshal([]byte(out), &shown) != nil || shown.State != DispatchCompleted || shown.Completed.DeliveryCommit != strings.Repeat("b", 40) || !shown.AcceptBefore().Equal(next.AcceptBefore()) || !shown.Input.ConfirmBy.Equal(d.Input.ConfirmBy) {
		t.Fatalf("show lost deadlines/receipt: %s %s", out, stderr)
	}
	// The deadline is checked against the execution original on every read.
	shown.Execution.ConfirmBy = shown.Execution.ConfirmBy.Add(time.Hour)
	if err = WithTransaction(root, LockScope{Tasks: []string{d.Input.TaskID}}, func(tx *Transaction) error { return putDispatch(tx, shown) }); err != nil {
		t.Fatal(err)
	}
	if _, err = ReadDispatch(root, d.Input.TaskID, d.Input.ID); err == nil {
		t.Fatal("modified execution deadline accepted")
	}
}

func TestPendingTakeoverRetainsRecoveredDeadline(t *testing.T) {
	root, old, s := expiredAcceptedFixture(t)
	recovered, err := ReauthorizeDispatch(root, old.Input.TaskID, old.Input.ID, old.Revision, stoppedDispatchFixture(s))
	if err != nil {
		t.Fatal(err)
	}
	next, err := ReauthorizeDispatch(root, old.Input.TaskID, old.Input.ID, recovered.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if next.Authorization.Epoch != recovered.Authorization.Epoch+1 || !next.AcceptBefore().Equal(recovered.AcceptBefore()) || !next.Input.ConfirmBy.Equal(old.Input.ConfirmBy) {
		t.Fatalf("pending takeover renewed or lost deadline: %+v", next)
	}
	if _, err = dispatchMove(t, root, recovered, "working"); err == nil {
		t.Fatal("retired recovery accepted")
	}
	if _, err = dispatchMove(t, root, next, "working"); err != nil {
		t.Fatal(err)
	}
}
