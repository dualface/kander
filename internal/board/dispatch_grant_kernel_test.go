package board

import (
	"errors"
	"strings"
	"testing"
	"time"

	dispatchkernel "github.com/dualface/kander/v2/internal/statemachine/dispatch"
)

// The wrap-up grant and the settlement of a completed binding are decided by the
// generated kernel (formal/KanderFormal/Machines/Dispatch.lean). Each test runs
// an entrance and asserts, through dispatchRefines, that the projection after it
// is the kernel step from the projection before it, or that the entrance and the
// kernel both refuse and the projection stays.

// authorizeEvent is the grant of the card's round at the pair a, on a card in the
// review column, with every fact holding but the ones adjust changes.
func authorizeEvent(a ExecutionAuthorization, adjust func(*dispatchkernel.AuthorizeWrapUp)) func(*dispatchView) dispatchkernel.Event {
	return func(v *dispatchView) dispatchkernel.Event {
		round, epoch := v.pair(a)
		e := dispatchkernel.AuthorizeWrapUp{AuthRound: round, AuthEpoch: epoch, Cas: true, Room: true, Col: dispatchkernel.ColReview,
			Named: true, Cycle: true, Outcome: dispatchkernel.ExitStopped, Fresh: true, Evidence: true}
		if adjust != nil {
			adjust(&e)
		}
		return e
	}
}

func authorizeFn(root string, d Dispatch, author string, delta uint64, exit WrapUpExitEvidence) func() error {
	return func() error {
		_, err := AuthorizeDispatchWrapUp(root, d.Input.TaskID, d.Input.ID, d.Revision+delta, author, "原执行者退出，仅代收尾", exit)
		return err
	}
}

// A grant is accepted for a wrap-up round whose executor stopped, and refused
// when one fact fails: the dispatch revision, the author, the exit outcome or its
// decision or freshness (theorem 1).
func TestDispatchAuthorizeWrapUpRefinesKernel(t *testing.T) {
	for _, tc := range []struct {
		name   string
		exit   func(*WrapUpExitEvidence)
		author string
		delta  uint64
		adjust func(*dispatchkernel.AuthorizeWrapUp)
		ok     bool
	}{
		{name: "stopped", author: "coordinator", ok: true},
		{name: "reclaimed with a decision", author: "coordinator", ok: true,
			exit:   func(e *WrapUpExitEvidence) { e.Outcome, e.Decision = "reclaimed", "user" },
			adjust: func(e *dispatchkernel.AuthorizeWrapUp) { e.Outcome, e.Decided = dispatchkernel.ExitReclaimed, true }},
		{name: "reclaimed without a decision", author: "coordinator",
			exit:   func(e *WrapUpExitEvidence) { e.Outcome = "reclaimed" },
			adjust: func(e *dispatchkernel.AuthorizeWrapUp) { e.Outcome = dispatchkernel.ExitReclaimed }},
		{name: "still alive", author: "coordinator",
			exit:   func(e *WrapUpExitEvidence) { e.Outcome = "alive" },
			adjust: func(e *dispatchkernel.AuthorizeWrapUp) { e.Outcome = dispatchkernel.ExitOther }},
		{name: "stale observation", author: "coordinator",
			exit:   func(e *WrapUpExitEvidence) { e.ObservedAt = time.Now().Add(-time.Minute) },
			adjust: func(e *dispatchkernel.AuthorizeWrapUp) { e.Fresh = false }},
		{name: "another card revision", author: "coordinator",
			exit:   func(e *WrapUpExitEvidence) { e.CardRevision-- },
			adjust: func(e *dispatchkernel.AuthorizeWrapUp) { e.Fresh = false }},
		{name: "no author", author: " ", adjust: func(e *dispatchkernel.AuthorizeWrapUp) { e.Named = false }},
		{name: "stale dispatch revision", author: "coordinator", delta: 1, adjust: func(e *dispatchkernel.AuthorizeWrapUp) { e.Cas = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, d, exit := wrapUpGrantFixture(t, true)
			if tc.exit != nil {
				tc.exit(&exit)
			}
			id, rid := d.Input.TaskID, d.Input.ID
			dispatchRefines(t, root, id, rid, rid, 0, authorizeEvent(d.Authorization, tc.adjust), tc.ok, authorizeFn(root, d, tc.author, tc.delta, exit))
			got, err := ReadDispatch(root, id, rid)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.ok {
				if got.WrapUpAuthority != nil || got.Authorization != d.Authorization {
					t.Fatalf("refused grant changed the record %+v", got)
				}
				return
			}
			// The grant raises the epoch, returns the round to prepared, and leaves no receipt.
			if got.WrapUpAuthority == nil || got.Authorization.Epoch != d.Authorization.Epoch+1 || got.State != DispatchPrepared ||
				got.Accepted != nil || got.Completed != nil || got.Attempts != 0 {
				t.Fatalf("granted %+v", got)
			}
		})
	}
}

// A grant is refused on a round that already ended, holds a grant, or sits at the
// last epoch, and the kernel refuses on a card outside working and review.
func TestDispatchAuthorizeWrapUpRefusalsRefineKernel(t *testing.T) {
	root, d, exit := wrapUpGrantFixture(t, true)
	id, rid := d.Input.TaskID, d.Input.ID
	// The round already ended: a legacy failed round keeps its binding.
	ended := rewriteDispatch(t, root, d, func(d *Dispatch) { d.State, d.Reason = DispatchFailed, "legacy" })
	dispatchRefines(t, root, id, rid, rid, 0, authorizeEvent(ended.Authorization, nil), false, authorizeFn(root, ended, "coordinator", 0, exit))

	// The epoch cannot rise any more.
	root, d, exit = wrapUpGrantFixture(t, true)
	last := rewriteDispatch(t, root, d, func(d *Dispatch) { d.Authorization.Epoch = ^uint64(0) })
	exit.CardRevision = transactionSnapshot(t, root, id).Revision
	dispatchRefines(t, root, id, rid, rid, 0, authorizeEvent(last.Authorization, func(e *dispatchkernel.AuthorizeWrapUp) { e.Room = false }), false,
		authorizeFn(root, last, "coordinator", 0, exit))

	// A round of another kind takes no grant.
	other := dispatchCard(t, root, "kernel-grant-kind")
	sync := prepareTestDispatch(t, root, dispatchInput(other, "sync-round"))
	oexit := stoppedDispatchFixture(transactionSnapshot(t, root, other.Entry.TaskID))
	dispatchRefines(t, root, other.Entry.TaskID, "sync-round", "sync-round", 0, authorizeEvent(sync.Authorization, nil), false, authorizeFn(root, sync, "coordinator", 0, oexit))

	// The kernel refuses the columns a bound round never has.
	v := emptyDispatchView()
	st := dispatchkernel.State{Round: 1, Epoch: 1, Bound: true, Phase: dispatchkernel.PhasePrepared, Kind: dispatchkernel.KindWrapUp}
	if _, ok := dispatchkernel.Step(st, authorizeEvent(ExecutionAuthorization{}, nil)(v), v.params); ok {
		t.Fatal("the kernel granted a card with an unbound pair")
	}
	for _, col := range []dispatchkernel.Col{dispatchkernel.ColWorking, dispatchkernel.ColReview, dispatchkernel.ColOther} {
		_, ok := dispatchkernel.Step(st, dispatchkernel.AuthorizeWrapUp{AuthRound: 1, AuthEpoch: 1, Cas: true, Room: true, Col: col, Named: true,
			Cycle: true, Outcome: dispatchkernel.ExitStopped, Fresh: true, Evidence: true}, v.params)
		if ok != (col != dispatchkernel.ColOther) {
			t.Fatalf("column %d: kernel accepted %t", col, ok)
		}
	}
}

func fullEvent(*dispatchView) dispatchkernel.Event { return dispatchkernel.Full{} }

// requireFullFn runs the production check of a full-execution write on the card.
func requireFullFn(root, id string) func() error {
	return func() error {
		return WithTransaction(root, LockScope{Tasks: []string{id}, ReadOnly: true}, func(tx *Transaction) error {
			s, err := tx.Snapshot(id)
			if err != nil {
				return err
			}
			return tx.requireFullExecution(s)
		})
	}
}

// A full-execution write is accepted on a card without a binding and on a card
// bound to a round that holds no grant; the projection stays (theorem 2).
func TestDispatchFullExecutionAcceptsRefineKernel(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "kernel-full-accepts")
	id := s.Entry.TaskID
	dispatchRefines(t, root, id, "", "", 0, fullEvent, true, requireFullFn(root, id))
	prepareTestDispatch(t, root, dispatchInput(s, "no-grant"))
	dispatchRefines(t, root, id, "no-grant", "no-grant", 0, fullEvent, true, requireFullFn(root, id))
}

func wrapUpWriteEvent(w dispatchkernel.WrapUpWrite) func(*dispatchView) dispatchkernel.Event {
	return func(*dispatchView) dispatchkernel.Event { return w }
}

// While the grant holds, a launch, a delivery, an upgrade, a second grant, a
// full-execution write, and a write with a contract decision are refused; only a
// record write is accepted, and the projection never changes (theorem 2).
func TestDispatchGrantRefusesWritesAndRefinesKernel(t *testing.T) {
	root, d, exit := wrapUpGrantFixture(t, true)
	id, rid := d.Input.TaskID, d.Input.ID
	next, err := AuthorizeDispatchWrapUp(root, id, rid, d.Revision, "coordinator", "原执行者退出，仅代收尾", exit)
	if err != nil {
		t.Fatal(err)
	}
	if got := projectDispatchAt(t, root, id, rid, nil).state; !got.Grant || got.AcceptedReceipt || got.CompletedReceipt || got.Phase != dispatchkernel.PhasePrepared {
		t.Fatalf("granted projection %+v", got)
	}
	dispatchRefines(t, root, id, rid, rid, 0, attemptEvent(next.Authorization, true, true), false, func() error {
		_, err := BeginDispatchAttempt(root, id, rid, next.Revision)
		return err
	})
	dispatchRefines(t, root, id, rid, rid, 0, reauthorizeEvent(next.Authorization, true, false, true), false, func() error {
		_, err := ReauthorizeDispatch(root, id, rid, next.Revision)
		return err
	})
	again := stoppedDispatchFixture(transactionSnapshot(t, root, id))
	dispatchRefines(t, root, id, rid, rid, 0, authorizeEvent(next.Authorization, nil), false, authorizeFn(root, next, "coordinator", 0, again))

	// The executor starts, then writes records only.
	dispatchRefines(t, root, id, rid, rid, 0, acceptEvent(next.Authorization, true, true), true, func() error {
		_, err := dispatchMove(t, root, next, "working")
		return err
	})
	if got := projectDispatchAt(t, root, id, rid, nil).state; !got.AcceptedReceipt || !got.Grant {
		t.Fatalf("accepted projection %+v", got)
	}
	requireFull := requireFullFn(root, id)
	dispatchRefines(t, root, id, rid, rid, 0, fullEvent, false, requireFull)
	dispatchRefines(t, root, id, rid, rid, 0, fullEvent, false, func() error {
		return fixtureWrite(root, transactionSnapshot(t, root, id).Entry, transactionSnapshot(t, root, id).Text+"\nnew runtime identity\n")
	})

	update := func(o UpdateOptions) func() error {
		return func() error {
			cur := transactionSnapshot(t, root, id)
			o.ExpectedRevision, o.Authorization = cur.Revision, next.Authorization
			if o.Document == "spec.md" && o.Text == "" {
				o.Text = strings.TrimRight(cur.Text, "\n") + "\n\n## WRAP_UP_RECORDS\n\n作者：coordinator。已确认集成；只清理任务资源。\n"
			}
			return UpdateDocument(root, id, o)
		}
	}
	w := func(contract, spec, appends, path, same bool) dispatchkernel.WrapUpWrite {
		return dispatchkernel.WrapUpWrite{Contract: contract, Spec: spec, Appends: appends, Path: path, Same: same}
	}
	record := "wrap-up/" + rid + "-" + "2.md"
	dispatchRefines(t, root, id, rid, rid, 0, wrapUpWriteEvent(w(true, true, true, true, true)), false, update(UpdateOptions{Document: "spec.md", ContractDecision: "user"}))
	dispatchRefines(t, root, id, rid, rid, 0, wrapUpWriteEvent(w(false, true, false, true, true)), false, update(UpdateOptions{Document: "spec.md", Text: "rewritten"}))
	dispatchRefines(t, root, id, rid, rid, 0, wrapUpWriteEvent(w(false, false, false, false, true)), false, update(UpdateOptions{Document: "notes.md", Text: "code edits"}))
	dispatchRefines(t, root, id, rid, rid, 0, wrapUpWriteEvent(w(false, true, true, true, true)), true, update(UpdateOptions{Document: "spec.md"}))
	dispatchRefines(t, root, id, rid, rid, 0, wrapUpWriteEvent(w(false, false, false, true, true)), true, update(UpdateOptions{Document: record, Text: "first"}))
	dispatchRefines(t, root, id, rid, rid, 0, wrapUpWriteEvent(w(false, false, false, true, true)), true, update(UpdateOptions{Document: record, Text: "first"}))
	dispatchRefines(t, root, id, rid, rid, 0, wrapUpWriteEvent(w(false, false, false, true, false)), false, update(UpdateOptions{Document: record, Text: "second"}))
}

// The previous epoch is fenced by the grant, and the round completes only into
// done (theorem 3).
func TestDispatchGrantFencesPreviousEpochRefinesKernel(t *testing.T) {
	root, d, exit := wrapUpGrantFixture(t, true)
	id, rid := d.Input.TaskID, d.Input.ID
	next, err := AuthorizeDispatchWrapUp(root, id, rid, d.Revision, "coordinator", "原执行者退出，仅代收尾", exit)
	if err != nil {
		t.Fatal(err)
	}
	dispatchRefines(t, root, id, rid, rid, 0, acceptEvent(d.Authorization, true, true), false, func() error {
		_, err := dispatchMove(t, root, d, "working")
		return err
	})
	write := func(a ExecutionAuthorization) func() error {
		return func() error {
			cur := transactionSnapshot(t, root, id)
			return UpdateDocument(root, id, UpdateOptions{Document: "late.md", Text: "late", ExpectedRevision: cur.Revision, Authorization: a})
		}
	}
	dispatchRefines(t, root, id, rid, rid, 0, execEvent(d.Authorization, true, false), false, write(d.Authorization))
	dispatchRefines(t, root, id, rid, rid, 0, acceptEvent(next.Authorization, true, true), true, func() error {
		_, err := dispatchMove(t, root, next, "working")
		return err
	})
	dispatchRefines(t, root, id, rid, rid, 0, completeEvent(d.Authorization, true, false, dispatchkernel.ColWorking), false, func() error {
		_, err := dispatchMove(t, root, d, "done")
		return err
	})
	dispatchRefines(t, root, id, rid, rid, 0, completeEvent(next.Authorization, false, false, dispatchkernel.ColWorking), false, func() error {
		_, err := dispatchMove(t, root, next, "review")
		return err
	})
	dispatchRefines(t, root, id, rid, rid, 0, completeEvent(next.Authorization, true, false, dispatchkernel.ColWorking), true, func() error {
		_, err := dispatchMove(t, root, next, "done")
		return err
	})
	if got := projectDispatchAt(t, root, id, rid, nil).state; got.Phase != dispatchkernel.PhaseCompleted || !got.CompletedReceipt || !got.Grant {
		t.Fatalf("completed projection %+v", got)
	}
}

func settleEvent(a ExecutionAuthorization, cas bool, col dispatchkernel.Col) func(*dispatchView) dispatchkernel.Event {
	return func(v *dispatchView) dispatchkernel.Event {
		round, epoch := v.pair(a)
		return dispatchkernel.Settle{AuthRound: round, AuthEpoch: epoch, Cas: cas, Col: col, Cycle: true}
	}
}

// SettleDispatch settles a completed binding once, in working or review, at its
// revision; a round that did not complete, a stale revision, and a settled round
// are refused; a settled round refuses the requests of its pair and ending
// (theorems 4, 5, and 6).
func TestDispatchSettleRefinesKernel(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "kernel-settle")
	id := s.Entry.TaskID
	settle := func(d Dispatch, delta uint64) func() error {
		return func() error { return SettleDispatch(root, id, d.Input.ID, d.Revision+delta) }
	}
	open := prepareTestDispatch(t, root, dispatchInput(s, "open-round"))
	dispatchRefines(t, root, id, "open-round", "open-round", 0, settleEvent(open.Authorization, true, dispatchkernel.ColReview), false, settle(open, 0))
	if err := EndDispatch(root, id, "open-round", open.Revision, DispatchCancelled, "stop"); err != nil {
		t.Fatal(err)
	}
	// A released round is not settled.
	released, err := ReadDispatch(root, id, "open-round")
	if err != nil {
		t.Fatal(err)
	}
	dispatchRefines(t, root, id, "open-round", "open-round", 0, settleEvent(released.Authorization, true, dispatchkernel.ColReview), false, settle(released, 0))

	d := completedRound(t, root, id, "settled-round")
	dispatchRefines(t, root, id, "settled-round", "settled-round", 0, settleEvent(d.Authorization, false, dispatchkernel.ColReview), false, settle(d, 1))
	dispatchRefines(t, root, id, "settled-round", "settled-round", 0, settleEvent(d.Authorization, true, dispatchkernel.ColReview), true, settle(d, 0))
	got, err := ReadDispatch(root, id, "settled-round")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != DispatchCompleted || got.Settlement == nil || got.Release != nil {
		t.Fatalf("settled %+v", got)
	}
	// The settled round settles no more, and its pair is fenced.
	dispatchRefines(t, root, id, "settled-round", "settled-round", 0, settleEvent(got.Authorization, true, dispatchkernel.ColReview), false, settle(got, 0))
	dispatchRefines(t, root, id, "settled-round", "settled-round", 0, acceptEvent(got.Authorization, true, true), false, func() error {
		_, err := dispatchMove(t, root, got, "working")
		return err
	})
	dispatchRefines(t, root, id, "settled-round", "settled-round", 0, completeEvent(got.Authorization, false, true, dispatchkernel.ColWorking), false, func() error {
		_, err := dispatchMove(t, root, got, "review")
		return err
	})
	dispatchRefines(t, root, id, "settled-round", "settled-round", 0, execEvent(got.Authorization, true, false), false, func() error {
		cur := transactionSnapshot(t, root, id)
		return UpdateDocument(root, id, UpdateOptions{Document: "late.md", Text: "late", ExpectedRevision: cur.Revision, Authorization: got.Authorization})
	})
	dispatchRefines(t, root, id, "settled-round", "settled-round", 0, func(v *dispatchView) dispatchkernel.Event {
		round, epoch := v.pair(got.Authorization)
		return dispatchkernel.End{AuthRound: round, AuthEpoch: epoch, Cas: true, Room: true, Cycle: true, Decided: true, ToFailed: true}
	}, false, func() error {
		return EndDispatch(root, id, "settled-round", got.Revision, DispatchFailed, "fixture", "user")
	})
}

// A wrap-up round completes into done, which is outside working and review: its
// binding is not settled by SettleDispatch there.
func TestDispatchSettleRefusesACardInDone(t *testing.T) {
	root, d, exit := wrapUpGrantFixture(t, true)
	id, rid := d.Input.TaskID, d.Input.ID
	next, err := AuthorizeDispatchWrapUp(root, id, rid, d.Revision, "coordinator", "原执行者退出，仅代收尾", exit)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"working", "done"} {
		if _, err = dispatchMove(t, root, next, target); err != nil {
			t.Fatal(err)
		}
	}
	done, err := ReadDispatch(root, id, rid)
	if err != nil {
		t.Fatal(err)
	}
	dispatchRefines(t, root, id, rid, rid, 0, settleEvent(done.Authorization, true, dispatchkernel.ColOther), false, func() error {
		return SettleDispatch(root, id, rid, done.Revision)
	})
}

func terminateEvent(reasoned, authOk, cycle bool) func(*dispatchView) dispatchkernel.Event {
	return func(*dispatchView) dispatchkernel.Event {
		return dispatchkernel.TerminateSettle{Reasoned: reasoned, AuthOk: authOk, Cycle: cycle}
	}
}

// A move into archived or trash settles a completed binding, with a reason and a
// decision, for the card's own authorization and its current cycle (theorem 4).
func TestDispatchTerminationSettlesRefineKernel(t *testing.T) {
	root := tempBoard(t)
	for i, tc := range []struct {
		name  string
		move  MoveOptions
		stale bool
		event func(*dispatchView) dispatchkernel.Event
		ok    bool
	}{
		{"no decision", MoveOptions{Result: "trashed", Reason: "stop"}, false, terminateEvent(false, true, true), false},
		{"another authorization", MoveOptions{Result: "trashed", Reason: "stop", Decision: "user", Authorization: ExecutionAuthorization{DispatchID: "foreign", Epoch: 1}}, false, terminateEvent(true, false, true), false},
		{"another cycle", MoveOptions{Result: "trashed", Reason: "stop", Decision: "user"}, true, terminateEvent(true, true, false), false},
		{"settled", MoveOptions{Result: "trashed", Reason: "stop", Decision: "user"}, false, terminateEvent(true, true, true), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := dispatchCard(t, root, "kernel-terminate-settle-"+string(rune('a'+i)))
			id := s.Entry.TaskID
			d := completedRound(t, root, id, "terminal-"+string(rune('a'+i)))
			if tc.stale {
				d = rewriteDispatch(t, root, d, func(d *Dispatch) { d.Cycle = "stale-cycle" })
			}
			dispatchRefines(t, root, id, d.Input.ID, d.Input.ID, 0, tc.event, tc.ok, func() error {
				_, err := MoveWithOptions(transactionSnapshot(t, root, id).Entry, root, "trash", tc.move)
				return err
			})
			if !tc.ok {
				return
			}
			got, err := ReadDispatch(root, id, d.Input.ID)
			if err != nil || got.State != DispatchCompleted || got.Settlement == nil || got.Release != nil {
				t.Fatalf("settled %+v %v", got, err)
			}
		})
	}
}

func takeoverSettleEvent(a ExecutionAuthorization, decided, rev bool) func(*dispatchView) dispatchkernel.Event {
	return func(v *dispatchView) dispatchkernel.Event {
		round, epoch := v.pair(a)
		return dispatchkernel.TakeoverSettle{AuthRound: round, AuthEpoch: epoch, Decided: decided, Rev: rev, Rec: true, Cycle: true}
	}
}

// A takeover that settles a completed round needs a decision and the revision it
// inspected; an open round is not settled by it (theorem 4).
func TestDispatchTakeoverSettlesRefineKernel(t *testing.T) {
	root := tempBoard(t)
	s := coordinatorCard(t, root, "kernel-takeover-settle")
	id := s.Entry.TaskID
	open := prepareTestDispatch(t, root, dispatchInput(s, "open"))
	entry := transactionSnapshot(t, root, id).Entry
	if _, err := BindExecutionCursor(root, entry); err != nil {
		t.Fatal(err)
	}
	takeover := func(entry Entry, d Dispatch, revision uint64, decision string) func() error {
		return func() error {
			_, err := BeginTakeover(root, entry, TakeoverInput{Owner: "codex", Session: "next", Decision: decision,
				Dispatch: &DispatchTakeover{Authorization: d.Authorization, Revision: revision, Mode: "settle-completed"}})
			return err
		}
	}
	dispatchRefines(t, root, id, "open", "open", 0, takeoverSettleEvent(open.Authorization, true, true), false, takeover(entry, open, open.Revision, "user"))
	for _, target := range []string{"working", "review"} {
		if _, err := dispatchMove(t, root, open, target); err != nil {
			t.Fatal(err)
		}
	}
	d, err := ReadDispatch(root, id, "open")
	if err != nil {
		t.Fatal(err)
	}
	entry = transactionSnapshot(t, root, id).Entry
	if _, err = BindExecutionCursor(root, entry); err != nil {
		t.Fatal(err)
	}
	dispatchRefines(t, root, id, "open", "open", 0, takeoverSettleEvent(d.Authorization, true, false), false, takeover(entry, d, d.Revision+1, "user"))
	dispatchRefines(t, root, id, "open", "open", 0, takeoverSettleEvent(d.Authorization, false, true), false, takeover(entry, d, d.Revision, ""))
	dispatchRefines(t, root, id, "open", "open", 0, takeoverSettleEvent(d.Authorization, true, true), true, takeover(entry, d, d.Revision, "user"))
	got, err := ReadDispatch(root, id, "open")
	if err != nil || got.State != DispatchCompleted || got.Settlement == nil || got.Settlement.Decision != "user" {
		t.Fatalf("settled %+v %v", got, err)
	}
}

// requireError asserts the code of an error of the board and, for an evidence
// error, the detail it names; the kernel decides, and the error of the first
// refusal in the old order stays the one returned.
func requireError(t *testing.T, err error, code, detail string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Code != code || !strings.Contains(e.Message, detail) {
		t.Fatalf("error %v, want %s %q", err, code, detail)
	}
}

const (
	evidenceCode = "board.dispatch_evidence_invalid"
	conflictCode = "board.dispatch_conflict"
	cycleCode    = "board.dispatch_cycle_changed"
)

// A request that fails several checks returns the error of the first one the
// entrance has always made, and each entrance keeps the error of its own check.
func TestDispatchGrantKeepsErrorOrder(t *testing.T) {
	root, d, exit := wrapUpGrantFixture(t, true)
	id, rid := d.Input.TaskID, d.Input.ID
	stale := exit
	stale.ObservedAt = time.Now().Add(-time.Minute)
	_, err := AuthorizeDispatchWrapUp(root, id, rid, d.Revision+1, "coordinator", "reason", stale)
	requireError(t, err, evidenceCode, "wrap-up authority conflict")
	_, err = AuthorizeDispatchWrapUp(root, id, rid, d.Revision, "coordinator", "reason", stale)
	requireError(t, err, evidenceCode, "fresh confirmed exit required")
	late := rewriteDispatch(t, root, d, func(d *Dispatch) { d.Cycle = "stale-cycle" })
	stale.CardRevision = transactionSnapshot(t, root, id).Revision
	_, err = AuthorizeDispatchWrapUp(root, id, rid, late.Revision, "coordinator", "reason", stale)
	requireError(t, err, cycleCode, rid)

	root, d, exit = wrapUpGrantFixture(t, true)
	next, err := AuthorizeDispatchWrapUp(root, id, rid, d.Revision, "coordinator", "reason", exit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = dispatchMove(t, root, next, "working"); err != nil {
		t.Fatal(err)
	}
	if err = WithTransaction(root, LockScope{Tasks: []string{id}, ReadOnly: true}, func(tx *Transaction) error {
		return tx.requireFullExecution(transactionSnapshot(t, root, id))
	}); true {
		requireError(t, err, evidenceCode, "wrap-up-only authority cannot modify")
	}
	write := func(o UpdateOptions) error {
		o.ExpectedRevision, o.Authorization = transactionSnapshot(t, root, id).Revision, next.Authorization
		return UpdateDocument(root, id, o)
	}
	requireError(t, write(UpdateOptions{Document: "spec.md", Text: "x", ContractDecision: "user"}), evidenceCode, "wrap-up cannot change contract")
	requireError(t, write(UpdateOptions{Document: "spec.md", Text: "x"}), evidenceCode, "must append WRAP_UP_RECORDS")
	requireError(t, write(UpdateOptions{Document: "notes.md", Text: "x"}), evidenceCode, "record path required")
	// The grant is refused first, before the cycle and the revision.
	next, err = ReadDispatch(root, id, rid)
	if err != nil {
		t.Fatal(err)
	}
	next = rewriteDispatch(t, root, next, func(d *Dispatch) { d.Cycle = "stale-cycle" })
	_, err = BeginDispatchAttempt(root, id, rid, next.Revision+1)
	requireError(t, err, evidenceCode, "wrap-up-only grant cannot launch or deliver")
	_, err = ReauthorizeDispatch(root, id, rid, next.Revision+1)
	requireError(t, err, conflictCode, "")
}

// A settlement of a round of another cycle is refused with the cycle error, after
// the revision and the phase checks.
func TestDispatchSettleKeepsCycleError(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "kernel-settle-cycle")
	id := s.Entry.TaskID
	d := completedRound(t, root, id, "cycle-round")
	d = rewriteDispatch(t, root, d, func(d *Dispatch) { d.Cycle = "stale-cycle" })
	dispatchRefines(t, root, id, "cycle-round", "cycle-round", 0, func(v *dispatchView) dispatchkernel.Event {
		round, epoch := v.pair(d.Authorization)
		return dispatchkernel.Settle{AuthRound: round, AuthEpoch: epoch, Cas: true, Col: dispatchkernel.ColReview}
	}, false, func() error {
		err := SettleDispatch(root, id, "cycle-round", d.Revision)
		requireError(t, err, cycleCode, "cycle-round")
		return err
	})
	requireError(t, SettleDispatch(root, id, "cycle-round", d.Revision+1), conflictCode, "")
}
