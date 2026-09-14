package liveness

import (
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
)

func TestSubscriptionUsesRecoveredEpochDeadline(t *testing.T) {
	root, opts, id := factsMember(t)
	setLocation(t, currentEntry(t, root, id).Document, "codex wanted", "herdr:w1:t1:w1:p0")
	d := prepareSubscriptionDispatch(t, root, id, "sync", time.Minute)
	if _, err := board.MoveWithOptions(currentEntry(t, root, id), root, "working", board.MoveOptions{Authorization: d.Authorization}); err != nil {
		t.Fatal(err)
	}
	s, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	d, err = board.ReadDispatch(root, id, d.Input.ID)
	if err != nil {
		t.Fatal(err)
	}
	exit := board.WrapUpExitEvidence{Outcome: "stopped", CardRevision: s.Revision, Session: board.MetadataFrom(s.Text, "SESSION"), Window: board.MetadataFrom(s.Text, "WINDOW"), Owner: board.MetadataFrom(s.Text, "OWNER"), StartedAt: board.MetadataFrom(s.Text, "STARTED_AT"), ObservedAt: time.Now().UTC()}
	next, err := board.ReauthorizeDispatch(root, id, d.Input.ID, d.Revision, exit)
	if err != nil {
		t.Fatal(err)
	}
	oldNow := nowFn
	t.Cleanup(func() { nowFn = oldNow })
	for _, overdue := range []bool{false, true} {
		observed := d.Input.ConfirmBy.Add(time.Second)
		if overdue {
			observed = next.AcceptBefore().Add(time.Second)
		}
		offset := time.Until(observed)
		nowFn = func() time.Time { return time.Now().Add(offset) }
		events, err := factsEvents(t, root, opts, nil)
		if err != nil {
			t.Fatal(err)
		}
		attention := false
		for _, event := range events {
			fact := event.Dispatches[id]
			if !fact.ConfirmBy.Equal(next.AcceptBefore()) || fact.Epoch != next.Authorization.Epoch || fact.ConfirmationOverdue != overdue || !fact.CreatedAt.Equal(d.Input.CreatedAt) || !fact.ConfirmationPending {
				t.Fatalf("wrong epoch summary: %+v", fact)
			}
			attention = attention || event.Event == "dispatch-attention"
		}
		if attention != overdue {
			t.Fatalf("attention=%v, overdue=%v", attention, overdue)
		}
	}
}
