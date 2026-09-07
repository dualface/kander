package board

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const coordinatorGroup = "20260908-coordinator-test-group"

func coordinatorCard(t *testing.T, root, slug string) Snapshot {
	t.Helper()
	s := dispatchCard(t, root, slug)
	if err := WithTransaction(root, LockScope{Tasks: []string{s.Entry.TaskID}}, func(tx *Transaction) error {
		text, e := setMetadata(s.Text, "TASK_GROUP", coordinatorGroup)
		if e != nil {
			return e
		}
		return tx.Put(s.Entry.TaskID, "spec.md", text)
	}); err != nil {
		t.Fatal(err)
	}
	return transactionSnapshot(t, root, s.Entry.TaskID)
}

func coordinatorClaim(t *testing.T, root string, ids ...string) CoordinatorCheckpoint {
	t.Helper()
	c, e := ClaimCoordinator(context.Background(), root, CoordinatorClaim{GroupID: coordinatorGroup, Owner: "coordinator", Token: "session-one", Basis: "测试编排授权", Members: ids})
	if e != nil {
		t.Fatal(e)
	}
	return c
}

func coordinatorRequest(t *testing.T, root string, c CoordinatorCheckpoint) CoordinatorReconcile {
	t.Helper()
	r := CoordinatorReconcile{GroupID: c.GroupID, ExpectedRevision: c.Revision, Authority: c.Authority, Members: map[string]CoordinatorObservation{}}
	for id := range c.Members {
		s := transactionSnapshot(t, root, id)
		o := CoordinatorObservation{Revision: s.Revision}
		if a := authFrom(s.Text); a.DispatchID != "" {
			d, e := ReadDispatch(root, id, a.DispatchID)
			if e != nil {
				t.Fatal(e)
			}
			o.DispatchID, o.Epoch, o.Base = a.DispatchID, a.Epoch, d.Input.Base
			if d.Completed != nil {
				o.DeliveryCommit = d.Completed.DeliveryCommit
			}
		}
		r.Members[id] = o
	}
	return r
}

func coordinatorReconcile(t *testing.T, root string, c CoordinatorCheckpoint) CoordinatorCheckpoint {
	t.Helper()
	next, e := ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), nil)
	if e != nil {
		t.Fatal(e)
	}
	return next
}

func TestCoordinatorConcurrentClaimAndFencing(t *testing.T) {
	root := tempBoard(t)
	s := coordinatorCard(t, root, "race")
	requests := []CoordinatorClaim{
		{GroupID: coordinatorGroup, Owner: "one", Token: "one", Basis: "授权", Members: []string{s.Entry.TaskID}},
		{GroupID: coordinatorGroup, Owner: "two", Token: "two", Basis: "授权", Members: []string{s.Entry.TaskID}},
	}
	var wg sync.WaitGroup
	results := make(chan CoordinatorCheckpoint, 2)
	for _, r := range requests {
		wg.Add(1)
		go func(r CoordinatorClaim) {
			defer wg.Done()
			c, e := ClaimCoordinator(context.Background(), root, r)
			if e == nil {
				results <- c
			}
		}(r)
	}
	wg.Wait()
	close(results)
	var first CoordinatorCheckpoint
	count := 0
	for c := range results {
		first = c
		count++
	}
	if count != 1 {
		t.Fatalf("writers=%d", count)
	}
	oldRequest := coordinatorRequest(t, root, first)
	r := CoordinatorClaim{GroupID: coordinatorGroup, ExpectedRevision: first.Revision, ExpectedEpoch: first.Authority.Epoch, Owner: "successor", Token: "new-session", Basis: "明确恢复同组", Members: []string{s.Entry.TaskID}}
	next, e := ClaimCoordinator(context.Background(), root, r)
	if e != nil {
		t.Fatal(e)
	}
	if next.Authority.Epoch != first.Authority.Epoch+1 || !reflect.DeepEqual(next.Members, first.Members) {
		t.Fatal("takeover lost cursor")
	}
	if _, e = ReconcileCoordinator(context.Background(), root, oldRequest, nil); e == nil {
		t.Fatal("old coordinator wrote")
	}
	replay, e := ClaimCoordinator(context.Background(), root, r)
	if e != nil || !reflect.DeepEqual(replay, next) {
		t.Fatal("claim replay advanced epoch", e)
	}
	if _, e = ClaimCoordinator(context.Background(), root, requests[0]); e == nil {
		t.Fatal("stale initial claim reclaimed authority")
	}
}

func TestCoordinatorSnapshotCompletesLostRoundTripOnce(t *testing.T) {
	root := tempBoard(t)
	s := coordinatorCard(t, root, "roundtrip")
	c := coordinatorClaim(t, root, s.Entry.TaskID)
	d := prepareTestDispatch(t, root, dispatchInput(s, "one-round"))
	c = coordinatorReconcile(t, root, c)
	if !c.Members[s.Entry.TaskID].Dispatch.PendingConfirmation {
		t.Fatal("prepared confirmed")
	}
	stale := coordinatorRequest(t, root, c)
	for _, state := range []string{"working", "review"} {
		if _, e := dispatchMove(t, root, d, state); e != nil {
			t.Fatal(e)
		}
	}
	// Recreate all consumer state from the checkpoint after executor/subscriber
	// restart; no working edge was observed and notify need not have returned.
	c, e := ReadCoordinatorCheckpoint(root, coordinatorGroup)
	if e != nil {
		t.Fatal(e)
	}
	r := coordinatorRequest(t, root, c)
	next, e := ReconcileCoordinator(context.Background(), root, r, nil)
	if e != nil {
		t.Fatal(e)
	}
	m := next.Members[s.Entry.TaskID]
	if m.Dispatch.PendingConfirmation || m.Dispatch.PendingDelivery || m.DeliveryCommit != strings.Repeat("b", 40) {
		t.Fatal(m)
	}
	before, e := operationRecords(root)
	if e != nil {
		t.Fatal(e)
	}
	for range 3 {
		replay, e := ReconcileCoordinator(context.Background(), root, r, nil)
		if e != nil || !reflect.DeepEqual(replay, next) {
			t.Fatal("duplicate changed checkpoint", e)
		}
		next = coordinatorReconcile(t, root, next)
	}
	after, e := operationRecords(root)
	if e != nil || len(before) != len(after) {
		t.Fatal("duplicate transaction", e)
	}
	if _, e = ReconcileCoordinator(context.Background(), root, stale, nil); e == nil {
		t.Fatal("reordered stale event overwrote completion")
	}
	if _, e = ReadSnapshot(root, s.Entry.TaskID); e != nil {
		t.Fatal(e)
	}
}

func TestCoordinatorRejectsUnprovenFactsWithoutWrites(t *testing.T) {
	for _, kind := range []string{"wrong-id", "old-epoch", "old-base", "old-delivery", "unknown-member", "old-revision"} {
		t.Run(kind, func(t *testing.T) {
			root := tempBoard(t)
			s := coordinatorCard(t, root, "invalid")
			c := coordinatorClaim(t, root, s.Entry.TaskID)
			d := prepareTestDispatch(t, root, dispatchInput(s, "round"))
			for _, state := range []string{"working", "review"} {
				if _, e := dispatchMove(t, root, d, state); e != nil {
					t.Fatal(e)
				}
			}
			r := coordinatorRequest(t, root, c)
			o := r.Members[s.Entry.TaskID]
			switch kind {
			case "wrong-id":
				o.DispatchID = "other"
			case "old-epoch":
				o.Epoch++
			case "old-base":
				o.Base = strings.Repeat("c", 40)
			case "old-delivery":
				o.DeliveryCommit = strings.Repeat("c", 40)
			case "unknown-member":
				r.Members["20260908-unknown-task"] = o
			case "old-revision":
				o.Revision--
			}
			r.Members[s.Entry.TaskID] = o
			if _, e := ReconcileCoordinator(context.Background(), root, r, nil); e == nil {
				t.Fatal("invalid evidence accepted")
			}
			after, e := ReadCoordinatorCheckpoint(root, c.GroupID)
			if e != nil || !reflect.DeepEqual(c, after) {
				t.Fatal("failed observation changed cursor", e)
			}
		})
	}
}

func TestCoordinatorMembershipAndCorruptionStop(t *testing.T) {
	root := tempBoard(t)
	s := coordinatorCard(t, root, "member")
	c := coordinatorClaim(t, root, s.Entry.TaskID)
	coordinatorCard(t, root, "added")
	if _, e := ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), nil); e == nil {
		t.Fatal("unknown group member silently omitted")
	}
	p := control(root, "groups", c.GroupID, "checkpoint.json")
	data, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	data = []byte(strings.Replace(string(data), `"state": "review"`, `"state": "done"`, 1))
	if e = os.WriteFile(p, data, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = ReadCoordinatorCheckpoint(root, c.GroupID); e == nil {
		t.Fatal("damaged checkpoint accepted")
	}
	if e = os.Remove(p); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(filepath.Join(t.TempDir(), "outside"), p); e != nil {
		t.Skip(e)
	}
	if _, e = ReadCoordinatorCheckpoint(root, c.GroupID); e == nil {
		t.Fatal("reparse checkpoint accepted")
	}
}

func TestCoordinatorDeadlinePreservesCursor(t *testing.T) {
	root := tempBoard(t)
	s := coordinatorCard(t, root, "deadline")
	c := coordinatorClaim(t, root, s.Entry.TaskID)
	r := coordinatorRequest(t, root, c)
	locks, e := acquire(root, LockScope{ExclusiveBoard: true})
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, e = ReconcileCoordinator(ctx, root, r, nil)
	if closeErr := locks.close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	after, e := ReadCoordinatorCheckpoint(root, c.GroupID)
	if e != nil || !reflect.DeepEqual(c, after) {
		t.Fatal("deadline changed checkpoint", e)
	}
}

func TestCoordinatorAdvancesOnlyFromPersistedRoundAndEpoch(t *testing.T) {
	root := tempBoard(t)
	s := coordinatorCard(t, root, "advance")
	c := coordinatorClaim(t, root, s.Entry.TaskID)
	d := prepareTestDispatch(t, root, dispatchInput(s, "first-round"))
	c = coordinatorReconcile(t, root, c)
	for _, state := range []string{"working", "review"} {
		if _, e := dispatchMove(t, root, d, state); e != nil {
			t.Fatal(e)
		}
	}
	// Another authorized coordinator may have prepared the successor before
	// this session saw the first completion. The old original must close it.
	d = prepareTestDispatch(t, root, dispatchInput(transactionSnapshot(t, root, s.Entry.TaskID), "next-round"))
	c = coordinatorReconcile(t, root, c)
	if c.Members[s.Entry.TaskID].Dispatch.ID != "next-round" {
		t.Fatal("missed completion blocked legitimate successor")
	}
	next, e := ReauthorizeDispatch(root, s.Entry.TaskID, d.Input.ID, d.Revision)
	if e != nil {
		t.Fatal(e)
	}
	c = coordinatorReconcile(t, root, c)
	if c.Members[s.Entry.TaskID].Dispatch.Epoch != next.Authorization.Epoch {
		t.Fatal("existing takeover original not consumed")
	}
	if _, e = dispatchMove(t, root, d, "working"); e == nil {
		t.Fatal("old execution grant remained usable")
	}
	// A missing isolation original prevents adoption of another epoch.
	last, e := ReauthorizeDispatch(root, s.Entry.TaskID, next.Input.ID, next.Revision)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(transactionSnapshot(t, root, s.Entry.TaskID).Entry.Path, dispatchPath(last.Input.ID, "execution-"+strconv.FormatUint(next.Authorization.Epoch, 10)))
	if e = os.Remove(path); e != nil {
		t.Fatal(e)
	}
	if _, e = ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), nil); e == nil {
		t.Fatal("new epoch accepted without isolation original")
	}
}
