package board

import (
	"strings"
	"testing"
)

// R20: a dispatch request names its revision when only the revision is stale, and
// names the dispatch state when the revision holds and the kernel refuses.
func TestDispatchRefusalNamesValues(t *testing.T) {
	english(t)
	root := tempBoard(t)
	s := dispatchCard(t, root, "dispatch-values")
	d := prepareTestDispatch(t, root, dispatchInput(s, "values-one"))
	task, id := d.Input.TaskID, d.Input.ID
	stale := d.Revision + 3

	staleRevision := func(err error) {
		t.Helper()
		requireRefusal(t, err, "board.dispatch_conflict", []string{id, "the request names dispatch revision " + num(stale), "the dispatch is at revision " + num(d.Revision), "kander dispatch show"}, "the dispatch is in state", "card revision")
	}
	// Only the revision is stale.
	_, err := BeginDispatchAttempt(root, task, id, stale)
	staleRevision(err)
	_, err = ReauthorizeDispatch(root, task, id, stale)
	staleRevision(err)
	staleRevision(EndDispatch(root, task, id, stale, DispatchFailed, "stopped"))
	staleRevision(SettleDispatch(root, task, id, stale))

	// The dispatch revision holds and the card revision is stale.
	card := transactionSnapshot(t, root, task).Revision
	_, err = BeginDispatchAttempt(root, task, id, d.Revision, card+9)
	requireRefusal(t, err, "board.dispatch_conflict", []string{id, "the request names card revision " + num(card+9), "the card is at revision " + num(card)}, "dispatch revision", "the dispatch is in state")

	// The revision holds and the kernel refuses the phase: no revision is named.
	err = SettleDispatch(root, task, id, d.Revision)
	requireRefusal(t, err, "board.dispatch_conflict", []string{id, "the dispatch is in state", "execution epoch", "the card binds dispatch " + id}, "the request names", "kander dispatch settle")
}

// R20: an ended dispatch no longer binds the card, so a later request at the
// current revision is refused for its state alone.
func TestDispatchRefusalAfterEnd(t *testing.T) {
	english(t)
	root := tempBoard(t)
	s := dispatchCard(t, root, "dispatch-ended")
	d := prepareTestDispatch(t, root, dispatchInput(s, "ended-one"))
	task, id := d.Input.TaskID, d.Input.ID
	if err := EndDispatch(root, task, id, d.Revision, DispatchFailed, "stopped"); err != nil {
		t.Fatal(err)
	}
	ended, err := ReadDispatch(root, task, id)
	if err != nil {
		t.Fatal(err)
	}
	for name, run := range map[string]func() error{
		"end":         func() error { return EndDispatch(root, task, id, ended.Revision, DispatchFailed, "again") },
		"settle":      func() error { return SettleDispatch(root, task, id, ended.Revision) },
		"reauthorize": func() error { _, e := ReauthorizeDispatch(root, task, id, ended.Revision); return e },
	} {
		t.Run(name, func(t *testing.T) {
			err := run()
			requireRefusal(t, err, "board.dispatch_conflict", []string{id, "the dispatch is in state", "the card binds dispatch \"\""}, "the request names")
		})
	}
}

// R20: the wrap-up grant names the stale dispatch revision, and no revision when
// the revision holds and another fact fails.
func TestWrapUpGrantRefusalNamesValues(t *testing.T) {
	english(t)
	root, d, exit := wrapUpGrantFixture(t, true)
	task, id := d.Input.TaskID, d.Input.ID
	_, err := AuthorizeDispatchWrapUp(root, task, id, d.Revision+4, "coordinator", "the owner left", exit)
	requireRefusal(t, err, "board.dispatch_evidence_invalid", []string{"wrap-up authority conflict", "the request names dispatch revision " + num(d.Revision+4), "the dispatch is at revision " + num(d.Revision), "kander dispatch show " + task + " " + id}, "the dispatch is in state")

	_, err = AuthorizeDispatchWrapUp(root, task, id, d.Revision, " ", "the owner left", exit)
	requireRefusal(t, err, "board.dispatch_evidence_invalid", []string{"wrap-up authority conflict", "author and reason must both be given"}, "the request names", "the dispatch is in state")
}

// R20 as a unit: the text of a refused request is built from the request and the
// stored dispatch alone.
func TestDispatchRefusalParts(t *testing.T) {
	english(t)
	d := Dispatch{State: DispatchPrepared, Revision: 4}
	s := Snapshot{Revision: 12, Text: "- DISPATCH_ID: round-1\n- EXECUTION_EPOCH: 2\n"}
	both := strings.Join(dispatchRefusalParts(d, s, 2, []uint64{9}), "|")
	for _, part := range []string{"dispatch revision 2", "is at revision 4", "card revision 9", "is at revision 12"} {
		if !strings.Contains(both, part) {
			t.Fatalf("%q missing from %q", part, both)
		}
	}
	state := strings.Join(dispatchRefusalParts(d, s, 4, []uint64{12}), "|")
	if strings.Contains(state, "the request names") || !strings.Contains(state, "the dispatch is in state") {
		t.Fatal(state)
	}
}

// R14: a stale revision or state of a card keeps the board code and names both values.
func TestStaleTaskRefusal(t *testing.T) {
	english(t)
	root := tempBoard(t)
	id := gateCard(t, root, "stale-task")
	s := transactionSnapshot(t, root, id)
	err := WithTransaction(root, LockScope{Tasks: []string{id}}, func(tx *Transaction) error {
		_, e := tx.Expect(id, "", s.Revision+7)
		return e
	})
	requireRefusal(t, err, "board.transaction_conflict", []string{id, "expected revision " + num(s.Revision+7), "current revision " + num(s.Revision), "kander show --json " + id, "re-read"}, "expected state")

	err = WithTransaction(root, LockScope{Tasks: []string{id}}, func(tx *Transaction) error {
		_, e := tx.Expect(id, "review", s.Revision+7)
		return e
	})
	requireRefusal(t, err, "board.transaction_conflict", []string{"expected revision " + num(s.Revision+7), "expected state review, current state working"})

	err = WithTransaction(root, LockScope{Tasks: []string{id}}, func(tx *Transaction) error {
		_, e := tx.Expect(id, "review", s.Revision)
		return e
	})
	requireRefusal(t, err, "board.transaction_conflict", []string{"expected state review, current state working"}, "expected revision")

	err = WithTransaction(root, LockScope{Tasks: []string{id}}, func(tx *Transaction) error {
		_, e := tx.Expect(id, "working", s.Revision+7)
		return e
	})
	requireRefusal(t, err, "board.transaction_conflict", []string{"expected revision " + num(s.Revision+7), "expected state working, current state working"})
}

// R14: kander move --expect-revision names the revision it expected.
func TestMoveExpectRevisionRefusal(t *testing.T) {
	english(t)
	root := tempBoard(t)
	id := gateCard(t, root, "move-expect")
	s := transactionSnapshot(t, root, id)
	expected := s.Revision + 3
	_, err := MoveWithOptions(s.Entry, root, "review", MoveOptions{ExpectedRevision: &expected})
	requireRefusal(t, err, "board.transaction_conflict", []string{id, "expected revision " + num(expected), "current revision " + num(s.Revision)})
}
