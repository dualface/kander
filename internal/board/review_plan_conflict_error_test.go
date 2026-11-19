package board

import (
	"strings"
	"testing"
)

// conflictPlan is a plan whose provenance carries values that no refusal may repeat.
func conflictPlan(planID, batchID string, base, target string, members ...string) ReviewPlan {
	requirements := map[string]string{"PMQA": "N/A: reason-token-pmqa", "Security": "N/A: reason-token-security"}
	return ReviewPlan{Schema: 1, PlanID: planID, Author: "author-token", Basis: "basis-token", CWD: "/cwd-token", TaskIDs: members, Batches: []ReviewPlanBatch{
		{BatchID: batchID, TaskIDs: members, Base: base, TargetCommit: target, Requirements: requirements},
	}}
}

// requirePlanIDConflict checks a refusal: it keeps its English prefix, names every part,
// carries the ID hint, and repeats no value a request holds besides IDs and fields.
func requirePlanIDConflict(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("the plan was accepted")
	}
	for _, part := range append(parts, "unique across the whole project") {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("%q missing from %q", part, err)
		}
	}
	for _, secret := range []string{"author-token", "basis-token", "/cwd-token", "reason-token", "N/A:"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("%q written in %q", secret, err)
		}
	}
}

// A plan that reuses the ID of a batch or a plan names that ID, who holds it, and
// the fields in which the new plan differs; what is accepted does not change.
func TestPlanIDConflictsNameTheHolderAndTheFields(t *testing.T) {
	t.Run("a batch held by another plan with another binding", func(t *testing.T) {
		useEnglish(t)
		root := tempBoard(t)
		first, second := gateCard(t, root, "conflict-first"), gateCard(t, root, "conflict-second")
		if err := CreateReviewPlan(root, conflictPlan("plan-one", "shared", sha("a"), sha("b"), first)); err != nil {
			t.Fatal(err)
		}
		err := CreateReviewPlan(root, conflictPlan("plan-two", "shared", sha("a"), sha("c"), second))
		requirePlanIDConflict(t, err, "existing batch/plan mismatch", "batch shared", "plan plan-one (members: "+first+")", "differs from it in: target_commit, task_ids")
	})

	t.Run("a batch of a review run outside any plan", func(t *testing.T) {
		useEnglish(t)
		root := tempBoard(t)
		id := gateCard(t, root, "conflict-stray")
		strayFinished(t, root, id, "stray")
		p := strayAdoptingPlan(id, "plan-adopt")
		p.Batches[0].TargetCommit = sha("d")
		err := CreateReviewPlan(root, p)
		requirePlanIDConflict(t, err, "existing batch/plan mismatch", "batch stray", "a review run outside any plan (members: "+id+")", "differs from it in: target_commit.")
	})

	t.Run("a batch that already belongs to a plan with the same binding", func(t *testing.T) {
		useEnglish(t)
		root := tempBoard(t)
		holder, id := gateCard(t, root, "conflict-holder"), gateCard(t, root, "conflict-planned")
		// The record of a planned batch whose member has no plan pointer: the pointer
		// check comes first on every path the commands write, so the batch check is
		// reached only with a record like this one.
		planned := conflictPlan("plan-new", "planned", sha("a"), sha("b"), id)
		err := WithTransaction(root, reviewScope([]string{holder, id}, false), func(tx *Transaction) error {
			return tx.PutGroup(reviewControlGroup, reviewBatchName("planned"), reviewJSON(ReviewBatch{Schema: 1, BatchID: "planned", PlanID: "plan-old", TaskIDs: []string{id}, Base: sha("a"), TargetCommit: sha("b"), Requirements: planned.Batches[0].Requirements, ReportLanguage: "zh-CN", Revision: 1}))
		})
		if err != nil {
			t.Fatal(err)
		}
		err = CreateReviewPlan(root, planned)
		requirePlanIDConflict(t, err, "batch already planned", "batch planned is already in plan plan-old (members: "+id+")")
	})

	t.Run("a plan ID with another content", func(t *testing.T) {
		useEnglish(t)
		root := tempBoard(t)
		id := gateCard(t, root, "conflict-plan")
		if err := CreateReviewPlan(root, conflictPlan("plan-one", "first", sha("a"), sha("b"), id)); err != nil {
			t.Fatal(err)
		}
		again := conflictPlan("plan-one", "second", sha("a"), sha("b"), id)
		again.Basis = "another-basis-token"
		err := CreateReviewPlan(root, again)
		requirePlanIDConflict(t, err, "immutable review plan conflict", "plan plan-one already exists (members: "+id+")", "differs from it in: basis, batches.")
	})

	t.Run("a member that already has a plan", func(t *testing.T) {
		useEnglish(t)
		root := tempBoard(t)
		id := gateCard(t, root, "conflict-member")
		if err := CreateReviewPlan(root, conflictPlan("plan-one", "first", sha("a"), sha("b"), id)); err != nil {
			t.Fatal(err)
		}
		err := CreateReviewPlan(root, conflictPlan("plan-two", "second", sha("a"), sha("b"), id))
		if err == nil || !strings.Contains(err.Error(), "execution already has a review plan: "+id+" (it belongs to plan plan-one)") {
			t.Fatalf("the member refusal does not name its plan: %v", err)
		}
		for _, secret := range []string{"author-token", "basis-token", "/cwd-token", "reason-token"} {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("%q written in %q", secret, err)
			}
		}
	})
}
