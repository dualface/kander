package board

import (
	"context"
	"slices"
	"testing"
)

func groupSettlement(t *testing.T, root string) GroupEvidenceSettlement {
	t.Helper()
	value, err := GroupEvidenceSettled(context.Background(), root, coordinatorGroup)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// The evidence of a group with a checkpoint stays while a member is still in
// flight, and while a completed wrap-up has no wrap-up Git evidence record:
// the next reconcile verifies that wrap-up in Git once. The reconcile that
// writes the record settles the group.
func TestGroupEvidenceSettlesAfterTheLastWrapUpRecord(t *testing.T) {
	root := tempBoard(t)
	a := coordinatorCard(t, root, "settle-a").Entry.TaskID
	b := coordinatorCard(t, root, "settle-b").Entry.TaskID
	ids := []string{a, b}
	c := coordinatorClaim(t, root, ids...)
	gatePlan(t, root, ids, noReviewRequirements())
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{}); err != nil {
		t.Fatal(err)
	}
	d := prepareTestDispatch(t, root, coordinatorWrapInput(t, root, a, "wrap-settle"))
	verify := func(context.Context, CoordinatorGitFacts) error { return nil }
	reconcile := func() {
		t.Helper()
		var err error
		if c, err = ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), verify); err != nil {
			t.Fatal(err)
		}
		if len(c.BlockedMembers()) != 0 {
			t.Fatalf("%+v", c.Members)
		}
	}
	reconcile()
	for _, state := range []string{"working", "done"} {
		if _, err := dispatchMove(t, root, d, state); err != nil {
			t.Fatal(err)
		}
	}
	if got := groupSettlement(t, root); got.Settled {
		t.Fatalf("a member in flight settled the group: %+v", got)
	}
	if err := deadEndDone(t, root, b); err != nil {
		t.Fatal(err)
	}
	// Both members are done, but the wrap-up of a has no record yet.
	if got := groupSettlement(t, root); got.Settled {
		t.Fatalf("an unrecorded wrap-up settled the group: %+v", got)
	}
	reconcile()
	got := groupSettlement(t, root)
	want := []string{a, b}
	slices.Sort(want)
	if !got.Settled || !slices.Equal(got.Done, want) {
		t.Fatalf("settlement after the record: %+v", got)
	}
}

// When the last record exists before the last member finishes, the move into
// done is the step that settles the group: whichever of the two commits last,
// a read after it sees the group settled.
func TestGroupEvidenceSettlesOnTheLastMoveAfterTheRecords(t *testing.T) {
	root := tempBoard(t)
	a := coordinatorCard(t, root, "order-a").Entry.TaskID
	b := coordinatorCard(t, root, "order-b").Entry.TaskID
	ids := []string{a, b}
	c := coordinatorClaim(t, root, ids...)
	gatePlan(t, root, ids, noReviewRequirements())
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{}); err != nil {
		t.Fatal(err)
	}
	d := prepareTestDispatch(t, root, coordinatorWrapInput(t, root, a, "wrap-order"))
	verify := func(context.Context, CoordinatorGitFacts) error { return nil }
	var err error
	if c, err = ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), verify); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"working", "done"} {
		if _, err := dispatchMove(t, root, d, state); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), verify); err != nil {
		t.Fatal(err)
	}
	if got := groupSettlement(t, root); got.Settled {
		t.Fatalf("a member in flight settled the group: %+v", got)
	}
	if err := deadEndDone(t, root, b); err != nil {
		t.Fatal(err)
	}
	if got := groupSettlement(t, root); !got.Settled || len(got.Done) != 2 {
		t.Fatalf("the last move did not settle the group: %+v", got)
	}
}

// Archived and trash members are finished members, but only done members
// contribute their task evidence. A group without a checkpoint needs no
// wrap-up record.
func TestGroupEvidenceWithoutCheckpointCountsFinishedMembers(t *testing.T) {
	root := tempBoard(t)
	a := coordinatorCard(t, root, "plain-a").Entry.TaskID
	b := coordinatorCard(t, root, "plain-b").Entry.TaskID
	c := coordinatorCard(t, root, "plain-c").Entry.TaskID
	ids := []string{a, b, c}
	gatePlan(t, root, ids, noReviewRequirements())
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{}); err != nil {
		t.Fatal(err)
	}
	if err := deadEndDone(t, root, a); err != nil {
		t.Fatal(err)
	}
	deadEndExit(t, root, b, "archived")
	if got := groupSettlement(t, root); got.Settled {
		t.Fatalf("a member in flight settled the group: %+v", got)
	}
	deadEndExit(t, root, c, "trash")
	if got := groupSettlement(t, root); !got.Settled || !slices.Equal(got.Done, []string{a}) {
		t.Fatalf("settlement: %+v", got)
	}
}

func TestGroupEvidenceOfAnEmptyGroupIsNotSettled(t *testing.T) {
	root := tempBoard(t)
	if got := groupSettlement(t, root); got.Settled || len(got.Done) != 0 {
		t.Fatalf("an empty group settled: %+v", got)
	}
	if _, err := GroupEvidenceSettled(context.Background(), root, "not a group"); err == nil {
		t.Fatal("an invalid group identity was accepted")
	}
}
