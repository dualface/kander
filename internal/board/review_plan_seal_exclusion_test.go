package board

import (
	"fmt"
	"strings"
	"testing"
)

// sealExits are the two states a started member leaves a plan by.
var sealExits = []struct{ state, result string }{
	{"archived", "cancelled"},
	{"trash", "trashed"},
}

func sealExit(t *testing.T, root, id, state, result string) {
	t.Helper()
	moveWith(t, root, id, state, MoveOptions{Result: result, Reason: "the member left the group", Decision: "decision-1"})
	if _, ok := deadEndExclusion(t, root, id); !ok {
		t.Fatalf("%s has no member exclusion record after the move into %s", id, state)
	}
}

// foundingWorld is an unsealed plan that names two started group cards from the
// start while its only batch names the first card, and that batch is closed.
func foundingWorld(t *testing.T, slug string) *membersWorld {
	t.Helper()
	useEnglish(t)
	root := tempBoard(t)
	w := &membersWorld{root: root, first: gateCard(t, root, slug+"-a"), second: gateCard(t, root, slug+"-b")}
	w.candidate = w.second
	p := ReviewPlan{Schema: 1, PlanID: "plan-" + slug, Author: "coordinator", Basis: "fixture", CWD: "/repo", TaskIDs: []string{w.first, w.second}, Batches: []ReviewPlanBatch{
		{BatchID: "first", TaskIDs: []string{w.first}, Base: sha("a"), TargetCommit: sha("b"), Requirements: noReviewRequirements()},
	}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	if _, err := deadEndClose(t, root, "first", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	w.plan.PlanID = p.PlanID
	w.refresh(t)
	return w
}

// addedWorld is the same plan, except the second card joined it by add_members.
func addedWorld(t *testing.T, slug string) *membersWorld {
	t.Helper()
	w := newMembersWorld(t, slug, false)
	if err := w.extend(ReviewPlanExtension{AddMembers: []string{w.second}}); err != nil {
		t.Fatal(err)
	}
	w.refresh(t)
	return w
}

// historyFiles counts the plan-history records of the plan.
func historyFiles(t *testing.T, w *membersWorld) int {
	t.Helper()
	n := 0
	for revision := 0; revision < 16; revision++ {
		if _, ok := w.groupFile(t, fmt.Sprintf("plan-history/%s/%d.json", w.plan.PlanID, revision)); ok {
			n++
		}
	}
	return n
}

// requireSealedAndDone seals with a request of only the seal, then follows the
// remaining member to done.
func requireSealedAndDone(t *testing.T, w *membersWorld) {
	t.Helper()
	if err := w.extend(ReviewPlanExtension{Seal: true}); err != nil {
		t.Fatalf("seal: %v", err)
	}
	w.refresh(t)
	requireSealedPlanFinishes(t, w)
}

func requireSealedPlanFinishes(t *testing.T, w *membersWorld) {
	t.Helper()
	if !w.plan.Sealed {
		t.Fatalf("plan is not sealed: %+v", w.plan)
	}
	p, err := ReviewTaskProgress(w.root, w.first)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range p.Pending {
		if item == "unsealed-plan" {
			t.Fatalf("progress still waits for the seal: %+v", p)
		}
	}
	if problems := CheckReviewGate(w.root, []string{w.first}); len(problems) != 0 {
		t.Fatalf("%v", problems)
	}
	if err := deadEndDone(t, w.root, w.first); err != nil {
		t.Fatalf("move done: %v", err)
	}
}

func TestSealSkipsAnExcludedAddedMember(t *testing.T) {
	for _, exit := range sealExits {
		t.Run(exit.state, func(t *testing.T) {
			w := addedWorld(t, "seal-added-"+exit.state)
			sealExit(t, w.root, w.second, exit.state, exit.result)
			requireSealedAndDone(t, w)
		})
	}
}

func TestSealSkipsAnExcludedFoundingMember(t *testing.T) {
	for _, exit := range sealExits {
		t.Run(exit.state, func(t *testing.T) {
			w := foundingWorld(t, "seal-founding-"+exit.state)
			sealExit(t, w.root, w.second, exit.state, exit.result)
			requireSealedAndDone(t, w)
		})
	}
}

func TestBatchAndSealSkipAnExcludedMember(t *testing.T) {
	w := addedWorld(t, "seal-batch")
	sealExit(t, w.root, w.second, "archived", "cancelled")
	batch := ReviewPlanBatch{BatchID: "second", PreviousBatchID: "first", TaskIDs: []string{w.first}, Base: sha("b"), TargetCommit: sha("c"), Requirements: noReviewRequirements()}
	if err := w.extend(ReviewPlanExtension{Batch: &batch, Seal: true}); err != nil {
		t.Fatalf("batch and seal: %v", err)
	}
	w.refresh(t)
	if _, err := deadEndClose(t, w.root, "second", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	requireSealedPlanFinishes(t, w)
}

func TestSealStillWaitsForAnUnassignedMember(t *testing.T) {
	for name, build := range map[string]func(*testing.T, string) *membersWorld{"founding": foundingWorld, "added": addedWorld} {
		t.Run(name, func(t *testing.T) {
			w := build(t, "seal-waits-"+name)
			history := historyFiles(t, w)
			err := w.extend(ReviewPlanExtension{Seal: true})
			if err == nil || !strings.Contains(err.Error(), "seal requires every member assigned") {
				t.Fatalf("seal with an unassigned member: %v", err)
			}
			if got := readOwedPlan(t, w.root, w.plan.PlanID); got.Revision != w.plan.Revision || got.Sealed {
				t.Fatalf("plan after the refusal: %+v", got)
			}
			if got := historyFiles(t, w); got != history {
				t.Fatalf("plan-history holds %d files, was %d", got, history)
			}
		})
	}
}

func TestSealWaitsForAnUnassignedMemberBesideAnExcludedOne(t *testing.T) {
	useEnglish(t)
	root := tempBoard(t)
	w := &membersWorld{root: root, first: gateCard(t, root, "seal-mixed-a"), second: gateCard(t, root, "seal-mixed-b")}
	third := gateCard(t, root, "seal-mixed-c")
	w.candidate = third
	p := ReviewPlan{Schema: 1, PlanID: "plan-seal-mixed", Author: "coordinator", Basis: "fixture", CWD: "/repo", TaskIDs: []string{w.first, w.second, third}, Batches: []ReviewPlanBatch{
		{BatchID: "first", TaskIDs: []string{w.first}, Base: sha("a"), TargetCommit: sha("b"), Requirements: noReviewRequirements()},
	}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	if _, err := deadEndClose(t, root, "first", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	w.plan.PlanID = p.PlanID
	w.refresh(t)
	sealExit(t, root, w.second, "archived", "cancelled")
	err := w.extend(ReviewPlanExtension{Seal: true})
	if err == nil || !strings.Contains(err.Error(), "seal requires every member assigned") {
		t.Fatalf("seal with an unassigned member beside an excluded one: %v", err)
	}
	if got := readOwedPlan(t, root, w.plan.PlanID); got.Revision != w.plan.Revision || got.Sealed {
		t.Fatalf("plan after the refusal: %+v", got)
	}
}

// A member a batch named before it left was never exempt from the seal; the seal
// reads the same either way.
func TestSealAfterAnAssignedMemberLeaves(t *testing.T) {
	w := addedWorld(t, "seal-assigned")
	batch := ReviewPlanBatch{BatchID: "second", PreviousBatchID: "first", TaskIDs: []string{w.second}, Base: sha("b"), TargetCommit: sha("c"), Requirements: noReviewRequirements()}
	if err := w.extend(ReviewPlanExtension{Batch: &batch}); err != nil {
		t.Fatal(err)
	}
	w.refresh(t)
	if _, err := deadEndClose(t, w.root, "second", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	sealExit(t, w.root, w.second, "archived", "cancelled")
	requireSealedAndDone(t, w)
}

func TestSealOfAStandaloneSingleMemberPlanIsUnchanged(t *testing.T) {
	useEnglish(t)
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
	if err = ExtendReviewPlan(root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: readOwedPlan(t, root, p.PlanID).Revision, Seal: true, Author: "codex", Basis: "the only batch closed"}); err != nil {
		t.Fatalf("seal: %v", err)
	}
	if !readOwedPlan(t, root, p.PlanID).Sealed {
		t.Fatal("the plan is not sealed")
	}
	if err = deadEndDone(t, root, id); err != nil {
		t.Fatalf("move done: %v", err)
	}
}
