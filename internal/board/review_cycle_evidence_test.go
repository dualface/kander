package board

import (
	"slices"
	"strings"
	"testing"
)

// staleUnplannedRun is the start of path 2: a member of a group has a passing run in
// a batch of no plan, and a reset gives it a new execution cycle.
func staleUnplannedRun(t *testing.T, root string) (string, ReviewRun) {
	t.Helper()
	ids := []string{recoveryMember(t, root, "stale-a"), recoveryMember(t, root, "stale-b")}
	c := coordinatorReconcile(t, root, coordinatorClaim(t, root, ids...))
	run := preparedRun(t, root, archiveInput(ids[:1], "old-run", "PMQA"), archiveRequirements(), nil, false)
	staleReclaim(t, root, ids[0])
	recoverMember(t, root, coordinatorRequestAfter(t, root, c), ids[0])
	return ids[0], run
}

// preparedRun publishes one successful run. strip leaves it without a cycle binding,
// as a run an older binary wrote.
func preparedRun(t *testing.T, root string, in ReviewInput, requirements map[string]string, advance *ReviewAdvance, strip bool) ReviewRun {
	t.Helper()
	run, _, err := PrepareReviewRun(root, in, requirements, advance, archiveOriginals(), "test")
	if err != nil {
		t.Fatal(err)
	}
	run.LaunchStatus, run.ExecutionStatus = "started", "ok"
	if strip {
		run.MemberCycles = nil
	}
	if run, err = FinalizeReviewRun(root, run, structuredReport(emptyFindings())); err != nil {
		t.Fatal(err)
	}
	return publishRun(t, root, run.RunID)
}

func adoptingPlan(t *testing.T, root, id string, requirements map[string]string) ReviewPlan {
	t.Helper()
	p := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-" + id, Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{{BatchID: "batch", TaskIDs: []string{id}, Base: owedCommit(0), TargetCommit: owedCommit(1), Requirements: requirements}}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	return readOwedPlan(t, root, p.PlanID)
}

func requireStaleRefusal(t *testing.T, root, id string, run ReviewRun) {
	t.Helper()
	assignGate(t, root, run, map[string][]string{})
	_, err := deadEndClose(t, root, "batch", map[string]ReviewRoleConclusion{"PMQA": passRole(run)}, nil)
	if err == nil || !strings.Contains(err.Error(), "PMQA") || !strings.Contains(err.Error(), id) || !strings.Contains(err.Error(), "superseded_runs") {
		t.Fatalf("the run of the old cycle closed the batch: %v", err)
	}
	err = deadEndDone(t, root, id)
	if err == nil || !strings.Contains(err.Error(), "stale-run:batch/PMQA/"+id) {
		t.Fatalf("the old cycle passed done: %v", err)
	}
	progress, err := ReviewTaskProgress(root, id)
	if err != nil || !slices.ContainsFunc(progress.NextActions, func(a ReviewNextAction) bool {
		return a.Action == "rerun-current-cycle" && a.Role == "PMQA" && a.TaskID == id && a.BatchID == "batch"
	}) {
		t.Fatalf("%+v %v", progress, err)
	}
}

func closeWithCurrentRoot(t *testing.T, root, id string, old ReviewRun) {
	t.Helper()
	next := preparedRun(t, root, archiveInput([]string{id}, "new-run", "PMQA"), nil, nil, false)
	assignGate(t, root, next, map[string][]string{})
	superseded := map[string]ReviewSupersededRun{old.RunID: {Replacement: next.RunID, Basis: "parallel run of the old cycle"}}
	if _, err := deadEndClose(t, root, "batch", map[string]ReviewRoleConclusion{"PMQA": passRole(next)}, superseded); err != nil {
		t.Fatal(err)
	}
}

func TestRunRecordsTheCycleOfEachMember(t *testing.T) {
	root := tempBoard(t)
	ids := []string{recoveryMember(t, root, "bound-a"), recoveryMember(t, root, "bound-b")}
	run := preparedRun(t, root, archiveInput(ids, "bound", "PMQA"), archiveRequirements(), nil, false)
	stored, err := ReadReviewRun(root, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if stored.MemberCycles[id] != planCycle(transactionSnapshot(t, root, id)) {
			t.Fatalf("%s: %+v", id, stored.MemberCycles)
		}
	}
}

func TestOldRunCannotCloseAnAdoptedBatchAfterAReset(t *testing.T) {
	root := tempBoard(t)
	id, run := staleUnplannedRun(t, root)
	adoptingPlan(t, root, id, archiveRequirements())
	requireStaleRefusal(t, root, id, run)
}

func TestOldRunCannotCloseAnAdoptedBatchAfterAReclaim(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "adopted-reclaim")
	run := preparedRun(t, root, archiveInput([]string{id}, "old-run", "PMQA"), archiveRequirements(), nil, false)
	deadEndReclaim(t, root, id)
	adoptingPlan(t, root, id, archiveRequirements())
	requireStaleRefusal(t, root, id, run)
	closeWithCurrentRoot(t, root, id, run)
	if err := deadEndDone(t, root, id); err != nil {
		t.Fatal(err)
	}
}

func TestAdoptedBatchClosesOnACurrentRunOfANewCommit(t *testing.T) {
	root := tempBoard(t)
	id, old := staleUnplannedRun(t, root)
	adoptingPlan(t, root, id, archiveRequirements())
	assignGate(t, root, old, map[string][]string{})
	in := archiveInput([]string{id}, "new-run", "PMQA")
	in.Commit, in.PreviousRunID, in.ReviewedCommit = owedCommit(2), old.RunID, old.Commit
	advance := &ReviewAdvance{PreviousTarget: old.Commit, Target: in.Commit, Reason: "new cycle delivery", Deliveries: map[string]string{in.Commit: id}}
	next := preparedRun(t, root, in, nil, advance, false)
	assignGate(t, root, next, map[string][]string{})
	if _, err := deadEndClose(t, root, "batch", map[string]ReviewRoleConclusion{"PMQA": passRole(next)}, nil); err != nil {
		t.Fatal(err)
	}
	if err := deadEndDone(t, root, id); err != nil {
		t.Fatal(err)
	}
}

func TestAdoptedBatchClosesOnACurrentRunOfTheSameCommit(t *testing.T) {
	root := tempBoard(t)
	id, old := staleUnplannedRun(t, root)
	adoptingPlan(t, root, id, archiveRequirements())
	assignGate(t, root, old, map[string][]string{})
	closeWithCurrentRoot(t, root, id, old)
	if err := deadEndDone(t, root, id); err != nil {
		t.Fatal(err)
	}
}

func TestOldRunOfAnOpenBatchCannotPassARebind(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "open-old-run")
	gatePlan(t, root, []string{id}, archiveRequirements())
	run := gateRun(t, root, archiveInput([]string{id}, "old-run", "PMQA"), emptyFindings())
	deadEndReclaim(t, root, id)
	deadEndRebind(t, root, id)
	requireStaleRefusal(t, root, id, run)
	closeWithCurrentRoot(t, root, id, run)
	if err := deadEndDone(t, root, id); err != nil {
		t.Fatal(err)
	}
}

func TestUnboundRunStaysEvidenceWhileTheCycleIsUnchanged(t *testing.T) {
	root := tempBoard(t)
	id := recoveryMember(t, root, "unbound-same")
	gatePlan(t, root, []string{id}, archiveRequirements())
	run := preparedRun(t, root, archiveInput([]string{id}, "unbound", "PMQA"), nil, nil, true)
	if run.MemberCycles != nil {
		t.Fatalf("%+v", run.MemberCycles)
	}
	assignGate(t, root, run, map[string][]string{})
	if _, err := deadEndClose(t, root, "batch", map[string]ReviewRoleConclusion{"PMQA": passRole(run)}, nil); err != nil {
		t.Fatal(err)
	}
	if err := deadEndDone(t, root, id); err != nil {
		t.Fatal(err)
	}
}

func TestUnboundRunIsNoEvidenceAfterACycleChange(t *testing.T) {
	for _, adopted := range []bool{false, true} {
		root := tempBoard(t)
		id := recoveryMember(t, root, "unbound-changed")
		if adopted {
			run := preparedRun(t, root, archiveInput([]string{id}, "unbound", "PMQA"), archiveRequirements(), nil, true)
			deadEndReclaim(t, root, id)
			adoptingPlan(t, root, id, archiveRequirements())
			requireStaleRefusal(t, root, id, run)
			continue
		}
		gatePlan(t, root, []string{id}, archiveRequirements())
		run := preparedRun(t, root, archiveInput([]string{id}, "unbound", "PMQA"), nil, nil, true)
		deadEndReclaim(t, root, id)
		deadEndRebind(t, root, id)
		requireStaleRefusal(t, root, id, run)
	}
}

func TestClosedBatchKeepsItsRunsAfterTheCycleChanges(t *testing.T) {
	for name, strip := range map[string]bool{"bound": false, "unbound": true} {
		t.Run(name, func(t *testing.T) {
			root := tempBoard(t)
			id := recoveryMember(t, root, "closed-keeps")
			gatePlan(t, root, []string{id}, archiveRequirements())
			run := preparedRun(t, root, archiveInput([]string{id}, "kept", "PMQA"), nil, nil, strip)
			assignGate(t, root, run, map[string][]string{})
			if _, err := deadEndClose(t, root, "batch", map[string]ReviewRoleConclusion{"PMQA": passRole(run)}, nil); err != nil {
				t.Fatal(err)
			}
			deadEndReclaim(t, root, id)
			deadEndRebind(t, root, id)
			progress, err := ReviewTaskProgress(root, id)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(progress.Pending, "cycle-batch:"+id) || slices.ContainsFunc(progress.Pending, func(item string) bool { return strings.HasPrefix(item, "stale-run:") }) {
				t.Fatalf("%+v", progress.Pending)
			}
		})
	}
}

// recoveredWithoutPlan resets a member that has no plan and no run.
func recoveredWithoutPlan(t *testing.T, root string) string {
	t.Helper()
	ids, c := blockedGroup(t, root)
	recoverMember(t, root, c, ids[0])
	return ids[0]
}

func recoveryRecord(t *testing.T, root, id string) (o RecoveryReview, ok bool) {
	t.Helper()
	err := WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
		var e error
		ok, e = readReviewJSON(tx, recoveryReviewName(id), &o)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	return o, ok
}

func TestResetWithNoPlanLeavesARecovery(t *testing.T) {
	root := tempBoard(t)
	id := recoveredWithoutPlan(t, root)
	if o, ok := recoveryRecord(t, root, id); !ok || o.PlanID != "" || o.RecoveryID == "" {
		t.Fatalf("%+v %v", o, ok)
	}
}

func TestAllNAPlanAfterAResetNeedsARecordedDecision(t *testing.T) {
	root := tempBoard(t)
	id := recoveredWithoutPlan(t, root)
	p := adoptingPlan(t, root, id, noReviewRequirements())
	if _, err := deadEndClose(t, root, "batch", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := deadEndDone(t, root, id); err == nil || !strings.Contains(err.Error(), "recovery-review:"+id) || !strings.Contains(err.Error(), "release_obligation") {
		t.Fatalf("an all N/A plan released the recovery: %v", err)
	}
	release := &ReviewObligationRelease{Kind: releaseRecovery, TaskIDs: []string{id}}
	if err := tryExtendOwed(root, p, ReviewPlanExtension{ReleaseObligation: release, Decision: " "}); err == nil {
		t.Fatal("a blank decision released the recovery")
	}
	p = extendOwed(t, root, p, ReviewPlanExtension{ReleaseObligation: release, Decision: "user-decision-3"})
	if got := historyDecision(t, root, p); got != "user-decision-3" {
		t.Fatalf("history %q", got)
	}
	if err := deadEndDone(t, root, id); err != nil {
		t.Fatal(err)
	}
}

func TestPlanWithARequiredRoleTakesTheReviewOfAReset(t *testing.T) {
	root := tempBoard(t)
	id := recoveredWithoutPlan(t, root)
	a, b, c := owedCommit(0), owedCommit(1), owedCommit(2)
	plan := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-" + id, Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{
		{BatchID: "batch", TaskIDs: []string{id}, Base: a, TargetCommit: b, Requirements: noReviewRequirements()},
		{BatchID: "second", PreviousBatchID: "batch", TaskIDs: []string{id}, Base: b, TargetCommit: c, Requirements: archiveRequirements()},
	}}
	if err := CreateReviewPlan(root, plan); err != nil {
		t.Fatal(err)
	}
	p := readOwedPlan(t, root, plan.PlanID)
	if _, err := deadEndClose(t, root, "batch", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	progress, err := ReviewTaskProgress(root, id)
	if err != nil || !slices.Contains(progress.Pending, "recovery-review:"+id) {
		t.Fatalf("an N/A batch discharged a required role: %+v %v", progress, err)
	}
	release := &ReviewObligationRelease{Kind: releaseRecovery, TaskIDs: []string{id}}
	if err = tryExtendOwed(root, p, ReviewPlanExtension{ReleaseObligation: release, Decision: "user-decision"}); err == nil || !strings.Contains(err.Error(), "PMQA") {
		t.Fatalf("a decision replaced a required role: %v", err)
	}
	closeOwedBatch(t, root, p.Batches[1])
	if err = deadEndDone(t, root, id); err != nil {
		t.Fatal(err)
	}
}

func TestReviewOfAnEarlierCycleDoesNotDischargeAReset(t *testing.T) {
	root := tempBoard(t)
	id := recoveredWithoutPlan(t, root)
	p := adoptingPlan(t, root, id, archiveRequirements())
	closeOwedBatch(t, root, p.Batches[0])
	deadEndReclaim(t, root, id)
	deadEndRebind(t, root, id)
	progress, err := ReviewTaskProgress(root, id)
	if err != nil || !slices.Contains(progress.Pending, "recovery-review:"+id) {
		t.Fatalf("a conclusion of the earlier cycle discharged the recovery: %+v %v", progress, err)
	}
}

func TestDroppedRoleStaysOnARecoveryWithNoPlan(t *testing.T) {
	root := tempBoard(t)
	id := recoveredWithoutPlan(t, root)
	a, b, c := owedCommit(0), owedCommit(1), owedCommit(2)
	plan := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-" + id, Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{
		{BatchID: "batch", TaskIDs: []string{id}, Base: a, TargetCommit: b, Requirements: noReviewRequirements()},
		{BatchID: "second", PreviousBatchID: "batch", TaskIDs: []string{id}, Base: b, TargetCommit: c, Requirements: archiveRequirements()},
	}}
	if err := CreateReviewPlan(root, plan); err != nil {
		t.Fatal(err)
	}
	p := extendOwed(t, root, readOwedPlan(t, root, plan.PlanID), ReviewPlanExtension{DropBatches: []string{"second"}, Decision: "user-decision"})
	release := &ReviewObligationRelease{Kind: releaseRecovery, TaskIDs: []string{id}}
	if err := tryExtendOwed(root, p, ReviewPlanExtension{ReleaseObligation: release, Decision: "another-decision"}); err == nil {
		t.Fatal("a drop turned the plan all N/A and let a decision release the recovery")
	}
}

func TestSecondResetKeepsTheRolesADropLeftOnTheRecovery(t *testing.T) {
	root := tempBoard(t)
	ids, c := blockedGroup(t, root)
	c = recoverMember(t, root, c, ids[0])
	id := ids[0]
	a, b, d := owedCommit(0), owedCommit(1), owedCommit(2)
	security := map[string]string{"PMQA": "N/A: fixture", "Security": "required"}
	plan := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-" + id, Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{
		{BatchID: "batch", TaskIDs: []string{id}, Base: a, TargetCommit: b, Requirements: security},
		{BatchID: "second", PreviousBatchID: "batch", TaskIDs: []string{id}, Base: b, TargetCommit: d, Requirements: archiveRequirements()},
	}}
	if err := CreateReviewPlan(root, plan); err != nil {
		t.Fatal(err)
	}
	extendOwed(t, root, readOwedPlan(t, root, plan.PlanID), ReviewPlanExtension{DropBatches: []string{"second"}, Decision: "user-decision"})
	staleReclaim(t, root, id)
	recoverMember(t, root, coordinatorRequestAfter(t, root, c), id)
	o, ok := recoveryRecord(t, root, id)
	if !ok || o.PlanID != plan.PlanID || o.FromBatch != 1 || !slices.Equal(o.Roles, []string{"PMQA", "Security"}) {
		t.Fatalf("the second reset forgot a role: %+v %v", o, ok)
	}
}

func TestArchiveCleanupRemovesARecoveryWithNoPlan(t *testing.T) {
	root := tempBoard(t)
	ids, c := blockedGroup(t, root)
	recoverMember(t, root, c, ids[0])
	id := ids[0]
	deadEndExit(t, root, ids[1], "archived")
	deadEndExit(t, root, id, "archived")
	if _, ok := recoveryRecord(t, root, id); ok {
		t.Fatal("the record of an archived card stayed")
	}
}
