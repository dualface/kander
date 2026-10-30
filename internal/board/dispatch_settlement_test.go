package board

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func completedRound(t *testing.T, root, id, name string) Dispatch {
	t.Helper()
	s := transactionSnapshot(t, root, id)
	d := prepareTestDispatch(t, root, dispatchInput(s, name))
	if _, err := dispatchMove(t, root, d, "working"); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatchMove(t, root, d, "review"); err != nil {
		t.Fatal(err)
	}
	read, err := ReadDispatch(root, id, name)
	if err != nil {
		t.Fatal(err)
	}
	return read
}

func TestPrepareRecordsCycleAndPredecessor(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "round-chain")
	id := s.Entry.TaskID
	first := completedRound(t, root, id, "round-one")
	if first.Cycle != planCycle(s) || first.PreviousID != "" || first.Schema != dispatchSchema {
		t.Fatalf("first %+v", first)
	}
	second := prepareTestDispatch(t, root, dispatchInput(transactionSnapshot(t, root, id), "round-two"))
	if second.PreviousID != first.Input.ID {
		t.Fatalf("second %+v", second)
	}
}

func TestDispatchTransitionsRefuseAnotherCycle(t *testing.T) {
	stale := func(t *testing.T, root, id string, d Dispatch) {
		t.Helper()
		path := filepath.Join(transactionSnapshot(t, root, id).Entry.Path, "dispatches", d.Input.ID)
		for _, name := range []string{"state.json", "intent.json"} {
			data, err := os.ReadFile(filepath.Join(path, name))
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(path, name), []byte(strings.ReplaceAll(string(data), d.Cycle, "earlier-cycle")), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	changed := func(t *testing.T, err error) {
		t.Helper()
		var e *Error
		if !errors.As(err, &e) || e.Code != "board.dispatch_cycle_changed" {
			t.Fatalf("err=%v", err)
		}
	}
	t.Run("accept", func(t *testing.T) {
		root := tempBoard(t)
		s := dispatchCard(t, root, "cycle-accept")
		d := prepareTestDispatch(t, root, dispatchInput(s, "stale-round"))
		stale(t, root, s.Entry.TaskID, d)
		_, err := dispatchMove(t, root, d, "working")
		changed(t, err)
	})
	t.Run("end", func(t *testing.T) {
		root := tempBoard(t)
		s := dispatchCard(t, root, "cycle-end")
		d := prepareTestDispatch(t, root, dispatchInput(s, "stale-round"))
		stale(t, root, s.Entry.TaskID, d)
		changed(t, EndDispatch(root, s.Entry.TaskID, d.Input.ID, d.Revision, DispatchCancelled, "stop"))
	})
	t.Run("settle", func(t *testing.T) {
		root := tempBoard(t)
		s := dispatchCard(t, root, "cycle-settle")
		d := completedRound(t, root, s.Entry.TaskID, "stale-round")
		stale(t, root, s.Entry.TaskID, d)
		changed(t, SettleDispatch(root, s.Entry.TaskID, d.Input.ID, d.Revision))
	})
}

func TestReleaseCycleEqualsDispatchCycle(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "release-cycle")
	d := prepareTestDispatch(t, root, dispatchInput(s, "released-round"))
	if err := EndDispatch(root, s.Entry.TaskID, d.Input.ID, d.Revision, DispatchCancelled, "stop"); err != nil {
		t.Fatal(err)
	}
	read, err := ReadDispatch(root, s.Entry.TaskID, d.Input.ID)
	if err != nil || read.Release.Cycle != read.Cycle {
		t.Fatalf("release %+v %v", read.Release, err)
	}
}

// A dispatch in flight across the upgrade has no cycle and must not become a dead end.
func TestSchemaOneDispatchStaysUsable(t *testing.T) {
	downgrade := func(t *testing.T, root, id string, d Dispatch) {
		t.Helper()
		path := filepath.Join(transactionSnapshot(t, root, id).Entry.Path, "dispatches", d.Input.ID)
		for _, name := range []string{"state.json", "intent.json"} {
			data, err := os.ReadFile(filepath.Join(path, name))
			if err != nil {
				t.Fatal(err)
			}
			text := strings.Replace(string(data), `"schema":2`, `"schema":1`, 1)
			text = strings.Replace(text, `"cycle":"`+d.Cycle+`",`, "", 1)
			if err = os.WriteFile(filepath.Join(path, name), []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	root := tempBoard(t)
	s := dispatchCard(t, root, "legacy-round")
	id := s.Entry.TaskID
	d := prepareTestDispatch(t, root, dispatchInput(s, "legacy-accept"))
	downgrade(t, root, id, d)
	if _, err := dispatchMove(t, root, d, "working"); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatchMove(t, root, d, "review"); err != nil {
		t.Fatal(err)
	}
	read, err := ReadDispatch(root, id, d.Input.ID)
	if err != nil || read.Cycle != "" {
		t.Fatalf("read %+v %v", read, err)
	}
	if err = SettleDispatch(root, id, d.Input.ID, read.Revision); err != nil {
		t.Fatal(err)
	}
	next := prepareTestDispatch(t, root, dispatchInput(transactionSnapshot(t, root, id), "legacy-end"))
	downgrade(t, root, id, next)
	if err = EndDispatch(root, id, next.Input.ID, next.Revision, DispatchCancelled, "stop"); err != nil {
		t.Fatal(err)
	}
	third := prepareTestDispatch(t, root, dispatchInput(transactionSnapshot(t, root, id), "after-legacy"))
	if third.PreviousID != next.Input.ID {
		t.Fatalf("third %+v", third)
	}
}

func TestLegacyDispatchOrderIsNotGuessed(t *testing.T) {
	revision := func(id string, n uint64) Dispatch {
		return Dispatch{Schema: 1, Input: DispatchInput{ID: id}, Accepted: &DispatchReceipt{CardRevision: n}}
	}
	ordered, err := orderLegacyDispatches("task", []Dispatch{revision("second", 9), revision("first", 3)})
	if err != nil || ordered[0].Input.ID != "first" {
		t.Fatalf("ordered %+v %v", ordered, err)
	}
	if _, err = orderLegacyDispatches("task", []Dispatch{revision("a", 3), revision("b", 3)}); err == nil {
		t.Fatal("equal receipts got an order")
	}
	silent := Dispatch{Schema: 1, Input: DispatchInput{ID: "silent"}}
	other := silent
	other.Input.ID = "other"
	if _, err = orderLegacyDispatches("task", []Dispatch{silent, other}); err == nil {
		t.Fatal("two rounds without receipts got an order")
	}
}

func TestSettleEndsACompletedBinding(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "settle-binding")
	id := s.Entry.TaskID
	prepared := prepareTestDispatch(t, root, dispatchInput(s, "not-done"))
	if err := SettleDispatch(root, id, prepared.Input.ID, prepared.Revision); err == nil {
		t.Fatal("settled a round that did not complete")
	}
	if err := EndDispatch(root, id, prepared.Input.ID, prepared.Revision, DispatchCancelled, "stop"); err != nil {
		t.Fatal(err)
	}
	d := completedRound(t, root, id, "settled-round")
	if err := SettleDispatch(root, id, d.Input.ID, d.Revision); err != nil {
		t.Fatal(err)
	}
	after := transactionSnapshot(t, root, id)
	if authFrom(after.Text) != (ExecutionAuthorization{}) {
		t.Fatal("settlement left the authorization")
	}
	read, err := ReadDispatch(root, id, d.Input.ID)
	if err != nil || read.State != DispatchCompleted || read.Settlement == nil || read.Completed.DeliveryCommit != d.Completed.DeliveryCommit {
		t.Fatalf("read %+v %v", read, err)
	}
	if _, err := os.Stat(filepath.Join(after.Entry.Path, "dispatches", d.Input.ID, "settlement.json")); err != nil {
		t.Fatal(err)
	}
	if err := SettleDispatch(root, id, d.Input.ID, read.Revision); err == nil {
		t.Fatal("settled twice")
	}
}

func TestArchiveAndTrashSettleACompletedBinding(t *testing.T) {
	for _, target := range []string{"archived", "trash"} {
		t.Run(target, func(t *testing.T) {
			root := tempBoard(t)
			s := dispatchCard(t, root, "terminal-settle")
			id := s.Entry.TaskID
			d := completedRound(t, root, id, "terminal-round")
			current := transactionSnapshot(t, root, id)
			result := map[string]string{"archived": "cancelled", "trash": "trashed"}[target]
			moved, err := MoveWithOptions(current.Entry, root, target, MoveOptions{Result: result, Reason: "stop", Decision: "user"})
			if err != nil {
				t.Fatal(err)
			}
			text, err := ReadDocument(moved)
			if err != nil || authFrom(text) != (ExecutionAuthorization{}) {
				t.Fatalf("binding kept: %v", err)
			}
			if _, err = os.Stat(filepath.Join(moved.Path, "dispatches", d.Input.ID, "settlement.json")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReclaimFollowsASettlement(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "settled-reclaim")
	id := s.Entry.TaskID
	d := completedRound(t, root, id, "reclaim-round")
	if err := SettleDispatch(root, id, d.Input.ID, d.Revision); err != nil {
		t.Fatal(err)
	}
	current := transactionSnapshot(t, root, id)
	if _, err := MoveWithOptions(current.Entry, root, "working", MoveOptions{Owner: "claude"}); err == nil {
		t.Fatal("reclaim without a decision passed")
	}
	if _, err := MoveWithOptions(current.Entry, root, "working", MoveOptions{Owner: "claude", Decision: "user"}); err != nil {
		t.Fatal(err)
	}
	if cur := ledgerOf(t, root, id).current; cur.Kind != ExecutionReclaim {
		t.Fatalf("record %+v", cur)
	}
}

func TestEndingAnAcceptedRoundNeedsADecision(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "accepted-end")
	id := s.Entry.TaskID
	d := prepareTestDispatch(t, root, dispatchInput(s, "accepted-round"))
	if _, err := dispatchMove(t, root, d, "working"); err != nil {
		t.Fatal(err)
	}
	read, err := ReadDispatch(root, id, d.Input.ID)
	if err != nil {
		t.Fatal(err)
	}
	var e *Error
	if err = EndDispatch(root, id, d.Input.ID, read.Revision, DispatchFailed, "stop"); !errors.As(err, &e) || e.Code != "board.dispatch_end_decision_required" {
		t.Fatalf("err=%v", err)
	}
	if err = EndDispatch(root, id, d.Input.ID, read.Revision, DispatchFailed, "stop", "user"); err != nil {
		t.Fatal(err)
	}
}
