package board

import (
	"strconv"
	"strings"
	"testing"
	"time"

	dispatchkernel "github.com/dualface/kander/internal/statemachine/dispatch"
)

// projectDispatchAt is the slot of card id with the record named record (none
// when empty), projected with the epoch shift base, or the one the card fixes.
func projectDispatchAt(t *testing.T, root, id, record string, base *uint64) *dispatchView {
	t.Helper()
	var v *dispatchView
	err := WithTransaction(root, LockScope{Tasks: []string{id}, ReadOnly: true}, func(tx *Transaction) error {
		s, err := tx.Snapshot(id)
		if err != nil {
			return err
		}
		var d *Dispatch
		if record != "" {
			read, err := readDispatch(tx, id, record)
			if err != nil {
				return err
			}
			d = &read
		}
		v = projectDispatchBase(s, d, base)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// dispatchRefines runs fn and asserts that the projection after it, with the
// epoch shift fixed before it, equals the kernel step from the projection
// before it. from and to name the record each projection holds; a new round
// raises the relative round by origin. A refused entrance must fail, the kernel
// must refuse the event, and the projection must stay.
func dispatchRefines(t *testing.T, root, id, from, to string, origin uint64, event func(before *dispatchView) dispatchkernel.Event, accepted bool, fn func() error) {
	t.Helper()
	before := projectDispatchAt(t, root, id, from, nil)
	e := event(before)
	err := fn()
	after := projectDispatchAt(t, root, id, to, &before.base)
	if !accepted {
		if err == nil {
			t.Fatalf("refused %T succeeded", e)
		}
		if _, ok := before.next(e); ok {
			t.Fatalf("kernel accepts %T on %+v", e, before.state)
		}
		if after.state != before.state {
			t.Fatalf("refused %T changed the projection: %+v -> %+v", e, before.state, after.state)
		}
		return
	}
	if err != nil {
		t.Fatalf("%T: %v", e, err)
	}
	want, ok := before.next(e)
	if !ok {
		t.Fatalf("kernel refuses %T on %+v", e, before.state)
	}
	after.state.Round += origin
	if after.state != want {
		t.Fatalf("%T: projection %+v, kernel %+v", e, after.state, want)
	}
}

// rewriteDispatch replaces the stored record of d with mutate(d), the card's
// binding epoch and the receipts named by it with the new epoch. A fixture, as
// production never rewrites a record.
func rewriteDispatch(t *testing.T, root string, d Dispatch, mutate func(*Dispatch)) Dispatch {
	t.Helper()
	oldEpoch := d.Authorization.Epoch
	mutate(&d)
	err := WithTransaction(root, LockScope{Tasks: []string{d.Input.TaskID}}, func(tx *Transaction) error {
		s, err := tx.Snapshot(d.Input.TaskID)
		if err != nil {
			return err
		}
		for name, value := range map[string]any{"intent": d, "state": d} {
			if err := tx.Put(d.Input.TaskID, dispatchPath(d.Input.ID, name), reviewJSON(value)); err != nil {
				return err
			}
		}
		for name, receipt := range map[string]*DispatchReceipt{"accepted": d.Accepted, "completed": d.Completed} {
			if receipt != nil && d.Authorization.Epoch != oldEpoch {
				if err := tx.Put(d.Input.TaskID, dispatchPath(d.Input.ID, name+"-"+strconv.FormatUint(d.Authorization.Epoch, 10)), reviewJSON(receipt)); err != nil {
					return err
				}
			}
		}
		if d.Authorization.Epoch == oldEpoch || authFrom(s.Text).DispatchID != d.Input.ID {
			return nil
		}
		text, err := setMetadata(s.Text, "EXECUTION_EPOCH", strconv.FormatUint(d.Authorization.Epoch, 10))
		if err != nil {
			return err
		}
		return tx.Put(d.Input.TaskID, "spec.md", text)
	})
	if err != nil {
		t.Fatal(err)
	}
	read, err := ReadDispatch(root, d.Input.TaskID, d.Input.ID)
	if err != nil {
		t.Fatal(err)
	}
	return read
}

func expiredInput(s Snapshot, id string) DispatchInput {
	in := dispatchInput(s, id)
	in.CreatedAt = time.Now().UTC().Add(-10 * time.Minute)
	in.ConfirmBy = in.CreatedAt.Add(5 * time.Minute)
	return in
}

// prepareEvent is the kernel event of a round of kind sync created on a card in
// the review column.
func prepareEvent(free, room bool, col dispatchkernel.Col) func(*dispatchView) dispatchkernel.Event {
	return func(*dispatchView) dispatchkernel.Event {
		return dispatchkernel.Prepare{Kind: dispatchkernel.KindSync, Col: col, Free: free, Room: room}
	}
}

func prepareFn(root string, in DispatchInput) func() error {
	return func() error { _, err := PrepareDispatch(root, in); return err }
}

// Rounds are created, retried, and refused as theorems 1 and 2 state: one open
// binding per card, a new round only after a terminal one, and the epoch ceiling.
func TestDispatchPrepareRefinesKernel(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "kernel-prepare")
	id := s.Entry.TaskID
	one := dispatchInput(s, "one")
	dispatchRefines(t, root, id, "", "one", 0, prepareEvent(true, true, dispatchkernel.ColReview), true, prepareFn(root, one))
	retry := func(same bool) func(*dispatchView) dispatchkernel.Event {
		return func(*dispatchView) dispatchkernel.Event { return dispatchkernel.Retry{Same: same} }
	}
	dispatchRefines(t, root, id, "one", "one", 0, retry(true), true, prepareFn(root, one))
	changed := one
	changed.Message += "!"
	dispatchRefines(t, root, id, "one", "one", 0, retry(false), false, prepareFn(root, changed))
	// An open round blocks the next one.
	dispatchRefines(t, root, id, "one", "one", 0, prepareEvent(true, true, dispatchkernel.ColReview), false, prepareFn(root, dispatchInput(s, "two")))
	d, err := ReadDispatch(root, id, "one")
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"working", "review"} {
		if _, err := dispatchMove(t, root, d, target); err != nil {
			t.Fatal(err)
		}
	}
	// A completed binding is terminal: the next round raises the round and the epoch.
	dispatchRefines(t, root, id, "one", "two", 1, prepareEvent(true, true, dispatchkernel.ColReview), true, prepareFn(root, dispatchInput(s, "two")))
	if got := projectDispatchAt(t, root, id, "two", nil).binding.Epoch; got != 2 {
		t.Fatalf("epoch of the next round %d", got)
	}

	// A card outside working and review takes no round.
	idle := transactionCard(t, root, "kernel-prepare-idle", false)
	dispatchRefines(t, root, idle.Entry.TaskID, "", "", 0, prepareEvent(true, true, dispatchkernel.ColOther), false, prepareFn(root, dispatchInput(idle, "idle")))
	// A start that has not finished blocks a round until it is adopted.
	started := startedCard(t, root, "kernel-prepare-start")
	startedSnap := transactionSnapshot(t, root, started.TaskID)
	dispatchRefines(t, root, started.TaskID, "", "", 0, prepareEvent(false, true, dispatchkernel.ColWorking), false, prepareFn(root, dispatchInput(startedSnap, "start")))
}

// The epoch is the card's plus one from any value, and the last one is a ceiling.
func TestDispatchPrepareEpochRefinesKernel(t *testing.T) {
	for _, epoch := range []uint64{1 << 31, 1 << 40, ^uint64(0) - 1, ^uint64(0)} {
		root := tempBoard(t)
		s := dispatchCard(t, root, "kernel-epoch")
		id := s.Entry.TaskID
		d := prepareTestDispatch(t, root, dispatchInput(s, "one"))
		for _, target := range []string{"working", "review"} {
			if _, err := dispatchMove(t, root, d, target); err != nil {
				t.Fatal(err)
			}
		}
		d, err := ReadDispatch(root, id, "one")
		if err != nil {
			t.Fatal(err)
		}
		d = rewriteDispatch(t, root, d, func(d *Dispatch) { d.Authorization.Epoch = epoch })
		room := epoch != ^uint64(0)
		dispatchRefines(t, root, id, "one", map[bool]string{true: "two", false: "one"}[room], 1, prepareEvent(true, room, dispatchkernel.ColReview), room, prepareFn(root, dispatchInput(s, "two")))
	}
}

func attemptEvent(a ExecutionAuthorization, cas, before bool) func(*dispatchView) dispatchkernel.Event {
	return func(v *dispatchView) dispatchkernel.Event {
		round, epoch := v.pair(a)
		return dispatchkernel.Attempt{AuthRound: round, AuthEpoch: epoch, Cas: cas, Cycle: true, Evidence: true, Before: before}
	}
}

func reauthorizeEvent(a ExecutionAuthorization, room, fresh, before bool) func(*dispatchView) dispatchkernel.Event {
	return func(v *dispatchView) dispatchkernel.Event {
		round, epoch := v.pair(a)
		return dispatchkernel.Reauthorize{AuthRound: round, AuthEpoch: epoch, Cas: true, Room: room, Cycle: true, Fresh: fresh, Before: before}
	}
}

// An attempt marks a pending round uncertain at its revision and before its
// deadline (theorem 3); a reauthorization raises the epoch of an open round and an
// accepted one needs fresh evidence of the stop (theorem 7).
func TestDispatchAttemptAndReauthorizeRefineKernel(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "kernel-attempt")
	id := s.Entry.TaskID
	text, _ := setMetadata(s.Text, FieldSession, "codex original-session")
	text, _ = setMetadata(text, FieldWindow, "herdr:t1:p1")
	text, _ = setMetadata(text, FieldOwner, "codex")
	if err := fixtureWrite(root, s.Entry, text); err != nil {
		t.Fatal(err)
	}
	d := prepareTestDispatch(t, root, dispatchInput(s, "one"))
	attempt := func(expected uint64) func() error {
		return func() error { _, err := BeginDispatchAttempt(root, id, "one", expected); return err }
	}
	dispatchRefines(t, root, id, "one", "one", 0, attemptEvent(d.Authorization, false, true), false, attempt(d.Revision+1))
	dispatchRefines(t, root, id, "one", "one", 0, attemptEvent(d.Authorization, true, true), true, attempt(d.Revision))
	d, _ = ReadDispatch(root, id, "one")
	dispatchRefines(t, root, id, "one", "one", 0, attemptEvent(d.Authorization, true, true), true, attempt(d.Revision))
	d, _ = ReadDispatch(root, id, "one")

	reauthorize := func(stopped ...WrapUpExitEvidence) func() error {
		return func() error {
			cur, _ := ReadDispatch(root, id, "one")
			_, err := ReauthorizeDispatch(root, id, "one", cur.Revision, stopped...)
			return err
		}
	}
	dispatchRefines(t, root, id, "one", "one", 0, reauthorizeEvent(d.Authorization, true, false, true), true, reauthorize())
	d, _ = ReadDispatch(root, id, "one")
	if d.Authorization.Epoch != 2 || d.State != DispatchPrepared {
		t.Fatalf("reauthorized %+v", d)
	}
	if _, err := dispatchMove(t, root, d, "working"); err != nil {
		t.Fatal(err)
	}
	// An accepted round needs a fresh stopped exit.
	dispatchRefines(t, root, id, "one", "one", 0, reauthorizeEvent(d.Authorization, true, false, true), false, reauthorize())
	exit := stoppedDispatchFixture(transactionSnapshot(t, root, id))
	d, _ = ReadDispatch(root, id, "one")
	dispatchRefines(t, root, id, "one", "one", 0, reauthorizeEvent(d.Authorization, true, true, true), true, reauthorize(exit))
	d, _ = ReadDispatch(root, id, "one")
	// A terminal round is never reauthorized or attempted again.
	for _, target := range []string{"working", "review"} {
		if _, err := dispatchMove(t, root, d, target); err != nil {
			t.Fatal(err)
		}
	}
	d, _ = ReadDispatch(root, id, "one")
	dispatchRefines(t, root, id, "one", "one", 0, reauthorizeEvent(d.Authorization, true, true, true), false, reauthorize())
	dispatchRefines(t, root, id, "one", "one", 0, attemptEvent(d.Authorization, true, true), false, attempt(d.Revision))

	// The epoch ceiling and the deadline refuse too.
	other := dispatchCard(t, root, "kernel-attempt-late")
	late := prepareTestDispatch(t, root, expiredInput(other, "late"))
	oid := other.Entry.TaskID
	dispatchRefines(t, root, oid, "late", "late", 0, attemptEvent(late.Authorization, true, false), false, func() error {
		_, err := BeginDispatchAttempt(root, oid, "late", late.Revision)
		return err
	})
	dispatchRefines(t, root, oid, "late", "late", 0, reauthorizeEvent(late.Authorization, true, false, false), false, func() error {
		_, err := ReauthorizeDispatch(root, oid, "late", late.Revision)
		return err
	})
	late = rewriteDispatch(t, root, late, func(d *Dispatch) { d.Authorization.Epoch = ^uint64(0); d.Input = prepareFresh(d.Input) })
	dispatchRefines(t, root, oid, "late", "late", 0, reauthorizeEvent(late.Authorization, false, false, true), false, func() error {
		_, err := ReauthorizeDispatch(root, oid, "late", late.Revision)
		return err
	})
}

// prepareFresh gives a backdated input a deadline in the future; a fixture.
func prepareFresh(in DispatchInput) DispatchInput {
	in.ConfirmBy = time.Now().UTC().Add(time.Hour)
	return in
}

func acceptEvent(a ExecutionAuthorization, rec, before bool) func(*dispatchView) dispatchkernel.Event {
	return func(v *dispatchView) dispatchkernel.Event {
		round, epoch := v.pair(a)
		return dispatchkernel.Accept{AuthRound: round, AuthEpoch: epoch, Rec: rec, Cycle: true, Before: before}
	}
}

func completeEvent(a ExecutionAuthorization, toDone, same bool, col dispatchkernel.Col) func(*dispatchView) dispatchkernel.Event {
	return func(v *dispatchView) dispatchkernel.Event {
		round, epoch := v.pair(a)
		return dispatchkernel.Complete{AuthRound: round, AuthEpoch: epoch, Rec: true, Cycle: true, ToDone: toDone, Commit: true,
			Evidence: true, Disp: true, Same: same, Col: col}
	}
}

// A move with the authorization accepts a pending round before its deadline and
// replays an accepted one (theorems 3 and 4); completing needs an accepted round and
// the kind's own target; an identical receipt replays and another is refused; a
// foreign pair is refused (theorem 2).
func TestDispatchAcceptAndCompleteRefineKernel(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "kernel-move")
	id := s.Entry.TaskID
	d := prepareTestDispatch(t, root, dispatchInput(s, "one"))
	move := func(a ExecutionAuthorization, target, commit string) func() error {
		return func() error {
			_, err := MoveWithOptions(transactionSnapshot(t, root, id).Entry, root, target, MoveOptions{Authorization: a, DeliveryCommit: commit})
			return err
		}
	}
	commit := strings.Repeat("b", 40)
	foreign := ExecutionAuthorization{DispatchID: d.Authorization.DispatchID, Epoch: d.Authorization.Epoch + 1}
	dispatchRefines(t, root, id, "one", "one", 0, acceptEvent(foreign, true, true), false, move(foreign, "working", ""))
	other := ExecutionAuthorization{DispatchID: "another", Epoch: 1}
	dispatchRefines(t, root, id, "one", "one", 0, acceptEvent(other, true, true), false, move(other, "working", ""))
	// Completion before acceptance.
	dispatchRefines(t, root, id, "one", "one", 0, completeEvent(d.Authorization, false, false, dispatchkernel.ColReview), false, move(d.Authorization, "review", commit))
	dispatchRefines(t, root, id, "one", "one", 0, acceptEvent(d.Authorization, true, true), true, move(d.Authorization, "working", ""))
	// An accepted round replays its acceptance.
	dispatchRefines(t, root, id, "one", "one", 0, acceptEvent(d.Authorization, true, true), true, move(d.Authorization, "working", ""))
	dispatchRefines(t, root, id, "one", "one", 0, completeEvent(foreign, false, false, dispatchkernel.ColWorking), false, move(foreign, "review", commit))
	// A sync round completes only into review.
	dispatchRefines(t, root, id, "one", "one", 0, completeEvent(d.Authorization, true, false, dispatchkernel.ColWorking), false, move(d.Authorization, "done", commit))
	dispatchRefines(t, root, id, "one", "one", 0, completeEvent(d.Authorization, false, false, dispatchkernel.ColWorking), true, move(d.Authorization, "review", commit))
	// The identical receipt replays; another commit does not.
	dispatchRefines(t, root, id, "one", "one", 0, completeEvent(d.Authorization, false, true, dispatchkernel.ColReview), true, move(d.Authorization, "review", commit))
	dispatchRefines(t, root, id, "one", "one", 0, completeEvent(d.Authorization, false, false, dispatchkernel.ColReview), false, move(d.Authorization, "review", strings.Repeat("c", 40)))

	// A round past its deadline is not accepted.
	late := dispatchCard(t, root, "kernel-move-late")
	lid := late.Entry.TaskID
	l := prepareTestDispatch(t, root, expiredInput(late, "late"))
	dispatchRefines(t, root, lid, "late", "late", 0, acceptEvent(l.Authorization, true, false), false, func() error {
		_, err := MoveWithOptions(transactionSnapshot(t, root, lid).Entry, root, "working", MoveOptions{Authorization: l.Authorization})
		return err
	})
}

func endEvent(d Dispatch, cas, decided, toFailed bool) func(*dispatchView) dispatchkernel.Event {
	return func(v *dispatchView) dispatchkernel.Event {
		round, epoch := v.pair(d.Authorization)
		return dispatchkernel.End{AuthRound: round, AuthEpoch: epoch, Cas: cas, Room: true, Cycle: true, Decided: decided, ToFailed: toFailed}
	}
}

// Ending a round releases its binding once: an accepted round needs a decision, a
// legacy terminal round is released only with its own state and a decision, and a
// released round or a stale revision is refused (theorem 6).
func TestDispatchEndRefinesKernel(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "kernel-end")
	id := s.Entry.TaskID
	d := prepareTestDispatch(t, root, dispatchInput(s, "one"))
	end := func(d Dispatch, expected uint64, state DispatchState, decisions ...string) func() error {
		return func() error { return EndDispatch(root, id, d.Input.ID, expected, state, "fixture", decisions...) }
	}
	dispatchRefines(t, root, id, "one", "one", 0, endEvent(d, false, false, true), false, end(d, d.Revision+1, DispatchFailed))
	dispatchRefines(t, root, id, "one", "one", 0, endEvent(d, true, false, true), true, end(d, d.Revision, DispatchFailed))
	// The release left no binding: a second end finds none, and the card takes a new round.
	released, _ := ReadDispatch(root, id, "one")
	dispatchRefines(t, root, id, "one", "one", 0, endEvent(released, true, true, true), false, end(released, released.Revision, DispatchFailed, "user"))
	dispatchRefines(t, root, id, "one", "two", 1, prepareEvent(true, true, dispatchkernel.ColReview), true, prepareFn(root, dispatchInput(s, "two")))

	// An accepted round is ended only with a decision.
	two, _ := ReadDispatch(root, id, "two")
	if _, err := dispatchMove(t, root, two, "working"); err != nil {
		t.Fatal(err)
	}
	two, _ = ReadDispatch(root, id, "two")
	dispatchRefines(t, root, id, "two", "two", 0, endEvent(two, true, false, false), false, end(two, two.Revision, DispatchCancelled))
	dispatchRefines(t, root, id, "two", "two", 0, endEvent(two, true, true, false), true, end(two, two.Revision, DispatchCancelled, "user"))

	// A legacy terminal binding needs its own state and a decision to be released.
	third := dispatchCard(t, root, "kernel-end-legacy")
	tid := third.Entry.TaskID
	legacy := prepareTestDispatch(t, root, dispatchInput(third, "legacy"))
	legacy = rewriteDispatch(t, root, legacy, func(d *Dispatch) { d.State, d.Reason = DispatchFailed, "legacy" })
	endLegacy := func(state DispatchState, decisions ...string) func() error {
		return func() error { return EndDispatch(root, tid, "legacy", legacy.Revision, state, "fixture", decisions...) }
	}
	dispatchRefines(t, root, tid, "legacy", "legacy", 0, endEvent(legacy, true, true, false), false, endLegacy(DispatchCancelled, "user"))
	dispatchRefines(t, root, tid, "legacy", "legacy", 0, endEvent(legacy, true, false, true), false, endLegacy(DispatchFailed))
	dispatchRefines(t, root, tid, "legacy", "legacy", 0, endEvent(legacy, true, true, true), true, endLegacy(DispatchFailed, "user"))
	// A completed round is settled, not ended.
	done := dispatchCard(t, root, "kernel-end-completed")
	did := done.Entry.TaskID
	c := prepareTestDispatch(t, root, dispatchInput(done, "completed"))
	for _, target := range []string{"working", "review"} {
		if _, err := dispatchMove(t, root, c, target); err != nil {
			t.Fatal(err)
		}
	}
	c, _ = ReadDispatch(root, did, "completed")
	dispatchRefines(t, root, did, "completed", "completed", 0, endEvent(c, true, true, true), false, func() error {
		return EndDispatch(root, did, "completed", c.Revision, DispatchFailed, "fixture", "user")
	})
}

// A round of another plan cycle is refused by every entrance that checks one
// (theorem 8): a card whose record names a cycle the plan has left.
func TestDispatchCycleRefinesKernel(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "kernel-cycle")
	id := s.Entry.TaskID
	d := prepareTestDispatch(t, root, dispatchInput(s, "one"))
	d = rewriteDispatch(t, root, d, func(d *Dispatch) { d.Cycle = "stale-cycle" })
	cycle := func(f func(*dispatchView) dispatchkernel.Event) func(*dispatchView) dispatchkernel.Event {
		return func(v *dispatchView) dispatchkernel.Event { return f(v) }
	}
	dispatchRefines(t, root, id, "one", "one", 0, cycle(func(v *dispatchView) dispatchkernel.Event {
		round, epoch := v.pair(d.Authorization)
		return dispatchkernel.Attempt{AuthRound: round, AuthEpoch: epoch, Cas: true, Evidence: true, Before: true}
	}), false, func() error { _, err := BeginDispatchAttempt(root, id, "one", d.Revision); return err })
	dispatchRefines(t, root, id, "one", "one", 0, cycle(func(v *dispatchView) dispatchkernel.Event {
		round, epoch := v.pair(d.Authorization)
		return dispatchkernel.Reauthorize{AuthRound: round, AuthEpoch: epoch, Cas: true, Room: true, Before: true}
	}), false, func() error { _, err := ReauthorizeDispatch(root, id, "one", d.Revision); return err })
	dispatchRefines(t, root, id, "one", "one", 0, cycle(func(v *dispatchView) dispatchkernel.Event {
		round, epoch := v.pair(d.Authorization)
		return dispatchkernel.Accept{AuthRound: round, AuthEpoch: epoch, Rec: true, Before: true}
	}), false, func() error {
		_, err := MoveWithOptions(transactionSnapshot(t, root, id).Entry, root, "working", MoveOptions{Authorization: d.Authorization})
		return err
	})
	dispatchRefines(t, root, id, "one", "one", 0, cycle(func(v *dispatchView) dispatchkernel.Event {
		round, epoch := v.pair(d.Authorization)
		return dispatchkernel.End{AuthRound: round, AuthEpoch: epoch, Cas: true, Room: true, Decided: true, ToFailed: true}
	}), false, func() error { return EndDispatch(root, id, "one", d.Revision, DispatchFailed, "fixture", "user") })
	dispatchRefines(t, root, id, "one", "one", 0, cycle(func(*dispatchView) dispatchkernel.Event {
		return dispatchkernel.Cancel{Reasoned: true, AuthOk: true}
	}), false, func() error {
		_, err := MoveWithOptions(transactionSnapshot(t, root, id).Entry, root, "trash", MoveOptions{Result: "trashed", Reason: "fixture", Decision: "user"})
		return err
	})

	// A completion and a retaining takeover of an accepted round refuse a stale cycle too.
	other := coordinatorCard(t, root, "kernel-cycle-accepted")
	oid := other.Entry.TaskID
	a := prepareTestDispatch(t, root, dispatchInput(other, "accepted"))
	if _, err := dispatchMove(t, root, a, "working"); err != nil {
		t.Fatal(err)
	}
	a, _ = ReadDispatch(root, oid, "accepted")
	a = rewriteDispatch(t, root, a, func(d *Dispatch) { d.Cycle = "stale-cycle" })
	dispatchRefines(t, root, oid, "accepted", "accepted", 0, cycle(func(v *dispatchView) dispatchkernel.Event {
		round, epoch := v.pair(a.Authorization)
		return dispatchkernel.Complete{AuthRound: round, AuthEpoch: epoch, Rec: true, Commit: true, Evidence: true, Disp: true, Col: dispatchkernel.ColWorking}
	}), false, func() error {
		_, err := MoveWithOptions(transactionSnapshot(t, root, oid).Entry, root, "review", MoveOptions{Authorization: a.Authorization, DeliveryCommit: strings.Repeat("b", 40)})
		return err
	})
	entry := transactionSnapshot(t, root, oid).Entry
	if _, err := BindExecutionCursor(root, entry); err != nil {
		t.Fatal(err)
	}
	dispatchRefines(t, root, oid, "accepted", "accepted", 0, cycle(func(v *dispatchView) dispatchkernel.Event {
		round, epoch := v.pair(a.Authorization)
		return dispatchkernel.Retain{AuthRound: round, AuthEpoch: epoch, Decided: true, Rev: true, Rec: true}
	}), false, func() error {
		_, err := BeginTakeover(root, entry, TakeoverInput{Owner: "codex", Session: "next", Decision: "user",
			Dispatch: &DispatchTakeover{Authorization: a.Authorization, Revision: a.Revision, Mode: "retain"}})
		return err
	})
}

// A move into trash cancels an open binding without releasing it, leaves a
// failed or cancelled one alone, and needs a reason and a decision (theorem 5).
func TestDispatchTerminationRefinesKernel(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "kernel-terminate")
	id := s.Entry.TaskID
	prepareTestDispatch(t, root, dispatchInput(s, "one"))
	trash := func(o MoveOptions) func() error {
		return func() error {
			_, err := MoveWithOptions(transactionSnapshot(t, root, id).Entry, root, "trash", o)
			return err
		}
	}
	cancel := func(reasoned bool) func(*dispatchView) dispatchkernel.Event {
		return func(*dispatchView) dispatchkernel.Event {
			return dispatchkernel.Cancel{Reasoned: reasoned, AuthOk: true, Cycle: true}
		}
	}
	dispatchRefines(t, root, id, "one", "one", 0, cancel(false), false, trash(MoveOptions{Result: "trashed", Reason: "fixture"}))
	dispatchRefines(t, root, id, "one", "one", 0, cancel(true), true, trash(MoveOptions{Result: "trashed", Reason: "fixture", Decision: "user"}))
	if got, _ := ReadDispatch(root, id, "one"); got.State != DispatchCancelled || got.Reason != "fixture" {
		t.Fatalf("cancelled %+v", got)
	}

	// A binding that is already failed stays as it is.
	other := dispatchCard(t, root, "kernel-terminate-failed")
	oid := other.Entry.TaskID
	d := prepareTestDispatch(t, root, dispatchInput(other, "failed"))
	rewriteDispatch(t, root, d, func(d *Dispatch) { d.State, d.Reason = DispatchFailed, "legacy" })
	dispatchRefines(t, root, oid, "failed", "failed", 0, cancel(true), true, func() error {
		_, err := MoveWithOptions(transactionSnapshot(t, root, oid).Entry, root, "trash", MoveOptions{Result: "trashed", Reason: "fixture", Decision: "user"})
		return err
	})
}

func retainEvent(a ExecutionAuthorization, decided, rev bool) func(*dispatchView) dispatchkernel.Event {
	return func(v *dispatchView) dispatchkernel.Event {
		round, epoch := v.pair(a)
		return dispatchkernel.Retain{AuthRound: round, AuthEpoch: epoch, Decided: decided, Rev: rev, Rec: true, Cycle: true}
	}
}

// A takeover that retains a round keeps an open one with its authorization and
// refuses a stale revision, a missing decision, or a terminal round.
func TestDispatchRetainRefinesKernel(t *testing.T) {
	root := tempBoard(t)
	s := coordinatorCard(t, root, "kernel-retain")
	id := s.Entry.TaskID
	d := prepareTestDispatch(t, root, dispatchInput(s, "one"))
	current := transactionSnapshot(t, root, id)
	if _, err := BindExecutionCursor(root, current.Entry); err != nil {
		t.Fatal(err)
	}
	takeover := func(revision uint64, decision string) func() error {
		return func() error {
			_, err := BeginTakeover(root, current.Entry, TakeoverInput{Owner: "codex", Session: "next", Decision: decision,
				Dispatch: &DispatchTakeover{Authorization: d.Authorization, Revision: revision, Mode: "retain"}})
			return err
		}
	}
	dispatchRefines(t, root, id, "one", "one", 0, retainEvent(d.Authorization, true, false), false, takeover(d.Revision+1, "user"))
	dispatchRefines(t, root, id, "one", "one", 0, retainEvent(d.Authorization, true, true), true, takeover(d.Revision, "user"))

	// A completed round is not retained.
	done := coordinatorCard(t, root, "kernel-retain-completed")
	did := done.Entry.TaskID
	c := prepareTestDispatch(t, root, dispatchInput(done, "completed"))
	for _, target := range []string{"working", "review"} {
		if _, err := dispatchMove(t, root, c, target); err != nil {
			t.Fatal(err)
		}
	}
	c, _ = ReadDispatch(root, did, "completed")
	entry := transactionSnapshot(t, root, did).Entry
	if _, err := BindExecutionCursor(root, entry); err != nil {
		t.Fatal(err)
	}
	dispatchRefines(t, root, did, "completed", "completed", 0, retainEvent(c.Authorization, true, true), false, func() error {
		_, err := BeginTakeover(root, entry, TakeoverInput{Owner: "codex", Session: "next", Decision: "user",
			Dispatch: &DispatchTakeover{Authorization: c.Authorization, Revision: c.Revision, Mode: "retain"}})
		return err
	})
}

func execEvent(a ExecutionAuthorization, before, author bool) func(*dispatchView) dispatchkernel.Event {
	return func(v *dispatchView) dispatchkernel.Event {
		round, epoch := v.pair(a)
		return dispatchkernel.Exec{AuthRound: round, AuthEpoch: epoch, Rec: true, Before: before, Author: author}
	}
}

// Writes of the executor: a card with no binding takes a write that presents none
// and refuses one that does; a pending round backs a runtime write but no author
// write; an accepted round backs both; a foreign pair is refused (theorem 2).
func TestDispatchExecutorWritesRefineKernel(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "kernel-exec")
	id := s.Entry.TaskID
	write := func(a ExecutionAuthorization, name string) func() error {
		return func() error {
			cur := transactionSnapshot(t, root, id)
			return UpdateDocument(root, id, UpdateOptions{Document: name, Text: "note", ExpectedRevision: cur.Revision, Authorization: a})
		}
	}
	none := ExecutionAuthorization{}
	dispatchRefines(t, root, id, "", "", 0, execEvent(none, true, true), true, write(none, "one.md"))
	phantom := ExecutionAuthorization{DispatchID: "phantom", Epoch: 1}
	dispatchRefines(t, root, id, "", "", 0, execEvent(phantom, true, true), false, write(phantom, "two.md"))
	d := prepareTestDispatch(t, root, dispatchInput(s, "one"))
	// No authorization on a bound card, a foreign pair, and an author write on a pending round.
	dispatchRefines(t, root, id, "one", "one", 0, execEvent(none, true, true), false, write(none, "three.md"))
	foreign := ExecutionAuthorization{DispatchID: d.Authorization.DispatchID, Epoch: d.Authorization.Epoch + 1}
	dispatchRefines(t, root, id, "one", "one", 0, execEvent(foreign, true, true), false, write(foreign, "four.md"))
	dispatchRefines(t, root, id, "one", "one", 0, execEvent(d.Authorization, true, true), false, write(d.Authorization, "five.md"))
	// A runtime write needs only the binding and the deadline.
	read := func(a ExecutionAuthorization) func() error {
		return func() error { _, err := ReadExecutionSnapshot(root, id, a); return err }
	}
	dispatchRefines(t, root, id, "one", "one", 0, execEvent(d.Authorization, true, false), true, read(d.Authorization))
	if _, err := dispatchMove(t, root, d, "working"); err != nil {
		t.Fatal(err)
	}
	dispatchRefines(t, root, id, "one", "one", 0, execEvent(d.Authorization, true, true), true, write(d.Authorization, "six.md"))
	// A pending round past its deadline backs no write.
	late := dispatchCard(t, root, "kernel-exec-late")
	lid := late.Entry.TaskID
	l := prepareTestDispatch(t, root, expiredInput(late, "late"))
	dispatchRefines(t, root, lid, "late", "late", 0, execEvent(l.Authorization, false, false), false, func() error {
		_, err := ReadExecutionSnapshot(root, lid, l.Authorization)
		return err
	})
	// A terminal round backs none.
	for _, target := range []string{"review"} {
		if _, err := dispatchMove(t, root, d, target); err != nil {
			t.Fatal(err)
		}
	}
	dispatchRefines(t, root, id, "one", "one", 0, func(v *dispatchView) dispatchkernel.Event {
		return execEvent(d.Authorization, true, false)(v)
	}, false, read(d.Authorization))
}
