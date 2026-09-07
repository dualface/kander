package board

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
)

func coordinatorTodo(t *testing.T, root, slug string) Snapshot {
	t.Helper()
	s := transactionCard(t, root, slug, false)
	text, err := setMetadata(strings.ReplaceAll(readyText(s), "<FILL_IN>", "fixture"), "TASK_GROUP", coordinatorGroup)
	if err != nil {
		t.Fatal(err)
	}
	text = strings.Replace(text, "- TASK_BRANCH:", "- TASK_BRANCH: coordinator-cycle-test", 1)
	updateSnapshot(t, root, s, text)
	s = transactionSnapshot(t, root, s.Entry.TaskID)
	if _, err = MoveEntry(s.Entry, root, "todo"); err != nil {
		t.Fatal(err)
	}
	return transactionSnapshot(t, root, s.Entry.TaskID)
}

func TestCoordinatorBindsFirstStartFromCompleteMemberSnapshot(t *testing.T) {
	for _, fast := range []bool{false, true} {
		t.Run(map[bool]string{false: "working", true: "missed-working"}[fast], func(t *testing.T) {
			root := tempBoard(t)
			a := coordinatorCard(t, root, "running")
			b := coordinatorTodo(t, root, "waiting")
			c := coordinatorClaim(t, root, a.Entry.TaskID, b.Entry.TaskID)
			if !c.Members[b.Entry.TaskID].AwaitingStart {
				t.Fatal("todo member has no explicit waiting state")
			}
			history := control(root, "groups", c.GroupID, "checkpoints", "1.json")
			original, err := os.ReadFile(history)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = MoveWithOptions(b.Entry, root, "working", MoveOptions{Owner: "codex"}); err != nil {
				t.Fatal(err)
			}
			if fast {
				s := transactionSnapshot(t, root, b.Entry.TaskID)
				if _, err = MoveEntry(s.Entry, root, "review"); err != nil {
					t.Fatal(err)
				}
			}
			// Recovery reclaims only coordinator authority; it must preserve the
			// waiting cursor until the same snapshot path proves the first start.
			c, err = ClaimCoordinator(context.Background(), root, CoordinatorClaim{GroupID: c.GroupID, ExpectedRevision: c.Revision, ExpectedEpoch: c.Authority.Epoch, Owner: "restarted", Token: "restart", Basis: "恢复编排", Members: checkpointIDs(c)})
			if err != nil {
				t.Fatal(err)
			}
			r := coordinatorRequest(t, root, c)
			next, err := ReconcileCoordinator(context.Background(), root, r, nil)
			if err != nil {
				t.Fatal("first start blocked full group recovery", err)
			}
			started := transactionSnapshot(t, root, b.Entry.TaskID)
			if next.Members[b.Entry.TaskID].AwaitingStart || next.Members[b.Entry.TaskID].Cycle != planCycle(started) || next.Revision != c.Revision+1 || !reflect.DeepEqual(next.Members[a.Entry.TaskID], c.Members[a.Entry.TaskID]) {
				t.Fatal("first start did not bind exactly once")
			}
			replay, err := ReconcileCoordinator(context.Background(), root, r, nil)
			if err != nil || !reflect.DeepEqual(replay, next) {
				t.Fatal("replay changed binding", err)
			}
			after, err := os.ReadFile(history)
			if err != nil || string(after) != string(original) {
				t.Fatal("first start rewrote checkpoint history", err)
			}
			// Simulate an unauthorized replacement of a producer-managed field.
			if err = WithTransaction(root, LockScope{Tasks: []string{b.Entry.TaskID}}, func(tx *Transaction) error {
				text, e := setMetadata(started.Text, FieldStartedAt, "2000-01-01 00:00")
				if e != nil {
					return e
				}
				return tx.Put(b.Entry.TaskID, "spec.md", text)
			}); err != nil {
				t.Fatal(err)
			}
			if _, err = ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, next), nil); err == nil {
				t.Fatal("already bound cycle silently replaced")
			}
			afterCheckpoint, err := ReadCoordinatorCheckpoint(root, c.GroupID)
			if err != nil || !reflect.DeepEqual(afterCheckpoint, next) {
				t.Fatal("invalid cycle changed cursor", err)
			}
		})
	}
}

func TestCoordinatorFirstStartRequiresPersistentFactsAndCAS(t *testing.T) {
	for _, kind := range []string{"missing-owner", "still-todo", "stale-revision", "stale-checkpoint"} {
		t.Run(kind, func(t *testing.T) {
			root := tempBoard(t)
			s := coordinatorTodo(t, root, "invalid-start")
			c := coordinatorClaim(t, root, s.Entry.TaskID)
			if _, err := MoveWithOptions(s.Entry, root, "working", MoveOptions{Owner: "codex"}); err != nil {
				t.Fatal(err)
			}
			if kind == "missing-owner" || kind == "still-todo" {
				if err := WithTransaction(root, LockScope{Tasks: []string{s.Entry.TaskID}, ExclusiveBoard: true}, func(tx *Transaction) error {
					if kind == "still-todo" {
						return tx.Relocate(s.Entry.TaskID, "todo")
					}
					current, err := tx.Snapshot(s.Entry.TaskID)
					if err != nil {
						return err
					}
					text, err := setMetadata(current.Text, FieldOwner, "")
					if err != nil {
						return err
					}
					return tx.Put(s.Entry.TaskID, "spec.md", text)
				}); err != nil {
					t.Fatal(err)
				}
			}
			r := coordinatorRequest(t, root, c)
			if kind == "stale-revision" {
				r.Members[s.Entry.TaskID] = CoordinatorObservation{Revision: s.Revision}
			}
			if kind == "stale-checkpoint" {
				r.ExpectedRevision--
			}
			if _, err := ReconcileCoordinator(context.Background(), root, r, nil); err == nil {
				t.Fatal("unproven first start accepted")
			}
			after, err := ReadCoordinatorCheckpoint(root, c.GroupID)
			if err != nil || !reflect.DeepEqual(after, c) {
				t.Fatal("failed start changed cursor", err)
			}
		})
	}
}

func TestCoordinatorLegacyWaitingCursorAndIncompleteLaunch(t *testing.T) {
	root := tempBoard(t)
	s := coordinatorTodo(t, root, "legacy-waiting")
	c := coordinatorClaim(t, root, s.Entry.TaskID)
	// Reproduce the old schema-1 cursor without the optional waiting flag.
	if err := WithTransaction(root, LockScope{Groups: []string{c.GroupID}}, func(tx *Transaction) error {
		m := c.Members[s.Entry.TaskID]
		m.AwaitingStart = false
		c.Members[s.Entry.TaskID] = m
		c.Revision++
		return putCheckpoint(tx, &c)
	}); err != nil {
		t.Fatal(err)
	}
	c = coordinatorReconcile(t, root, c)
	if !c.Members[s.Entry.TaskID].AwaitingStart {
		t.Fatal("legacy waiting cursor not upgraded")
	}
	// start publishes state before metadata. The intermediate snapshot must
	// retain the waiting fact across a restart instead of binding an empty cycle.
	if _, err := MoveEntry(s.Entry, root, "working"); err != nil {
		t.Fatal(err)
	}
	c = coordinatorReconcile(t, root, c)
	if !c.Members[s.Entry.TaskID].AwaitingStart {
		t.Fatal("incomplete launch bound a cycle")
	}
	s = transactionSnapshot(t, root, s.Entry.TaskID)
	text, err := moveMetadata(s.Text, "todo", "working", MoveOptions{Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteManagedDocument(root, s.Entry, text); err != nil {
		t.Fatal(err)
	}
	c = coordinatorReconcile(t, root, c)
	if !c.Members[s.Entry.TaskID].AwaitingStart {
		t.Fatal("metadata alone confirmed the launcher")
	}
	if err := ConfirmTaskStart(root, s.Entry); err != nil {
		t.Fatal(err)
	}
	c = coordinatorReconcile(t, root, c)
	if c.Members[s.Entry.TaskID].AwaitingStart {
		t.Fatal("first start remains pending")
	}
}
