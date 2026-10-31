package liveness

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/board"
)

// watchedGroupBoard builds a board with one subscription member, one watched
// group holding a card, and one backlog card whose group ownership is broken in
// the given way.
func watchedGroupBoard(t *testing.T, brokenMetadata string) (string, subscribeOptions, string) {
	t.Helper()
	root, opts, _ := factsMember(t)
	watched := "20260908-watched-group"
	_, path := makeWorking(t, "scope-watched", "Watched")
	setTaskGroup(t, path, watched)
	opts.Watch = []string{watched}

	if _, err := board.NewTask(root, "chore", "scope-broken", "Broken", "en", false); err != nil {
		t.Fatal(err)
	}
	setMeta(t, filepath.Join(root, "backlog", todayID("scope-broken"), "spec.md"), "- TASK_GROUP:\n", brokenMetadata)
	return root, opts, watched
}

func TestReadContextIgnoresCardsOutsideTheWatchedGroups(t *testing.T) {
	root, opts, watched := watchedGroupBoard(t, "- TASK_GROUP: not a group id\n")
	scanned, err := board.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if scanned.GroupMembership().ErrFor() == nil {
		t.Fatal("the card under test is not actually broken")
	}
	s, err := newSubscription(opts)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := s.readContext(context.Background(), root)
	if err != nil {
		t.Fatalf("an unrelated broken card blocked the subscription: %v", err)
	}
	if len(facts.groups[watched]) != 1 {
		t.Fatalf("watched members changed under narrowing: %+v", facts.groups)
	}
}

func TestReadContextStillBlocksOnOwnershipItCannotRuleOut(t *testing.T) {
	root, opts, watched := watchedGroupBoard(t, "- TASK_GROUP: 20260908-other-group\n- TASK_GROUP: "+"20260908-watched-group"+"\n")
	s, err := newSubscription(opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.readContext(context.Background(), root); err == nil {
		t.Fatalf("ownership that could hide a member of %s did not block", watched)
	}
}

func TestSubscriptionShowsBlockedMembers(t *testing.T) {
	root, opts, _ := factsMember(t)
	id := opts.Members[0]
	s, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	c, err := board.ClaimCoordinator(context.Background(), root, board.CoordinatorClaim{GroupID: opts.Group, Owner: "coordinator", Token: "session-one", Basis: "fixture", Members: []string{id}})
	if err != nil {
		t.Fatal(err)
	}
	opts.Refresh = 0.02
	events := make(runtimeEvents, 64)
	cancel, _ := runRuntimeSubscription(t, root, opts, events)
	defer cancel()
	nextRuntimeEvent(t, events, func(e groupEvent) bool { return e.Event == "snapshot" && len(e.Blocked) == 0 })
	r := board.CoordinatorReconcile{GroupID: c.GroupID, ExpectedRevision: c.Revision, Authority: c.Authority, Members: map[string]board.CoordinatorObservation{id: {Revision: s.Revision, DispatchID: "stale-round", Epoch: 1}}}
	c, _ = board.ReconcileCoordinator(context.Background(), root, r, nil)
	if c.Members[id].Blocked == nil {
		t.Fatalf("no block: %+v", c.Members[id])
	}
	event := nextRuntimeEvent(t, events, func(e groupEvent) bool { return e.Event == "member-blocked" })
	if event.Blocked[id] != c.Members[id].Blocked.Reason {
		t.Fatalf("reason missing: %+v", event.Blocked)
	}
	r = board.CoordinatorReconcile{GroupID: c.GroupID, ExpectedRevision: c.Revision, Authority: c.Authority, Members: map[string]board.CoordinatorObservation{id: {Revision: s.Revision}}}
	if _, err = board.ReconcileCoordinator(context.Background(), root, r, nil); err != nil {
		t.Fatal(err)
	}
	nextRuntimeEvent(t, events, func(e groupEvent) bool { return e.Event == "member-blocked" && len(e.Blocked) == 0 })
}
