package board

import (
	"context"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/statemachine/cardlifecycle"
	"github.com/dualface/kander/internal/statemachine/executionledger"
)

// kernelView is the projection of one card into both kernels.
type kernelView struct {
	card   cardlifecycle.State
	ledger executionledger.State
}

func observeKernels(t *testing.T, root, id string) kernelView {
	t.Helper()
	var v kernelView
	err := WithTransaction(root, LockScope{Groups: []string{taskStartGroup, reviewControlGroup}, Tasks: []string{id}, ReadOnly: true}, func(tx *Transaction) error {
		s, err := tx.Snapshot(id)
		if err != nil {
			return err
		}
		if v.card, err = projectCard(tx, s); err != nil {
			return err
		}
		l, err := readExecutionLedger(tx, s)
		if err != nil {
			return err
		}
		k, err := projectLedger(s, l)
		if err != nil {
			return err
		}
		v.ledger = k.state
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// lifecycleSteps are the kernel events of one entrance, in order, built from
// the projection before it runs.
type lifecycleSteps func(before kernelView) ([]cardlifecycle.Event, []executionledger.Event)

// lifecycleRefines runs fn and asserts that the projection after it equals the
// kernel steps from the projection before it. A refused entrance must fail, its
// first kernel event must be refused, and both projections must stay.
func lifecycleRefines(t *testing.T, root, id string, steps lifecycleSteps, accepted bool, fn func() error) {
	t.Helper()
	before := observeKernels(t, root, id)
	card, ledger := steps(before)
	err := fn()
	after := observeKernels(t, root, id)
	if !accepted {
		if err == nil {
			t.Fatal("refused entrance succeeded")
		}
		if after != before {
			t.Fatalf("refused entrance changed the projection: %+v -> %+v", before, after)
		}
		if len(card) > 0 {
			if _, ok := lifecycleStep(before.card, card[0]); ok {
				t.Fatalf("card kernel accepts %T on %+v", card[0], before.card)
			}
		}
		if len(ledger) > 0 {
			params, _ := ledgerParams(before.ledger)
			if _, ok := executionledger.Step(before.ledger, ledger[0], params); ok {
				t.Fatalf("ledger kernel accepts %T on %+v", ledger[0], before.ledger)
			}
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	want := before
	for _, e := range card {
		var ok bool
		if want.card, ok = lifecycleStep(want.card, e); !ok {
			t.Fatalf("card kernel refuses %T on %+v", e, want.card)
		}
	}
	params, _ := ledgerParams(before.ledger)
	for _, e := range ledger {
		var ok bool
		if want.ledger, ok = executionledger.Step(want.ledger, e, params); !ok {
			t.Fatalf("ledger kernel refuses %T on %+v", e, want.ledger)
		}
	}
	if after != want {
		t.Fatalf("projection %+v, kernels %+v", after, want)
	}
}

func cardOnly(e ...cardlifecycle.Event) lifecycleSteps {
	return func(kernelView) ([]cardlifecycle.Event, []executionledger.Event) { return e, nil }
}

// moveSteps is an ordinary move that records no execution.
func moveSteps(to cardlifecycle.Column, col executionledger.Col) lifecycleSteps {
	return func(v kernelView) ([]cardlifecycle.Event, []executionledger.Event) {
		return []cardlifecycle.Event{cardlifecycle.Move{To: to}}, []executionledger.Event{executionledger.Relocate{At: v.ledger.Rev, To: col}}
	}
}

// Ordinary moves, a manual claim, a reclaim, and their refusals refine both
// kernels: a move outside the table (theorem 1), a claim at a stale revision
// and a reclaim without a decision (theorem 5).
func TestLifecycleMovesRefineKernels(t *testing.T) {
	root := tempBoard(t)
	s := coordinatorTodo(t, root, "lifecycle-moves")
	id := s.Entry.TaskID
	move := func(state string, o MoveOptions) func() error {
		return func() error {
			_, err := MoveWithOptions(transactionSnapshot(t, root, id).Entry, root, state, o)
			return err
		}
	}
	lifecycleRefines(t, root, id, cardOnly(cardlifecycle.Move{To: cardlifecycle.ColumnReview}), false, move("review", MoveOptions{}))
	lifecycleRefines(t, root, id, moveSteps(cardlifecycle.ColumnBacklog, executionledger.ColTodo), true, move("backlog", MoveOptions{}))
	lifecycleRefines(t, root, id, moveSteps(cardlifecycle.ColumnTodo, executionledger.ColTodo), true, move("todo", MoveOptions{}))

	stale := transactionSnapshot(t, root, id).Revision - 1
	lifecycleRefines(t, root, id, func(v kernelView) ([]cardlifecycle.Event, []executionledger.Event) {
		return nil, []executionledger.Event{executionledger.Claim{At: stale, Owner: true, Chain: true}}
	}, false, move("working", MoveOptions{Owner: "codex", ExpectedRevision: &stale}))
	lifecycleRefines(t, root, id, func(v kernelView) ([]cardlifecycle.Event, []executionledger.Event) {
		return []cardlifecycle.Event{cardlifecycle.Move{To: cardlifecycle.ColumnWorking}},
			[]executionledger.Event{executionledger.Claim{At: v.ledger.Rev, Owner: true, Chain: true}}
	}, true, move("working", MoveOptions{Owner: "codex"}))
	lifecycleRefines(t, root, id, moveSteps(cardlifecycle.ColumnReview, executionledger.ColReview), true, move("review", MoveOptions{}))
	lifecycleRefines(t, root, id, func(v kernelView) ([]cardlifecycle.Event, []executionledger.Event) {
		return nil, []executionledger.Event{executionledger.Reclaim{At: v.ledger.Rev, Proof: true}}
	}, false, move("working", MoveOptions{Owner: "claude"}))
	lifecycleRefines(t, root, id, func(v kernelView) ([]cardlifecycle.Event, []executionledger.Event) {
		return []cardlifecycle.Event{cardlifecycle.Move{To: cardlifecycle.ColumnWorking}},
			[]executionledger.Event{executionledger.Reclaim{At: v.ledger.Rev, Decision: true, Proof: true}}
	}, true, move("working", MoveOptions{Owner: "claude", Decision: "user:reclaim"}))
	// A move into trash keeps the exclusion record; the archive cleanup (M14)
	// removes an archived card's records after its move commits.
	lifecycleRefines(t, root, id, func(v kernelView) ([]cardlifecycle.Event, []executionledger.Event) {
		return []cardlifecycle.Event{cardlifecycle.Move{To: cardlifecycle.ColumnTrash, Member: true}},
			[]executionledger.Event{executionledger.Relocate{At: v.ledger.Rev, To: executionledger.ColClosed}}
	}, true, move("trash", MoveOptions{Result: "trashed", Reason: "superseded", Decision: "user:trash"}))
	if v := observeKernels(t, root, id); !v.card.Excluded {
		t.Fatal("trashing a group member records no exclusion")
	}
}

// Contract updates refine the card kernel: a frozen-field change without a
// decision is refused (theorem 6), one with a decision adds a record, and an
// acceptance tick passes in working.
func TestContractUpdatesRefineKernels(t *testing.T) {
	root := tempBoard(t)
	s := coordinatorTodo(t, root, "lifecycle-contract")
	id := s.Entry.TaskID
	update := func(text, decision string) func() error {
		return func() error {
			s := transactionSnapshot(t, root, id)
			return UpdateDocument(root, id, UpdateOptions{Document: "spec.md", Text: text, ExpectedRevision: s.Revision, ContractDecision: decision})
		}
	}
	write := func(change cardlifecycle.Change, decision bool) lifecycleSteps {
		return func(v kernelView) ([]cardlifecycle.Event, []executionledger.Event) {
			return []cardlifecycle.Event{cardlifecycle.Update{Change: change, Decision: decision}}, []executionledger.Event{executionledger.Write{At: v.ledger.Rev}}
		}
	}
	goal := func() string {
		text := transactionSnapshot(t, root, id).Text
		body, _ := SectionBody(text, SectionGoal)
		return strings.Replace(text, body, body+"\nA changed goal.", 1)
	}
	lifecycleRefines(t, root, id, cardOnly(cardlifecycle.Update{Change: cardlifecycle.ChangeOther}), false, update(goal(), ""))
	lifecycleRefines(t, root, id, write(cardlifecycle.ChangeOther, true), true, update(goal(), "user:goal"))
	if v := observeKernels(t, root, id); v.card.Decisions != 1 {
		t.Fatalf("decision records: %d", v.card.Decisions)
	}
	lifecycleRefines(t, root, id, cardOnly(cardlifecycle.Update{Change: cardlifecycle.ChangeTicks}), false, update(strings.Replace(transactionSnapshot(t, root, id).Text, "- [ ] ", "- [x] ", 1), ""))
	if _, err := MoveWithOptions(transactionSnapshot(t, root, id).Entry, root, "working", MoveOptions{Owner: "codex"}); err != nil {
		t.Fatal(err)
	}
	lifecycleRefines(t, root, id, write(cardlifecycle.ChangeTicks, false), true, update(strings.Replace(transactionSnapshot(t, root, id).Text, "- [ ] ", "- [x] ", 1), ""))
	lifecycleRefines(t, root, id, write(cardlifecycle.ChangeNone, false), true, update(strings.Replace(transactionSnapshot(t, root, id).Text, "## IMPLEMENTATION\n\nfixture", "## IMPLEMENTATION\n\nfixture notes", 1), ""))
}

// A launcher start, its window, a session confirmation, a session repair, a
// process identity, a takeover, its rollback, and a stale fence refine the
// ledger kernel (theorem 5 for fenced writes).
func TestStartAndTakeoverRefineKernels(t *testing.T) {
	root := tempBoard(t)
	s := coordinatorTodo(t, root, "lifecycle-start")
	id := s.Entry.TaskID
	var moved Entry
	lifecycleRefines(t, root, id, func(v kernelView) ([]cardlifecycle.Event, []executionledger.Event) {
		return []cardlifecycle.Event{cardlifecycle.Move{To: cardlifecycle.ColumnWorking}}, []executionledger.Event{executionledger.Start{At: v.ledger.Rev, Chain: true}}
	}, true, func() (err error) {
		moved, err = BeginTaskStart(s.Entry, root, TaskStartInput{Owner: "codex", Session: "codex"})
		return err
	})
	field := func(text bool) lifecycleSteps {
		return func(v kernelView) ([]cardlifecycle.Event, []executionledger.Event) {
			return nil, []executionledger.Event{executionledger.Field{Fence: v.ledger.Record, Text: text}}
		}
	}
	lifecycleRefines(t, root, id, field(true), true, func() error { _, err := RecordExecutionWindow(root, moved, "tmux:s:w:p", ""); return err })
	lifecycleRefines(t, root, id, func(v kernelView) ([]cardlifecycle.Event, []executionledger.Event) {
		return nil, []executionledger.Event{executionledger.Confirm{Fence: v.ledger.Record, Match: true, Session: true}}
	}, true, func() error { return ConfirmTaskStartSession(root, moved, "codex", "codex found") })
	b := bound(t, root, id)
	lifecycleRefines(t, root, id, field(true), true, func() error {
		_, err := RecordExecutionSession(context.Background(), root, b.Entry, "codex found", "codex repaired")
		return err
	})
	lifecycleRefines(t, root, id, field(false), true, func() error {
		_, err := RecordExecutionProcess(root, b.Entry, ExecutionProcess{Launcher: "foreground", AgentPID: 7})
		return err
	})
	original := transactionSnapshot(t, root, id)
	taker := bound(t, root, id)
	lifecycleRefines(t, root, id, func(v kernelView) ([]cardlifecycle.Event, []executionledger.Event) {
		return nil, []executionledger.Event{executionledger.Takeover{Fence: v.ledger.Record, Decision: true, Changes: true}}
	}, true, func() error {
		_, err := BeginTakeover(root, taker.Entry, TakeoverInput{Owner: "claude", Session: "claude next", Window: "foreground", Decision: "user"})
		return err
	})
	// The launcher of the replaced record writes with a stale fence.
	lifecycleRefines(t, root, id, func(v kernelView) ([]cardlifecycle.Event, []executionledger.Event) {
		return nil, []executionledger.Event{executionledger.Field{Fence: v.ledger.Record - 1, Text: true}}
	}, false, func() error { _, err := RecordExecutionWindow(root, moved, "tmux:s:w:q", ""); return err })
	lifecycleRefines(t, root, id, func(v kernelView) ([]cardlifecycle.Event, []executionledger.Event) {
		return nil, []executionledger.Event{executionledger.Rollback{Fence: v.ledger.Record, At: v.ledger.Rev, Launcher: true,
			RestoredKind: executionledger.KindStart, RestoredStatus: executionledger.StatusRunning}}
	}, true, func() error { return RollbackDocument(root, taker.Entry, original.Text, "") })
}

// A start rollback returns the card to todo (theorem 2); a second rollback of
// the same cursor is refused.
func TestStartRollbackRefinesKernels(t *testing.T) {
	root := tempBoard(t)
	s := coordinatorTodo(t, root, "lifecycle-rollback")
	id := s.Entry.TaskID
	moved, err := BeginTaskStart(s.Entry, root, TaskStartInput{Owner: "codex", Session: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	rollback := func(v kernelView) ([]cardlifecycle.Event, []executionledger.Event) {
		return []cardlifecycle.Event{cardlifecycle.Rollback{ToTodo: true, Owns: true}},
			[]executionledger.Event{executionledger.Rollback{Fence: v.ledger.Record, At: v.ledger.Rev, Launcher: true, ToTodo: true}}
	}
	lifecycleRefines(t, root, id, rollback, true, func() error { return RollbackDocument(root, moved, s.Text, "todo") })
	lifecycleRefines(t, root, id, rollback, false, func() error { return RollbackDocument(root, moved, s.Text, "todo") })
}

// An unresolved start and its adoption refine the ledger kernel; an unresolve
// from a cursor older than the record is refused (theorem 5).
func TestUnresolveAndAdoptRefineKernels(t *testing.T) {
	root := tempBoard(t)
	s := coordinatorTodo(t, root, "lifecycle-adopt")
	id := s.Entry.TaskID
	moved, err := BeginTaskStart(s.Entry, root, TaskStartInput{Owner: "codex", Session: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	lifecycleRefines(t, root, id, func(v kernelView) ([]cardlifecycle.Event, []executionledger.Event) {
		return nil, []executionledger.Event{executionledger.Unresolve{Fence: v.ledger.Record, At: v.ledger.Rev, Launcher: true, Window: true}}
	}, true, func() error { _, err := UnresolveExecution(root, moved, "tmux:s:w:p", "late step failed"); return err })
	observed := bound(t, root, id)
	lifecycleRefines(t, root, id, func(v kernelView) ([]cardlifecycle.Event, []executionledger.Event) {
		return nil, []executionledger.Event{executionledger.Adopt{Fence: v.ledger.Record, Decision: true, Identity: true}}
	}, true, func() error { _, err := AdoptExecution(root, observed, "user:adopt"); return err })
	lifecycleRefines(t, root, id, func(v kernelView) ([]cardlifecycle.Event, []executionledger.Event) {
		return nil, []executionledger.Event{executionledger.Adopt{Fence: v.ledger.Record, Decision: true, Identity: true}}
	}, false, func() error { _, err := AdoptExecution(root, observed, "user:adopt"); return err })
}

// A window restore of a caller that owns no pending record refines the ledger
// kernel.
func TestWindowRestoreRefinesKernels(t *testing.T) {
	root := tempBoard(t)
	s := coordinatorTodo(t, root, "lifecycle-restore")
	id := s.Entry.TaskID
	if _, err := MoveWithOptions(s.Entry, root, "working", MoveOptions{Owner: "codex"}); err != nil {
		t.Fatal(err)
	}
	current := bound(t, root, id)
	text, err := setExecutionField(current.Text, FieldWindow, FieldSession, "tmux:s:w:p")
	if err != nil {
		t.Fatal(err)
	}
	lifecycleRefines(t, root, id, func(v kernelView) ([]cardlifecycle.Event, []executionledger.Event) {
		return nil, []executionledger.Event{executionledger.Restore{At: v.ledger.Rev, Same: true, Changed: true}}
	}, true, func() error { return RollbackDocument(root, current.Entry, text, "") })
}

// A coordinator recovery reset refines both kernels (theorem 2).
func TestRecoverResetRefinesKernels(t *testing.T) {
	root := tempBoard(t)
	ids, c := blockedGroup(t, root)
	lifecycleRefines(t, root, ids[0], func(v kernelView) ([]cardlifecycle.Event, []executionledger.Event) {
		return []cardlifecycle.Event{cardlifecycle.Recover{Cas: true, Blocked: true, Unbound: true, Settled: true}},
			[]executionledger.Event{executionledger.Recover{At: v.ledger.Rev, Unbound: true, Cas: true}}
	}, true, func() error {
		_, err := RecoverCoordinator(context.Background(), root, recoverRequest(c, ids[0]), transactionRevision(t, root, ids[0]))
		return err
	})
}

// A dispatch accepted by a card already in working stays in its column and
// refines the card kernel (theorem 2); the same move without an authorization
// is outside the transition table and refused.
func TestDispatchSameColumnMoveRefinesKernels(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "lifecycle-dispatch")
	id := s.Entry.TaskID
	if _, err := MoveEntry(s.Entry, root, "working"); err != nil {
		t.Fatal(err)
	}
	lifecycleRefines(t, root, id, cardOnly(cardlifecycle.Move{To: cardlifecycle.ColumnWorking}), false, func() error {
		_, err := MoveEntry(transactionSnapshot(t, root, id).Entry, root, "working")
		return err
	})
	d := prepareTestDispatch(t, root, dispatchInput(transactionSnapshot(t, root, id), "sync-one"))
	lifecycleRefines(t, root, id, func(v kernelView) ([]cardlifecycle.Event, []executionledger.Event) {
		return []cardlifecycle.Event{cardlifecycle.DispatchMove{Authorized: true}}, []executionledger.Event{executionledger.Write{At: v.ledger.Rev}}
	}, true, func() error { _, err := dispatchMove(t, root, d, "working"); return err })
}
