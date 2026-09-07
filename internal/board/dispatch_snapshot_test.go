package board

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDispatchSnapshotRetainsAtomicReceipts(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "dispatch-snapshot")
	id := s.Entry.TaskID
	d := prepareTestDispatch(t, root, dispatchInput(s, "snapshot-round"))
	read := func() Board {
		t.Helper()
		b, err := ScanDispatchesContext(context.Background(), root, []string{id})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	before := read()
	for _, state := range []string{"working", "review"} {
		if _, err := dispatchMove(t, root, d, state); err != nil {
			t.Fatal(err)
		}
	}
	after := read()
	old, err := before.CurrentDispatch(id)
	if err != nil || old.State != DispatchPrepared || old.Accepted != nil {
		t.Fatalf("old snapshot changed: %+v %v", old, err)
	}
	fact, err := after.CurrentDispatch(id)
	revision, revisionErr := after.Revision(id)
	if err != nil || revisionErr != nil || fact.State != DispatchCompleted || fact.Completed.CardRevision != revision || fact.Accepted.CardRevision != revision-1 || after.Entries[id].State != "review" {
		t.Fatalf("mixed snapshot: %+v rev=%d errors=%v %v", fact, revision, err, revisionErr)
	}
	fact.Completed.State = "forged"
	unchanged, err := after.CurrentDispatch(id)
	if err != nil || unchanged.Completed.State != "review" {
		t.Fatal("caller changed retained snapshot")
	}
	next := prepareTestDispatch(t, root, dispatchInput(transactionSnapshot(t, root, id), "next-round"))
	latest, err := read().CurrentDispatch(id)
	if err != nil || latest.ID != next.Input.ID || latest.Accepted != nil || latest.Epoch != old.Epoch+1 {
		t.Fatal("new grant reused old business receipt")
	}
	if previous, err := ReadDispatch(root, id, d.Input.ID); err != nil || previous.State != DispatchCompleted {
		t.Fatal("historical dispatch lost")
	}
}

func TestDispatchSnapshotReadFailuresAndCompatibility(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "dispatch-read")
	id := s.Entry.TaskID
	legacy, err := ScanDispatchesContext(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fact, err := legacy.CurrentDispatch(id); err != nil || fact != nil {
		t.Fatal("legacy card fabricated dispatch")
	}
	d := prepareTestDispatch(t, root, dispatchInput(s, "read-round"))
	if err := os.Remove(filepath.Join(s.Entry.Path, dispatchPath(d.Input.ID, "intent"))); err != nil {
		t.Fatal(err)
	}
	plain, err := ScanContext(context.Background(), root)
	if err != nil {
		t.Fatal("optional facts changed old scan", err)
	}
	if _, err := plain.CurrentDispatch(id); err == nil {
		t.Fatal("uncaptured facts reported absent")
	}
	broken, err := ScanDispatchesContext(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := broken.CurrentDispatch(id); err == nil {
		t.Fatal("missing intent reported absent")
	}
	locks, err := acquire(root, LockScope{Tasks: []string{id}})
	if err != nil {
		t.Fatal(err)
	}
	defer locks.close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = ScanDispatchesContext(ctx, root, []string{id})
	if err == nil || SnapshotReadStatus(err) != "maintenance" || time.Since(started) > time.Second {
		t.Fatalf("dispatch snapshot ignored lock deadline: %v", err)
	}
}
