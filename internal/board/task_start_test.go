package board

import (
	"context"
	"os"
	"reflect"
	"testing"
)

func startAttemptFixture(t *testing.T, root string, original Snapshot) Entry {
	t.Helper()
	moved, err := MoveEntry(original.Entry, root, "working")
	if err != nil {
		t.Fatal(err)
	}
	text, err := moveMetadata(original.Text, "todo", "working", MoveOptions{Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	// Keep the timestamp identical across retries to prove identity fencing
	// does not accidentally depend on the wall clock's minute precision.
	text, err = setMetadata(text, FieldStartedAt, "2026-09-08 10:00")
	if err != nil {
		t.Fatal(err)
	}
	if err = WriteManagedDocument(root, moved, text); err != nil {
		t.Fatal(err)
	}
	return moved
}

func TestStartResultsFenceRetryAndPreserveTaskRevision(t *testing.T) {
	root := tempBoard(t)
	original := coordinatorTodo(t, root, "attempt-fence")
	first := startAttemptFixture(t, root, original)
	// A coordinator first appearing during a launch cannot adopt metadata as
	// confirmation, even though it never observed the original todo snapshot.
	c := coordinatorClaim(t, root, original.Entry.TaskID)
	c = coordinatorReconcile(t, root, c)
	if !c.Members[original.Entry.TaskID].AwaitingStart {
		t.Fatal("mid-launch claim confirmed execution")
	}
	if err := RollbackDocument(root, first, original.Text, "todo"); err != nil {
		t.Fatal(err)
	}
	second := startAttemptFixture(t, root, transactionSnapshot(t, root, original.Entry.TaskID))
	if err := ConfirmTaskStart(root, first); err == nil {
		t.Fatal("stale launcher confirmed a same-minute retry")
	}
	c = coordinatorReconcile(t, root, c)
	if !c.Members[original.Entry.TaskID].AwaitingStart {
		t.Fatal("retry confirmed without result")
	}
	// A fast executor can write or move before launcher confirmation. Success
	// changes only result evidence, preserving the newer task and its revision.
	s := transactionSnapshot(t, root, original.Entry.TaskID)
	updateSnapshot(t, root, s, s.Text+"\n新作者工作记录。\n")
	s = transactionSnapshot(t, root, original.Entry.TaskID)
	if _, err := MoveEntry(s.Entry, root, "review"); err != nil {
		t.Fatal(err)
	}
	before := transactionSnapshot(t, root, original.Entry.TaskID)
	if err := ConfirmTaskStart(root, second); err != nil {
		t.Fatal(err)
	}
	after := transactionSnapshot(t, root, original.Entry.TaskID)
	if after.Revision != before.Revision || after.Text != before.Text || after.Entry.State != "review" {
		t.Fatal("success overwrote fast executor work")
	}
	c = coordinatorReconcile(t, root, c)
	if c.Members[original.Entry.TaskID].AwaitingStart {
		t.Fatal("successful result not consumed")
	}
	if err := ConfirmTaskStart(root, second); err != nil {
		t.Fatal("success replay failed", err)
	}
	if next := coordinatorReconcile(t, root, c); !reflect.DeepEqual(next, c) {
		t.Fatal("success replay changed checkpoint")
	}
	if err := RollbackDocument(root, second, original.Text, "todo"); err == nil {
		t.Fatal("stale rollback erased executor work")
	}
}

func TestStartOriginalDamageStopsRecovery(t *testing.T) {
	for _, damage := range []string{"pending", "rollback", "success", "current", "unobserved-current", "adopted-rollback"} {
		t.Run(damage, func(t *testing.T) {
			root := tempBoard(t)
			original := coordinatorTodo(t, root, "damaged-start")
			c := coordinatorClaim(t, root, original.Entry.TaskID)
			first := startAttemptFixture(t, root, original)
			if damage != "unobserved-current" {
				c = coordinatorReconcile(t, root, c)
			}
			id := c.Members[original.Entry.TaskID].StartAttempt
			path := taskStartPath(original.Entry.TaskID, id, "pending")
			if damage == "rollback" || damage == "adopted-rollback" {
				if err := RollbackDocument(root, first, original.Text, "todo"); err != nil {
					t.Fatal(err)
				}
				startAttemptFixture(t, root, transactionSnapshot(t, root, original.Entry.TaskID))
				path = taskStartPath(original.Entry.TaskID, id, "result")
				if damage == "adopted-rollback" {
					c = coordinatorReconcile(t, root, c)
				}
			}
			if damage == "success" {
				if err := ConfirmTaskStart(root, first); err != nil {
					t.Fatal(err)
				}
				path = taskStartPath(original.Entry.TaskID, id, "result")
			}
			if damage == "current" || damage == "unobserved-current" {
				path = original.Entry.TaskID + "/current.json"
			}
			if err := os.Remove(control(root, "groups", taskStartGroup, path)); err != nil {
				t.Fatal(err)
			}
			if _, err := ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), nil); err == nil {
				t.Fatal("missing start original accepted")
			}
			after, err := ReadCoordinatorCheckpoint(root, c.GroupID)
			if err != nil || !reflect.DeepEqual(after, c) {
				t.Fatal("damage changed checkpoint", err)
			}
		})
	}
}

func TestConfirmedStartCannotBeRolledBackOrSilentlyReplaced(t *testing.T) {
	root := tempBoard(t)
	original := coordinatorTodo(t, root, "confirmed-start")
	moved := startAttemptFixture(t, root, original)
	if err := ConfirmTaskStart(root, moved); err != nil {
		t.Fatal(err)
	}
	c := coordinatorClaim(t, root, original.Entry.TaskID)
	c = coordinatorReconcile(t, root, c)
	before := transactionSnapshot(t, root, original.Entry.TaskID)
	if err := RollbackDocument(root, moved, original.Text, "todo"); err == nil {
		t.Fatal("confirmed launcher rolled back")
	}
	after := transactionSnapshot(t, root, original.Entry.TaskID)
	if after.Text != before.Text || after.Revision != before.Revision {
		t.Fatal("rejected rollback changed task")
	}
	text, err := setMetadata(after.Text, FieldStartedAt, "2026-09-08 10:01")
	if err != nil {
		t.Fatal(err)
	}
	if err = WriteManagedDocument(root, after.Entry, text); err != nil {
		t.Fatal(err)
	}
	if _, err = ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), nil); err == nil {
		t.Fatal("confirmed cycle silently replaced")
	}
}

func TestStartRollbackProofRevokesPrematureCursorAndAllowsManualClaim(t *testing.T) {
	root := tempBoard(t)
	original := coordinatorTodo(t, root, "premature")
	c := coordinatorClaim(t, root, original.Entry.TaskID)
	moved := startAttemptFixture(t, root, original)
	s := transactionSnapshot(t, root, original.Entry.TaskID)
	// Preserve a cursor shaped like the earlier metadata-only implementation.
	if err := WithTransaction(root, LockScope{Groups: []string{c.GroupID}}, func(tx *Transaction) error {
		c.Members[original.Entry.TaskID] = CoordinatorMember{Revision: s.Revision, Cycle: planCycle(s), State: "working"}
		c.Revision++
		return putCheckpoint(tx, &c)
	}); err != nil {
		t.Fatal(err)
	}
	if err := RollbackDocument(root, moved, original.Text, "todo"); err != nil {
		t.Fatal(err)
	}
	c = coordinatorReconcile(t, root, c)
	if !c.Members[original.Entry.TaskID].AwaitingStart {
		t.Fatal("exact rollback proof did not revoke temporary binding")
	}
	s = transactionSnapshot(t, root, original.Entry.TaskID)
	if _, err := MoveWithOptions(s.Entry, root, "working", MoveOptions{Owner: "codex"}); err != nil {
		t.Fatal(err)
	}
	c = coordinatorReconcile(t, root, c)
	if c.Members[original.Entry.TaskID].AwaitingStart {
		t.Fatal("explicit manual claim blocked")
	}
	if next := coordinatorReconcile(t, root, c); !reflect.DeepEqual(next, c) {
		t.Fatal("manual claim did not remain stable")
	}
}

func TestStartSuccessAndRollbackHaveOneWinner(t *testing.T) {
	root := tempBoard(t)
	original := coordinatorTodo(t, root, "result-race")
	c := coordinatorClaim(t, root, original.Entry.TaskID)
	moved := startAttemptFixture(t, root, original)
	independent := transactionSnapshot(t, root, original.Entry.TaskID)
	ready := make(chan struct{})
	results := make(chan error, 2)
	go func() { <-ready; results <- ConfirmTaskStart(root, moved) }()
	go func() { <-ready; results <- RollbackDocument(root, independent.Entry, original.Text, "todo") }()
	close(ready)
	winners := 0
	for range 2 {
		if <-results == nil {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("start result winners=%d", winners)
	}
	c = coordinatorReconcile(t, root, c)
	s := transactionSnapshot(t, root, original.Entry.TaskID)
	if c.Members[original.Entry.TaskID].AwaitingStart != (s.Entry.State == "todo") {
		t.Fatal("result disagrees with committed task")
	}
}
