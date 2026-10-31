package board

import (
	"errors"
	"strings"
	"testing"
)

func TestReconcileReadsEveryRoundItMissed(t *testing.T) {
	root := tempBoard(t)
	s := coordinatorCard(t, root, "missed-rounds")
	id := s.Entry.TaskID
	c := coordinatorClaim(t, root, id)
	x := completedRound(t, root, id, "round-x")
	c = coordinatorReconcile(t, root, c)
	if c.Members[id].Dispatch == nil || c.Members[id].Dispatch.ID != x.Input.ID {
		t.Fatalf("mirror %+v", c.Members[id])
	}
	y := prepareTestDispatch(t, root, dispatchInput(transactionSnapshot(t, root, id), "round-y"))
	if err := EndDispatch(root, id, y.Input.ID, y.Revision, DispatchCancelled, "drop"); err != nil {
		t.Fatal(err)
	}
	next := coordinatorReconcile(t, root, c)
	m := next.Members[id]
	if m.Dispatch != nil || m.DispatchSeq != 2 || len(m.ReleasedDispatches) != 1 {
		t.Fatalf("mirror %+v", m)
	}
}

func TestReconcileAcceptsAChainOfUnobservedRecords(t *testing.T) {
	root := tempBoard(t)
	moved := startedCard(t, root, "unobserved-chain")
	id := moved.TaskID
	c := coordinatorClaim(t, root, id)
	// The launcher, a takeover and an adoption all finish between two reconciles.
	if _, err := RecordExecutionWindow(root, moved, "tmux:a:b:c", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := UnresolveExecution(root, moved, "", "late failure"); err != nil {
		t.Fatal(err)
	}
	s := bound(t, root, id)
	if _, err := BeginTakeover(root, s.Entry, TakeoverInput{Owner: "claude", Session: "claude next", Decision: "user"}); err != nil {
		t.Fatal(err)
	}
	if _, err := UnresolveExecution(root, s.Entry, "", "second failure"); err != nil {
		t.Fatal(err)
	}
	if _, err := AdoptExecution(root, bound(t, root, id), "user"); err != nil {
		t.Fatal(err)
	}
	next := coordinatorReconcile(t, root, c)
	m := next.Members[id]
	if m.AwaitingStart || m.Cycle != planCycle(transactionSnapshot(t, root, id)) || m.TransitionSeq != ledgerOf(t, root, id).end() {
		t.Fatalf("mirror %+v", m)
	}
}

// A member with an unfinished start waits. The attempt refuses a dispatch, so
// no round can bind it, and reconcile stays clean.
func TestUnresolvedStartWaitsWithoutADispatch(t *testing.T) {
	for _, status := range []string{ExecutionPending, ExecutionUnresolved} {
		t.Run(status, func(t *testing.T) {
			root := tempBoard(t)
			moved := startedCard(t, root, "waiting-start")
			id := moved.TaskID
			if status == ExecutionUnresolved {
				if _, err := UnresolveExecution(root, moved, "", "late failure"); err != nil {
					t.Fatal(err)
				}
			}
			c := coordinatorClaim(t, root, id)
			s := transactionSnapshot(t, root, id)
			if _, err := MoveEntry(s.Entry, root, "review"); err != nil {
				t.Fatal(err)
			}
			var e *Error
			if _, err := PrepareDispatch(root, dispatchInput(transactionSnapshot(t, root, id), "waiting-round")); !errors.As(err, &e) || e.Code != "board.dispatch_start_unfinished" {
				t.Fatalf("err=%v", err)
			}
			next := coordinatorReconcile(t, root, c)
			m := next.Members[id]
			if !m.AwaitingStart || m.Dispatch != nil {
				t.Fatalf("mirror %+v", m)
			}
		})
	}
}

func TestReconcileRejectsAChangeNoLinkRecords(t *testing.T) {
	for name, field := range map[string]string{"owner": FieldOwner, "session": FieldSession, "window": FieldWindow, "cycle": FieldStartedAt} {
		t.Run(name, func(t *testing.T) {
			root := tempBoard(t)
			moved := startedCard(t, root, "unrecorded-change")
			id := moved.TaskID
			if err := ConfirmTaskStart(root, moved); err != nil {
				t.Fatal(err)
			}
			c := coordinatorClaim(t, root, id)
			c = coordinatorReconcile(t, root, c)
			s := transactionSnapshot(t, root, id)
			text, err := setMetadata(s.Text, field, "forged")
			if err != nil {
				t.Fatal(err)
			}
			if err = fixtureWrite(root, s.Entry, text); err != nil {
				t.Fatal(err)
			}
			var blocked *CoordinatorBlockedError
			if _, err = ReconcileCoordinator(t.Context(), root, coordinatorRequest(t, root, c), nil); !errors.As(err, &blocked) || !strings.Contains(err.Error(), "without lifecycle handoff") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestTerminalMemberNeedsNoStartFacts(t *testing.T) {
	for _, target := range []string{"archived", "trash"} {
		t.Run(target, func(t *testing.T) {
			root := tempBoard(t)
			a := coordinatorTodo(t, root, "terminal-a")
			b := coordinatorTodo(t, root, "terminal-b")
			c := coordinatorClaim(t, root, a.Entry.TaskID, b.Entry.TaskID)
			moved, err := MoveWithOptions(a.Entry, root, "working", MoveOptions{Owner: "codex"})
			if err != nil {
				t.Fatal(err)
			}
			result := map[string]string{"archived": "cancelled", "trash": "trashed"}[target]
			if _, err = MoveWithOptions(moved, root, target, MoveOptions{Result: result, Reason: "stop", Decision: "user"}); err != nil {
				t.Fatal(err)
			}
			next := coordinatorReconcile(t, root, c)
			if m := next.Members[a.Entry.TaskID]; m.State != target || m.AwaitingStart {
				t.Fatalf("mirror %+v", m)
			}
		})
	}
}

func TestUpdateCannotMoveACardAcrossACheckpoint(t *testing.T) {
	root := tempBoard(t)
	member := coordinatorTodo(t, root, "frozen-member")
	outsider := transactionCard(t, root, "late-member", false)
	coordinatorClaim(t, root, member.Entry.TaskID)
	refuse := func(s Snapshot, group string) {
		t.Helper()
		text, err := setMetadata(s.Text, FieldTaskGroup, group)
		if err != nil {
			t.Fatal(err)
		}
		var e *Error
		err = UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: "spec.md", Text: text, ExpectedRevision: s.Revision, ContractDecision: "user"})
		if !errors.As(err, &e) || e.Code != "board.update_group_checkpointed" {
			t.Fatalf("err=%v", err)
		}
	}
	refuse(outsider, coordinatorGroup)
	refuse(member, "20261031-other-group")
}
