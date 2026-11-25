package board

import (
	"strings"
	"testing"
)

// openPlan is a plan with one batch that is not sealed yet.
func openPlan(t *testing.T, root string, ids []string) ReviewPlan {
	t.Helper()
	p := ReviewPlan{Schema: 1, PlanID: "plan-" + ids[0], Author: "coordinator", Basis: "测试契约明确指定适用角色", CWD: "/repo", ReportLanguage: "en", TaskIDs: ids,
		Batches: []ReviewPlanBatch{{BatchID: "batch", TaskIDs: ids, Base: strings.Repeat("a", 40), TargetCommit: strings.Repeat("b", 40), Requirements: noReviewRequirements()}}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	return p
}

// R4 and R5: an advance names the revision and the targets that differ.
func TestAdvanceRefusals(t *testing.T) {
	english(t)
	root := tempBoard(t)
	id := gateCard(t, root, "advance-refusal")
	gatePlan(t, root, []string{id}, noReviewRequirements())
	registered, final := strings.Repeat("b", 40), strings.Repeat("c", 40)
	advance := func(revision uint64, a ReviewAdvance) error {
		a.Deliveries = map[string]string{final: id}
		return AdvanceReviewBatch(root, ReviewBatchAdvance{BatchID: "batch", ExpectedRevision: revision, Advance: a})
	}

	err := advance(9, ReviewAdvance{PreviousTarget: registered, Target: final, Reason: "member fix"})
	requireRefusal(t, err, "board.review_evidence_invalid", []string{"batch revision CAS conflict", "expected_revision is 9", "batch batch is at revision 1", "kander review aggregate"}, "target_commit")

	stale := strings.Repeat("e", 40)
	err = advance(1, ReviewAdvance{PreviousTarget: stale, Target: final, Reason: "member fix"})
	requireRefusal(t, err, "board.review_evidence_invalid", []string{"batch target CAS conflict", "previous_target is " + stale, "batch batch has target_commit " + registered}, "expected_revision")

	err = advance(1, ReviewAdvance{PreviousTarget: registered, Target: registered, Reason: " "})
	requireRefusal(t, err, "board.review_evidence_invalid", []string{"batch target invalid", "target " + registered + " equals previous_target", "reason must not be empty"}, "but batch batch has target_commit", "CAS")

	err = advance(1, ReviewAdvance{PreviousTarget: registered, Target: "abc", Reason: "member fix"})
	requireRefusal(t, err, "board.review_evidence_invalid", []string{"batch target invalid", "target <invalid> is not a full commit SHA"}, "CAS")

	err = AdvanceReviewBatch(root, ReviewBatchAdvance{BatchID: "missing-batch", ExpectedRevision: 1, Advance: ReviewAdvance{PreviousTarget: registered, Target: final, Reason: "member fix", Deliveries: map[string]string{final: id}}})
	requireRefusal(t, err, "board.review_evidence_invalid", []string{"missing batch: missing-batch"}, "CAS")
}

// R6: a run for another commit than the batch target names what the advance file lacks.
func TestRunAdvanceRefusal(t *testing.T) {
	english(t)
	root := tempBoard(t)
	id := gateCard(t, root, "run-advance-refusal")
	gatePlan(t, root, []string{id}, archiveRequirements())
	first := gateRun(t, root, archiveInput([]string{id}, "first", "PMQA"), emptyFindings())
	next := archiveInput([]string{id}, "second", "PMQA")
	next.Commit = strings.Repeat("c", 40)
	next.ReviewedCommit = first.Commit
	next.PreviousRunID = first.RunID

	_, _, err := PrepareReviewRun(root, next, nil, nil, archiveOriginals(), "test")
	requireRefusal(t, err, "board.review_evidence_invalid", []string{"batch target CAS conflict", "batch batch has target_commit " + first.Commit, "this run reviews " + next.Commit, "no --advance-file"})

	stale := strings.Repeat("e", 40)
	other := strings.Repeat("d", 40)
	_, _, err = PrepareReviewRun(root, next, nil, &ReviewAdvance{PreviousTarget: stale, Target: other, Reason: " ", Deliveries: map[string]string{other: id}}, archiveOriginals(), "test")
	requireRefusal(t, err, "board.review_evidence_invalid", []string{"previous_target is " + stale, "advance target " + other + " differs from the run commit " + next.Commit, "reason must not be empty"})
}

// R8: a plan extension names the revision when only the revision is wrong, and the
// sealed plan when only the plan is sealed.
func TestPlanExtensionRefusals(t *testing.T) {
	english(t)
	t.Run("only the revision", func(t *testing.T) {
		root := tempBoard(t)
		id := gateCard(t, root, "extend-cas")
		p := openPlan(t, root, []string{id})
		err := ExtendReviewPlan(root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: 8, Seal: true, Author: "coordinator", Basis: "seal"})
		requireRefusal(t, err, "board.review_evidence_invalid", []string{"plan extension CAS/sealed conflict", "expected_revision is 8", "plan " + p.PlanID + " is at revision 1", "kander show --document reviews/plan.json"}, "is sealed", "Carrying Conclusions Forward")
	})
	t.Run("only the seal", func(t *testing.T) {
		root := tempBoard(t)
		id := gateCard(t, root, "extend-sealed")
		p := gatePlan(t, root, []string{id}, noReviewRequirements())
		batch := ReviewPlanBatch{BatchID: "second", PreviousBatchID: "batch", TaskIDs: []string{id}, Base: strings.Repeat("b", 40), TargetCommit: strings.Repeat("c", 40), Requirements: noReviewRequirements()}
		err := ExtendReviewPlan(root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: 1, Batch: &batch, Author: "coordinator", Basis: "more"})
		requireRefusal(t, err, "board.review_evidence_invalid", []string{"plan extension CAS/sealed conflict", "plan " + p.PlanID + " is sealed", "rebind_cycles", "release_obligation", "append-cycle-batch", "Next step: plan " + p.PlanID + " stays sealed", "new card", "Carrying Conclusions Forward"}, "expected_revision", "at revision", "kander show --document reviews/plan.json")
	})
	t.Run("a plan that is not there", func(t *testing.T) {
		root := tempBoard(t)
		err := ExtendReviewPlan(root, ReviewPlanExtension{PlanID: "missing-plan", ExpectedRevision: 1, Seal: true, Author: "coordinator", Basis: "seal"})
		requireRefusal(t, err, "board.review_evidence_invalid", []string{"missing plan: missing-plan"}, "CAS")
	})
}

// R11: a sync request names the target it holds and the target the batch has.
func TestSyncTargetRefusal(t *testing.T) {
	english(t)
	root := tempBoard(t)
	id := gateCard(t, root, "sync-refusal")
	p := gatePlan(t, root, []string{id}, noReviewRequirements())
	drifted, wrong := strings.Repeat("c", 40), strings.Repeat("d", 40)
	driftBatchTarget(t, root, "batch", drifted)
	err := ExtendReviewPlan(root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: 1, SyncTargets: map[string]string{"batch": wrong}, Author: "coordinator", Basis: "sync"})
	requireRefusal(t, err, "board.review_evidence_invalid", []string{"sync target CAS requires the current batch target", "sync_targets names " + wrong + " for batch batch", "target_commit is " + drifted, "plan_target and batch_target with kander review progress"})
}

// R12: a resolution of an unplanned batch names a missing plan, and the revision
// of the plan when it is stale.
func TestResolutionEntryRefusals(t *testing.T) {
	english(t)
	root := tempBoard(t)
	id := gateCard(t, root, "resolve-refusal")
	p := gatePlan(t, root, []string{id}, noReviewRequirements())
	resolve := func(plan string, revision uint64) error {
		x := ReviewPlanExtension{PlanID: plan, ExpectedRevision: revision, ResolveBatch: &ReviewBatchResolutionRequest{BatchID: "stray", CoveringBatchID: "batch"}}
		return WithTransaction(root, reviewScope([]string{id}, false), func(tx *Transaction) error {
			_, _, e := resolutionEntry(tx, x, func(ReviewPlan, batchResolutionFacts) error { return nil })
			return e
		})
	}
	requireRefusal(t, resolve("missing-plan", 1), "board.review_evidence_invalid", []string{"missing plan: missing-plan"}, "CAS")
	requireRefusal(t, resolve(p.PlanID, 6), "board.review_evidence_invalid", []string{"plan extension CAS/sealed conflict", "expected_revision is 6", "plan " + p.PlanID + " is at revision 1"})
}

// R13: a disposition whose previous record is not the last of its chain names the
// run, the finding, the task, and both records.
func TestDispositionPredecessorRefusal(t *testing.T) {
	english(t)
	f := newRecordFixture(t, "PMQA")
	f.assign(t)
	if err := f.submit(t, f.recordOf("one", f.a, f.gate, "rejected")); err != nil {
		t.Fatal(err)
	}
	two := f.recordOf("two", f.a, f.gate, "rejected")
	two.PreviousRecordID = "one"
	if err := f.submit(t, two); err != nil {
		t.Fatal(err)
	}
	stale := f.recordOf("three", f.a, f.gate, "rejected")
	stale.PreviousRecordID = "one"
	requireRefusal(t, f.submit(t, stale), "board.review_evidence_invalid", []string{
		"disposition predecessor CAS conflict", "run " + f.run.RunID, "finding " + f.gate.ID, "task " + f.a, "ends at record two", "previous_record_id is one", "kander review aggregate",
	})
	fresh := f.recordOf("four", f.b, f.gate, "rejected")
	fresh.PreviousRecordID = "one"
	requireRefusal(t, f.submit(t, fresh), "board.review_evidence_invalid", []string{"ends at record \"\"", "previous_record_id is one"})
	empty := f.recordOf("five", f.a, f.gate, "rejected")
	requireRefusal(t, f.submit(t, empty), "board.review_evidence_invalid", []string{"ends at record two", "previous_record_id is \"\""})

}

// R10 through the entrance: a rebind request naming a member whose cycle did not
// change is refused with the member and the matching next step.
func TestRebindRequestRefusal(t *testing.T) {
	english(t)
	root := tempBoard(t)
	id := gateCard(t, root, "rebind-refusal")
	p := gatePlan(t, root, []string{id}, noReviewRequirements())
	err := ExtendReviewPlan(root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: 1, RebindCycles: map[string]string{id: "stale-cycle"}, Author: "coordinator", Basis: "rebind"})
	requireRefusal(t, err, "board.review_evidence_invalid", []string{"cycle rebind CAS requires exactly all changed members", "did not change: " + id, "no member's cycle changed"}, "kander review progress <CWD> <task-id> and write")
}
