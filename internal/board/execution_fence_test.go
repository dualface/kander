package board

import (
	"errors"
	"strings"
	"testing"
)

func requireConflict(t *testing.T, err error) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Code != "board.transaction_conflict" {
		t.Fatalf("err=%v", err)
	}
}

// A snapshot read before a takeover belongs to the replaced execution. Binding it
// must fail: a caller holding it probed that execution, not the new one.
func TestBindRefusesARecordNewerThanTheSnapshot(t *testing.T) {
	root := tempBoard(t)
	moved := startedCard(t, root, "stale-snapshot")
	id := moved.TaskID
	if err := ConfirmTaskStart(root, moved); err != nil {
		t.Fatal(err)
	}
	stale := transactionSnapshot(t, root, id)
	s := bound(t, root, id)
	if _, err := BeginTakeover(root, s.Entry, TakeoverInput{Owner: "claude", Session: "claude next", Window: "tmux:new"}); err != nil {
		t.Fatal(err)
	}
	if _, err := BindExecutionCursor(root, stale.Entry); err == nil {
		t.Fatal("a stale snapshot bound the successor record")
	} else {
		requireConflict(t, err)
	}
	if _, err := RecordExecutionWindow(root, stale.Entry, "tmux:old", ""); err == nil {
		t.Fatal("the stale cursor rewrote the successor's WINDOW")
	}
	if _, err := AdoptExecution(root, stale.Entry, "user"); err == nil {
		t.Fatal("the stale cursor adopted the successor")
	}
	if got := MetadataFrom(transactionSnapshot(t, root, id).Text, FieldWindow); got != "tmux:new" {
		t.Fatalf("window=%q", got)
	}
}

// The first stored record of a legacy card replaces the synthesized one the same way.
func TestBindRefusesTheFirstRecordAfterALegacySnapshot(t *testing.T) {
	root := tempBoard(t)
	stale := legacyStartedCard(t, root, "legacy-stale")
	s := bound(t, root, stale.Entry.TaskID)
	if _, err := BeginTakeover(root, s.Entry, TakeoverInput{Owner: "claude", Session: "claude next"}); err != nil {
		t.Fatal(err)
	}
	if _, err := BindExecutionCursor(root, stale.Entry); err == nil {
		t.Fatal("a legacy snapshot bound the first stored record")
	}
}

// Confirmation keeps the record, so it never invalidates a snapshot of that record.
func TestBindAcceptsAConfirmedRecord(t *testing.T) {
	root := tempBoard(t)
	moved := startedCard(t, root, "confirmed-snapshot")
	s := transactionSnapshot(t, root, moved.TaskID)
	if err := ConfirmTaskStart(root, moved); err != nil {
		t.Fatal(err)
	}
	if _, err := BindExecutionCursor(root, s.Entry); err != nil {
		t.Fatal(err)
	}
}

func TestBindRefusesACursorThatPinnedNothing(t *testing.T) {
	root := tempBoard(t)
	moved := startedCard(t, root, "scan-cursor")
	loaded, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := Locate(loaded, moved.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = BindExecutionCursor(root, entry); err == nil {
		t.Fatal("a cursor that read no execution was bound")
	}
}

// A dispatch that bound the card after the cursor was read owns the card. The
// cursor's empty authorization no longer matches.
func TestEmptyAuthorizationCannotWriteABoundCard(t *testing.T) {
	root := tempBoard(t)
	moved := startedCard(t, root, "empty-authorization")
	id := moved.TaskID
	if err := ConfirmTaskStart(root, moved); err != nil {
		t.Fatal(err)
	}
	s := bound(t, root, id)
	d := prepareTestDispatch(t, root, DispatchInput{ID: "later-round", TaskID: id, Kind: "sync", Message: "同步", Base: strings.Repeat("a", 40)})
	if _, err := RecordExecutionWindow(root, s.Entry, "tmux:old", ""); err == nil {
		t.Fatal("an unauthorized cursor wrote WINDOW on a bound card")
	}
	if _, err := RecordExecutionSession(t.Context(), root, s.Entry, "codex", "codex other"); err == nil {
		t.Fatal("an unauthorized cursor wrote SESSION on a bound card")
	}
	current := bound(t, root, id)
	if current.Entry.Version.authorization != d.Authorization {
		t.Fatalf("authorization %+v", current.Entry.Version.authorization)
	}
	if _, err := RecordExecutionWindow(root, current.Entry, "tmux:new", ""); err != nil {
		t.Fatal("the matching authorization was refused:", err)
	}
}

func TestPendingTakeoverDropsTheMirrorDispatchAndKeepsItsPosition(t *testing.T) {
	for _, status := range []string{ExecutionPending, ExecutionUnresolved} {
		t.Run(status, func(t *testing.T) {
			root := tempBoard(t)
			moved := startedCard(t, root, "waiting-mirror")
			id := moved.TaskID
			if err := ConfirmTaskStart(root, moved); err != nil {
				t.Fatal(err)
			}
			c := coordinatorClaim(t, root, id)
			d := prepareTestDispatch(t, root, DispatchInput{ID: "held-round", TaskID: id, Kind: "sync", Message: "同步", Base: strings.Repeat("a", 40)})
			if _, err := dispatchMove(t, root, d, "working"); err != nil {
				t.Fatal(err)
			}
			c = coordinatorReconcile(t, root, c)
			if c.Members[id].Dispatch == nil || c.Members[id].Dispatch.State != DispatchAccepted {
				t.Fatalf("mirror %+v", c.Members[id])
			}
			s := transactionSnapshot(t, root, id)
			read, err := ReadDispatch(root, id, d.Input.ID)
			if err != nil {
				t.Fatal(err)
			}
			if read, err = ReauthorizeDispatch(root, id, d.Input.ID, read.Revision, modelStopped(s)); err != nil {
				t.Fatal(err)
			}
			s = bound(t, root, id)
			if _, err = BeginTakeover(root, s.Entry, TakeoverInput{Owner: "claude", Session: "claude next", Decision: "user"}); err != nil {
				t.Fatal(err)
			}
			if status == ExecutionUnresolved {
				if _, err = UnresolveExecution(root, s.Entry, "", "late failure"); err != nil {
					t.Fatal(err)
				}
			}
			c = coordinatorReconcile(t, root, c)
			if m := c.Members[id]; !m.AwaitingStart || m.Dispatch != nil || m.DispatchSeq != 0 {
				t.Fatalf("mirror %+v", m)
			}
			if status == ExecutionUnresolved {
				s = bound(t, root, id)
				if _, err = AdoptExecution(root, s.Entry, "user"); err != nil {
					t.Fatal(err)
				}
			} else if err = ConfirmTaskStart(root, s.Entry); err != nil {
				t.Fatal(err)
			}
			c = coordinatorReconcile(t, root, c)
			if m := c.Members[id]; m.AwaitingStart || m.Dispatch == nil || m.Dispatch.Epoch != read.Authorization.Epoch {
				t.Fatalf("mirror %+v", m)
			}
		})
	}
}
