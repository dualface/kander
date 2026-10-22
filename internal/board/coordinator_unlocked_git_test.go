package board

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// unlockedGitFixture is one wrap-up member whose reconciliation needs Git.
func unlockedGitFixture(t *testing.T, slug string) (root, id string, c CoordinatorCheckpoint) {
	t.Helper()
	root = tempBoard(t)
	s := coordinatorCard(t, root, slug)
	id = s.Entry.TaskID
	c = coordinatorClaim(t, root, id)
	exemptReviewFixture(t, root, id)
	prepareTestDispatch(t, root, coordinatorWrapInput(t, root, id, "wrap-"+slug))
	return root, id, c
}

func acceptGit(context.Context, CoordinatorGitFacts) error { return nil }

func TestCoordinatorGitRunsWithoutBoardLocks(t *testing.T) {
	root, id, c := unlockedGitFixture(t, "unlocked")
	calls := 0
	verify := func(context.Context, CoordinatorGitFacts) error {
		calls++
		var locks lockSet
		defer func() {
			if e := locks.close(); e != nil {
				t.Error(e)
			}
		}()
		for _, name := range []string{"board", c.GroupID, reviewControlGroup, dispatchRegistry, taskStartGroup, id, "journal"} {
			if e := locks.tryTake(root, control(root, "locks", name+".lock")); e != nil {
				t.Fatalf("%s lock held during Git verification: %v", name, e)
			}
		}
		if e := locks.close(); e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		b, e := ScanDispatchesContext(ctx, root, []string{id})
		if e != nil {
			t.Fatal("subscription read blocked during Git verification:", e)
		}
		if b.dispatches[id] == nil || b.dispatchErrors[id] != nil {
			t.Fatal("subscription read lacks dispatch facts", b.dispatchErrors[id])
		}
		return nil
	}
	next, e := ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), verify)
	if e != nil {
		t.Fatal(e)
	}
	if calls != 1 || next.Revision != c.Revision+1 || next.Members[id].Dispatch == nil {
		t.Fatal("reconciliation did not commit verified facts", calls, next)
	}
}

func TestCoordinatorBoardChangeDuringGitVerification(t *testing.T) {
	cases := []struct {
		name string
		code string
		// change runs while the outer verifier holds no lock; it returns the
		// checkpoint the board must hold after the outer call.
		change func(t *testing.T, root, id string, c CoordinatorCheckpoint, r CoordinatorReconcile) CoordinatorCheckpoint
		// replay reports that the outer call returns the committed checkpoint.
		replay bool
	}{
		{name: "card revision", code: "board.transaction_conflict", change: func(t *testing.T, root, id string, c CoordinatorCheckpoint, _ CoordinatorReconcile) CoordinatorCheckpoint {
			bumpCoordinatorCard(t, root, id)
			return c
		}},
		{name: "other request", code: "board.coordinator_conflict", change: func(t *testing.T, root, id string, c CoordinatorCheckpoint, _ CoordinatorReconcile) CoordinatorCheckpoint {
			bumpCoordinatorCard(t, root, id)
			next, e := ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), acceptGit)
			if e != nil {
				t.Fatal(e)
			}
			return next
		}},
		{name: "same request", replay: true, change: func(t *testing.T, root, _ string, _ CoordinatorCheckpoint, r CoordinatorReconcile) CoordinatorCheckpoint {
			next, e := ReconcileCoordinator(context.Background(), root, r, acceptGit)
			if e != nil {
				t.Fatal(e)
			}
			return next
		}},
		{name: "Git facts", code: "board.coordinator_conflict", change: func(t *testing.T, root, id string, c CoordinatorCheckpoint, _ CoordinatorReconcile) CoordinatorCheckpoint {
			// No producer rewrites a closure; the test edits both copies in place
			// so the closure differs while the card revision stays the same.
			s := transactionSnapshot(t, root, id)
			batch := "exempt-" + id
			if e := WithTransaction(root, LockScope{Groups: []string{reviewControlGroup}}, func(tx *Transaction) error {
				var closure ReviewClosure
				if _, e := readReviewJSON(tx, closureName(batch), &closure); e != nil {
					return e
				}
				closure.ClosedAt = time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
				text := reviewJSON(closure)
				if e := os.WriteFile(filepath.Join(s.Entry.Path, "reviews", "batches", batch, "closed.json"), []byte(text), 0600); e != nil {
					return e
				}
				return tx.PutGroup(reviewControlGroup, closureName(batch), text)
			}); e != nil {
				t.Fatal(e)
			}
			if transactionSnapshot(t, root, id).Revision != s.Revision {
				t.Fatal("closure change moved the card revision")
			}
			return c
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, id, c := unlockedGitFixture(t, "changed")
			r := coordinatorRequest(t, root, c)
			var want CoordinatorCheckpoint
			verify := func(context.Context, CoordinatorGitFacts) error {
				want = tc.change(t, root, id, c, r)
				return nil
			}
			got, e := ReconcileCoordinator(context.Background(), root, r, verify)
			if tc.replay {
				if e != nil || !reflect.DeepEqual(got, want) {
					t.Fatal("same request did not replay its committed result", e)
				}
			} else {
				var ke *Error
				if !errors.As(e, &ke) || ke.Code != tc.code {
					t.Fatalf("want %s, got %v", tc.code, e)
				}
			}
			after, e := ReadCoordinatorCheckpoint(root, c.GroupID)
			if e != nil || !reflect.DeepEqual(after, want) {
				t.Fatal("outer reconciliation changed the checkpoint", e)
			}
		})
	}
}

func TestCoordinatorGitFailureKeepsCheckpoint(t *testing.T) {
	root, _, c := unlockedGitFixture(t, "failed")
	absent := errors.New("Git ancestry absent")
	if _, e := ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), func(context.Context, CoordinatorGitFacts) error { return absent }); !errors.Is(e, absent) {
		t.Fatal("Git error not returned", e)
	}
	after, e := ReadCoordinatorCheckpoint(root, c.GroupID)
	if e != nil || !reflect.DeepEqual(after, c) {
		t.Fatal("Git failure changed the checkpoint", e)
	}
}

func bumpCoordinatorCard(t *testing.T, root, id string) {
	t.Helper()
	s := transactionSnapshot(t, root, id)
	text, e := setMetadata(s.Text, FieldSession, "bumped")
	if e != nil {
		t.Fatal(e)
	}
	if e = WriteManagedDocument(root, s.Entry, text); e != nil {
		t.Fatal(e)
	}
}

func TestCoordinatorInvalidCardFailsBeforeGit(t *testing.T) {
	root, id, c := unlockedGitFixture(t, "invalid")
	r := coordinatorRequest(t, root, c)
	s := transactionSnapshot(t, root, id)
	text := strings.Replace(s.Text, "- SIZE: small", "- SIZE: huge", 1)
	if text == s.Text {
		t.Fatal("fixture has no SIZE line")
	}
	if e := os.WriteFile(filepath.Join(s.Entry.Path, "spec.md"), []byte(text), 0600); e != nil {
		t.Fatal(e)
	}
	calls := 0
	_, e := ReconcileCoordinator(context.Background(), root, r, func(context.Context, CoordinatorGitFacts) error {
		calls++
		return errors.New("Git ancestry absent")
	})
	var ke *Error
	if !errors.As(e, &ke) || ke.Code != "board.size_invalid" || calls != 0 {
		t.Fatal("card error not reported before Git", e, calls)
	}
	after, e := ReadCoordinatorCheckpoint(root, c.GroupID)
	if e != nil || !reflect.DeepEqual(after, c) {
		t.Fatal("invalid card changed the checkpoint", e)
	}
}
