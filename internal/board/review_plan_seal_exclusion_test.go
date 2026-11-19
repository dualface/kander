package board

import (
	"fmt"
	"strings"
	"testing"
)

// sealExits are the two states a started member leaves a plan by.
var sealExits = []string{"archived", "trash"}

const sealRefusal = "seal requires every member assigned"

// sealExit moves a member out of the plan and checks it left an exclusion record.
func sealExit(t *testing.T, root, id, state string) {
	t.Helper()
	deadEndExit(t, root, id, state)
	if _, ok := deadEndExclusion(t, root, id); !ok {
		t.Fatalf("%s has no member exclusion record after the move into %s", id, state)
	}
}

// sealPlan is an unsealed plan whose members are all started group cards. Its
// only batch names the first member and is closed, so the rest are unassigned.
func sealPlan(t *testing.T, slug string, members int) (root, planID string, ids []string) {
	t.Helper()
	root = tempBoard(t)
	for i := 0; i < members; i++ {
		ids = append(ids, gateCard(t, root, fmt.Sprintf("%s-%c", slug, 'a'+i)))
	}
	planID = "plan-" + slug
	p := ReviewPlan{Schema: 1, PlanID: planID, Author: "coordinator", Basis: "fixture", CWD: "/repo", TaskIDs: ids, Batches: []ReviewPlanBatch{
		{BatchID: "first", TaskIDs: ids[:1], Base: sha("a"), TargetCommit: sha("b"), Requirements: noReviewRequirements()},
	}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	if _, err := deadEndClose(t, root, "first", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	return root, planID, ids
}

// sealExtend applies an extension to the current revision of the plan.
func sealExtend(t *testing.T, root, planID string, x ReviewPlanExtension) error {
	t.Helper()
	return tryExtendOwed(root, readOwedPlan(t, root, planID), x)
}

// planHistoryFiles counts the plan-history records of the plan.
func planHistoryFiles(t *testing.T, root, planID string) int {
	t.Helper()
	n := 0
	err := WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
		for revision := 0; revision < 16; revision++ {
			if _, ok, err := tx.ReadGroup(reviewControlGroup, fmt.Sprintf("plan-history/%s/%d.json", planID, revision)); err != nil {
				return err
			} else if ok {
				n++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// requireSealedAndDone checks that the plan is sealed and that the member can
// follow it to done.
func requireSealedAndDone(t *testing.T, root, planID, id string) {
	t.Helper()
	if !readOwedPlan(t, root, planID).Sealed {
		t.Fatal("the plan is not sealed")
	}
	p, err := ReviewTaskProgress(root, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range p.Pending {
		if item == "unsealed-plan" {
			t.Fatalf("progress still waits for the seal: %+v", p)
		}
	}
	if problems := CheckReviewGate(root, []string{id}); len(problems) != 0 {
		t.Fatalf("%v", problems)
	}
	if err := deadEndDone(t, root, id); err != nil {
		t.Fatalf("move done: %v", err)
	}
}

func requireSealRefusal(t *testing.T, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), sealRefusal) {
		t.Fatalf("seal: %v", err)
	}
}

func TestSealSkipsAnExcludedFoundingMember(t *testing.T) {
	for _, state := range sealExits {
		t.Run(state, func(t *testing.T) {
			root, planID, ids := sealPlan(t, "seal-founding-"+state, 2)
			sealExit(t, root, ids[1], state)
			if err := sealExtend(t, root, planID, ReviewPlanExtension{Seal: true}); err != nil {
				t.Fatalf("seal: %v", err)
			}
			requireSealedAndDone(t, root, planID, ids[0])
		})
	}
}

func TestBatchAndSealSkipAnExcludedMember(t *testing.T) {
	root, planID, ids := sealPlan(t, "seal-batch", 2)
	sealExit(t, root, ids[1], "archived")
	batch := ReviewPlanBatch{BatchID: "second", PreviousBatchID: "first", TaskIDs: ids[:1], Base: sha("b"), TargetCommit: sha("c"), Requirements: noReviewRequirements()}
	if err := sealExtend(t, root, planID, ReviewPlanExtension{Batch: &batch, Seal: true}); err != nil {
		t.Fatalf("batch and seal: %v", err)
	}
	if _, err := deadEndClose(t, root, "second", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	requireSealedAndDone(t, root, planID, ids[0])
}

func TestSealSkipsAMemberWhoseBatchWasDropped(t *testing.T) {
	root, planID, ids := sealPlan(t, "seal-dropped", 2)
	second := ReviewPlanBatch{BatchID: "second", PreviousBatchID: "first", TaskIDs: ids[1:], Base: sha("b"), TargetCommit: sha("c"), Requirements: noReviewRequirements()}
	if err := sealExtend(t, root, planID, ReviewPlanExtension{Batch: &second}); err != nil {
		t.Fatal(err)
	}
	sealExit(t, root, ids[1], "archived")
	if err := sealExtend(t, root, planID, ReviewPlanExtension{DropBatches: []string{"second"}}); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := sealExtend(t, root, planID, ReviewPlanExtension{Seal: true}); err != nil {
		t.Fatalf("seal: %v", err)
	}
	requireSealedAndDone(t, root, planID, ids[0])
}

func TestSealStillWaitsForAnUnassignedMember(t *testing.T) {
	root, planID, _ := sealPlan(t, "seal-waits", 2)
	history := planHistoryFiles(t, root, planID)
	before := readOwedPlan(t, root, planID)
	requireSealRefusal(t, sealExtend(t, root, planID, ReviewPlanExtension{Seal: true}))
	if got := readOwedPlan(t, root, planID); got.Revision != before.Revision || got.Sealed {
		t.Fatalf("plan after the refusal: %+v", got)
	}
	if got := planHistoryFiles(t, root, planID); got != history {
		t.Fatalf("plan-history holds %d files, was %d", got, history)
	}
}

func TestSealWaitsForAnUnassignedMemberBesideAnExcludedOne(t *testing.T) {
	root, planID, ids := sealPlan(t, "seal-mixed", 3)
	sealExit(t, root, ids[1], "archived")
	requireSealRefusal(t, sealExtend(t, root, planID, ReviewPlanExtension{Seal: true}))
	if readOwedPlan(t, root, planID).Sealed {
		t.Fatal("the plan is sealed")
	}
}

// A member a batch named before it left was never exempt from the seal; the seal
// reads the same either way.
func TestSealAfterAnAssignedMemberLeaves(t *testing.T) {
	root, planID, ids := sealPlan(t, "seal-assigned", 2)
	second := ReviewPlanBatch{BatchID: "second", PreviousBatchID: "first", TaskIDs: ids[1:], Base: sha("b"), TargetCommit: sha("c"), Requirements: noReviewRequirements()}
	if err := sealExtend(t, root, planID, ReviewPlanExtension{Batch: &second}); err != nil {
		t.Fatal(err)
	}
	if _, err := deadEndClose(t, root, "second", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	sealExit(t, root, ids[1], "archived")
	if err := sealExtend(t, root, planID, ReviewPlanExtension{Seal: true}); err != nil {
		t.Fatalf("seal: %v", err)
	}
	requireSealedAndDone(t, root, planID, ids[0])
}

func TestSealOfAStandaloneSingleMemberPlanIsUnchanged(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "seal-standalone", false)
	id := s.Entry.TaskID
	text, err := setMetadata(readyText(s), FieldOwner, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err = WithTransaction(root, LockScope{Tasks: []string{id}, ExclusiveBoard: true}, func(tx *Transaction) error {
		if err := tx.Put(id, "spec.md", text); err != nil {
			return err
		}
		return tx.Relocate(id, "working")
	}); err != nil {
		t.Fatal(err)
	}
	p := ReviewPlan{Schema: 1, PlanID: "plan-seal-standalone", Author: "coordinator", Basis: "fixture", CWD: "/repo", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{
		{BatchID: "first", TaskIDs: []string{id}, Base: sha("a"), TargetCommit: sha("b"), Requirements: noReviewRequirements()},
	}}
	if err = CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	if _, err = deadEndClose(t, root, "first", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	if err = sealExtend(t, root, p.PlanID, ReviewPlanExtension{Seal: true}); err != nil {
		t.Fatalf("seal: %v", err)
	}
	requireSealedAndDone(t, root, p.PlanID, id)
}
