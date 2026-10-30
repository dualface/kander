package board

import (
	"context"
	"testing"
)

func reclaimMember(t *testing.T, root, id, decision string) error {
	t.Helper()
	s := transactionSnapshot(t, root, id)
	_, err := MoveWithOptions(s.Entry, root, "working", MoveOptions{Owner: "pi", Decision: decision})
	return err
}

// reviewedMember is a never-dispatched member the checkpoint saw in review.
func reviewedMember(t *testing.T, root, slug string) (string, CoordinatorCheckpoint) {
	t.Helper()
	s := coordinatorTodo(t, root, slug)
	id := s.Entry.TaskID
	if _, err := MoveWithOptions(s.Entry, root, "working", MoveOptions{Owner: "pi"}); err != nil {
		t.Fatal(err)
	}
	c := coordinatorReconcile(t, root, coordinatorClaim(t, root, id))
	gatePlan(t, root, []string{id}, noReviewRequirements())
	moveMember(t, root, id, "review")
	return id, coordinatorReconcile(t, root, c)
}

func rebindPlan(t *testing.T, root, id string) {
	t.Helper()
	p, err := ReviewTaskProgress(root, id)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ReadReviewPlan(root, p.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if err = ExtendReviewPlan(root, ReviewPlanExtension{PlanID: plan.PlanID, ExpectedRevision: plan.Revision, RebindCycles: p.RebindCycles, Author: "coordinator", Basis: "reclaim 后重绑"}); err != nil {
		t.Fatal(err)
	}
}

func TestReclaimFromReviewRequiresDecision(t *testing.T) {
	root := tempBoard(t)
	id, _ := reviewedMember(t, root, "needs-decision")
	before := transactionSnapshot(t, root, id)
	for _, decision := range []string{"", " "} {
		if err := reclaimMember(t, root, id, decision); err == nil {
			t.Fatalf("reclaim accepted decision %q", decision)
		}
	}
	after := transactionSnapshot(t, root, id)
	if after.Revision != before.Revision || after.Entry.State != "review" {
		t.Fatal("refused reclaim had a side effect")
	}
}

func TestReclaimWritesDispatchlessHandoff(t *testing.T) {
	root := tempBoard(t)
	id, c := reviewedMember(t, root, "dispatchless")
	before := transactionSnapshot(t, root, id)
	if err := reclaimMember(t, root, id, "user-ref"); err != nil {
		t.Fatal(err)
	}
	after := transactionSnapshot(t, root, id)
	if err := WithTransaction(root, LockScope{Tasks: []string{id}, ReadOnly: true}, func(tx *Transaction) error {
		chain, err := lifecycleHandoffs(tx, after, planCycle(before))
		if err != nil {
			return err
		}
		want := lifecycleHandoff{Kind: "reclaim", OldCycle: planCycle(before), NewCycle: planCycle(after), Decision: "user-ref", Owner: "pi", CardRevision: before.Revision + 1}
		if len(chain) != 1 || chain[0] != want {
			t.Fatalf("handoff chain: %+v", chain)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rebindPlan(t, root, id)
	c = coordinatorReconcile(t, root, c)
	if got := c.Members[id]; got.Cycle != planCycle(after) || len(got.Handoffs) != 1 {
		t.Fatalf("checkpoint did not follow the reclaim: %+v", got)
	}
	// The group still wraps up after the reclaim.
	moveMember(t, root, id, "review")
	c = coordinatorReconcile(t, root, c)
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{}); err != nil {
		t.Fatal(err)
	}
	d := prepareTestDispatch(t, root, coordinatorWrapInput(t, root, id, "wrap-reclaimed"))
	verify := func(context.Context, CoordinatorGitFacts) error { return nil }
	if _, err := ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), verify); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"working", "done"} {
		if _, err := dispatchMove(t, root, d, state); err != nil {
			t.Fatal(err)
		}
	}
	c, err := ReadCoordinatorCheckpoint(root, coordinatorGroup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), verify); err != nil {
		t.Fatal(err)
	}
}

func TestReclaimRejectsUnreleasedBinding(t *testing.T) {
	root := tempBoard(t)
	id, _ := reviewedMember(t, root, "still-bound")
	s := transactionSnapshot(t, root, id)
	d := prepareTestDispatch(t, root, dispatchInput(s, "completed-sync"))
	if _, err := dispatchMove(t, root, d, "working"); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatchMove(t, root, d, "review"); err != nil {
		t.Fatal(err)
	}
	before := transactionSnapshot(t, root, id)
	if err := reclaimMember(t, root, id, "user-ref"); err == nil {
		t.Fatal("reclaim accepted while a completed dispatch is unreleased")
	}
	if after := transactionSnapshot(t, root, id); after.Revision != before.Revision || after.Entry.State != "review" {
		t.Fatal("refused reclaim had a side effect")
	}
}

// A dispatch cycle ends with a release, so a later cycle without one may still
// hand off although the card holds an older completed dispatch.
func TestDispatchlessHandoffFollowsReleasedCycle(t *testing.T) {
	root := tempBoard(t)
	id, c := reviewedMember(t, root, "released-then-plain")
	s := transactionSnapshot(t, root, id)
	done := prepareTestDispatch(t, root, dispatchInput(s, "older-completed"))
	if _, err := dispatchMove(t, root, done, "working"); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatchMove(t, root, done, "review"); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, id)
	cancelled := prepareTestDispatch(t, root, dispatchInput(s, "older-cancelled"))
	if err := EndDispatch(root, id, cancelled.Input.ID, cancelled.Revision, DispatchCancelled, "释放绑定"); err != nil {
		t.Fatal(err)
	}
	if err := reclaimMember(t, root, id, "first-reclaim"); err != nil {
		t.Fatal(err)
	}
	moveMember(t, root, id, "review")
	if err := reclaimMember(t, root, id, "second-reclaim"); err != nil {
		t.Fatal(err)
	}
	rebindPlan(t, root, id)
	if got := coordinatorReconcile(t, root, c).Members[id]; len(got.Handoffs) != 2 {
		t.Fatalf("handoff chain: %+v", got.Handoffs)
	}
}

func TestLifecycleHandoffsRejectUnprovenDispatchlessRecords(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(t *testing.T, root, id string)
		change  func(*lifecycleHandoff)
	}{
		{name: "blank decision", change: func(h *lifecycleHandoff) { h.Decision = " " }},
		{name: "revision not advancing", change: func(h *lifecycleHandoff) { h.CardRevision = 0 }},
		{name: "old cycle has a release", prepare: func(t *testing.T, root, id string) {
			s := transactionSnapshot(t, root, id)
			d := prepareTestDispatch(t, root, dispatchInput(s, "released"))
			if err := EndDispatch(root, id, d.Input.ID, d.Revision, DispatchCancelled, "释放绑定"); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "old cycle holds a completed dispatch", prepare: func(t *testing.T, root, id string) {
			s := transactionSnapshot(t, root, id)
			d := prepareTestDispatch(t, root, dispatchInput(s, "unreleased"))
			if _, err := dispatchMove(t, root, d, "working"); err != nil {
				t.Fatal(err)
			}
			if _, err := dispatchMove(t, root, d, "review"); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := tempBoard(t)
			id, _ := reviewedMember(t, root, "forged")
			if tc.prepare != nil {
				tc.prepare(t, root, id)
			}
			s := transactionSnapshot(t, root, id)
			from := planCycle(s)
			text, err := NewClaimMetadata(s.Text)
			if err != nil {
				t.Fatal(err)
			}
			to := planCycle(Snapshot{Entry: s.Entry, Text: text})
			h := lifecycleHandoff{Kind: "reclaim", OldCycle: from, NewCycle: to, Decision: "forged", Owner: "pi", CardRevision: s.Revision + 1}
			if tc.change != nil {
				tc.change(&h)
			}
			text = appendLifecycleRecord(text, "LIFECYCLE_DECISION", lifecycleJSON(h))
			if err = WithTransaction(root, LockScope{Tasks: []string{id}}, func(tx *Transaction) error { return tx.Put(id, "spec.md", text) }); err != nil {
				t.Fatal(err)
			}
			s = transactionSnapshot(t, root, id)
			if err = WithTransaction(root, LockScope{Tasks: []string{id}, ReadOnly: true}, func(tx *Transaction) error {
				_, e := lifecycleHandoffs(tx, s, from)
				return e
			}); err == nil {
				t.Fatal("unproven dispatchless handoff accepted")
			}
		})
	}
}
