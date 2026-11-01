package board

import (
	"slices"
	"strings"
	"testing"
)

// droppedThenReset builds a group whose plan closed an N/A batch and dropped a
// required tail, runs between, and then resets the first member.
func droppedThenReset(t *testing.T, root, decision string, between func(ReviewPlan, []string) ReviewPlan) ([]string, ReviewPlan) {
	t.Helper()
	ids := []string{recoveryMember(t, root, "owed-reset-a"), recoveryMember(t, root, "owed-reset-b")}
	c := coordinatorReconcile(t, root, coordinatorClaim(t, root, ids...))
	a, b, d := owedCommit(0), owedCommit(1), owedCommit(2)
	plan := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-owed-reset", Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: ids, Batches: []ReviewPlanBatch{
		{BatchID: "batch", TaskIDs: ids, Base: a, TargetCommit: b, Requirements: noReviewRequirements()},
		{BatchID: "tail", PreviousBatchID: "batch", TaskIDs: ids, Base: b, TargetCommit: d, Requirements: archiveRequirements()},
	}}
	if err := CreateReviewPlan(root, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := deadEndClose(t, root, "batch", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	p := extendOwed(t, root, readOwedPlan(t, root, plan.PlanID), ReviewPlanExtension{DropBatches: []string{"tail"}, Decision: decision})
	if between != nil {
		p = between(p, ids)
	}
	staleReclaim(t, root, ids[0])
	recoverMember(t, root, coordinatorRequestAfter(t, root, c), ids[0])
	return ids, deadEndRebind(t, root, ids[0])
}

func releaseDrop(t *testing.T, root string, p ReviewPlan, ids []string) ReviewPlan {
	t.Helper()
	return extendOwed(t, root, p, ReviewPlanExtension{ReleaseObligation: &ReviewObligationRelease{Kind: releaseDropped, TaskIDs: ids}, Decision: "user-decision"})
}

// A drop never hides a required role from a later reset, whichever way the drop
// obligation ended. The N/A batch stands in for the review that reset voided.
func TestResetAfterADropOwesTheDroppedRole(t *testing.T) {
	cases := []struct {
		name     string
		decision string
		between  func(*testing.T, string) func(ReviewPlan, []string) ReviewPlan
		after    func(*testing.T, string, ReviewPlan, []string) ReviewPlan
	}{
		{name: "released after the reset", after: releaseDrop},
		{name: "drop with a decision", decision: "user-decision"},
		{name: "released before the reset", between: func(t *testing.T, root string) func(ReviewPlan, []string) ReviewPlan {
			return func(p ReviewPlan, ids []string) ReviewPlan { return releaseDrop(t, root, p, ids) }
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := tempBoard(t)
			var between func(ReviewPlan, []string) ReviewPlan
			if tc.between != nil {
				between = tc.between(t, root)
			}
			ids, p := droppedThenReset(t, root, tc.decision, between)
			if o, ok := recoveryRecord(t, root, ids[0]); !ok || o.PlanID != p.PlanID || !slices.Equal(o.Roles, []string{"PMQA"}) {
				t.Fatalf("%+v %v", o, ok)
			}
			p = appendBatch(t, root, p, ids[0], "cycle-na", noReviewRequirements())
			if _, err := deadEndClose(t, root, "cycle-na", map[string]ReviewRoleConclusion{}, nil); err != nil {
				t.Fatal(err)
			}
			if tc.after != nil {
				p = tc.after(t, root, p, ids)
			}
			p = extendOwed(t, root, p, ReviewPlanExtension{Seal: true})
			err := deadEndDone(t, root, ids[0])
			if err == nil || !strings.Contains(err.Error(), "recovery-review:"+ids[0]) || !strings.Contains(err.Error(), "recovery-role:"+ids[0]+"/PMQA") {
				t.Fatalf("a reset after a drop passed the gate: %v", err)
			}
			release := ReviewPlanExtension{ReleaseObligation: &ReviewObligationRelease{Kind: releaseRecovery, TaskIDs: []string{ids[0]}}, Decision: "user-decision"}
			if err = tryExtendOwed(root, p, release); err == nil {
				t.Fatal("a decision released the recovery of a planned member")
			}
			p = appendBatch(t, root, p, ids[0], "cycle-real", archiveRequirements())
			closeActualBatch(t, root, p, ids[0], "cycle-real")
			if err = deadEndDone(t, root, ids[0]); err != nil {
				t.Fatalf("the actual review did not discharge the recovery: %v", err)
			}
		})
	}
}

func TestPlanKeepsTheDroppedRolesOfEachMember(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "kept-a"), gateCard(t, root, "kept-b")}
	a, b, c, d := owedCommit(0), owedCommit(1), owedCommit(2), owedCommit(3)
	stored := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-kept", Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: ids, DroppedRoles: map[string][]string{ids[1]: {"Security"}}, Batches: []ReviewPlanBatch{
		{BatchID: "keep", TaskIDs: ids, Base: a, TargetCommit: b, Requirements: noReviewRequirements()},
		{BatchID: "middle", PreviousBatchID: "keep", TaskIDs: ids, Base: b, TargetCommit: c, Requirements: map[string]string{"PMQA": "N/A: fixture", "Security": "required"}},
		{BatchID: "tail", PreviousBatchID: "middle", TaskIDs: ids[:1], Base: c, TargetCommit: d, Requirements: archiveRequirements()},
	}}
	if err := CreateReviewPlan(root, stored); err != nil {
		t.Fatal(err)
	}
	p := readOwedPlan(t, root, stored.PlanID)
	if p.DroppedRoles != nil {
		t.Fatalf("plan input forged the record: %+v", p.DroppedRoles)
	}
	p = extendOwed(t, root, p, ReviewPlanExtension{DropBatches: []string{"tail"}})
	if !slices.Equal(p.DroppedRoles[ids[0]], []string{"PMQA"}) || p.DroppedRoles[ids[1]] != nil {
		t.Fatalf("%+v", p.DroppedRoles)
	}
	p = extendOwed(t, root, p, ReviewPlanExtension{DropBatches: []string{"middle"}, Decision: "user-decision"})
	if !slices.Equal(p.DroppedRoles[ids[0]], []string{"PMQA", "Security"}) || !slices.Equal(p.DroppedRoles[ids[1]], []string{"Security"}) {
		t.Fatalf("a decision left no record: %+v", p.DroppedRoles)
	}
	p = releaseDrop(t, root, p, ids[:1])
	if len(p.DropObligations) != 0 || !slices.Equal(p.DroppedRoles[ids[0]], []string{"PMQA", "Security"}) {
		t.Fatalf("a release removed the record: %+v", p)
	}
}

func TestDroppingAnNABatchRecordsNoRole(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "kept-none")
	a, b, c := owedCommit(0), owedCommit(1), owedCommit(2)
	stored := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-kept-none", Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{
		{BatchID: "keep", TaskIDs: []string{id}, Base: a, TargetCommit: b, Requirements: noReviewRequirements()},
		{BatchID: "tail", PreviousBatchID: "keep", TaskIDs: []string{id}, Base: b, TargetCommit: c, Requirements: noReviewRequirements()},
	}}
	if err := CreateReviewPlan(root, stored); err != nil {
		t.Fatal(err)
	}
	if p := extendOwed(t, root, readOwedPlan(t, root, stored.PlanID), ReviewPlanExtension{DropBatches: []string{"tail"}}); p.DroppedRoles != nil {
		t.Fatalf("%+v", p.DroppedRoles)
	}
}

// resetUnderNAPlan resets the first member of a group with no plan, then plans the
// group all N/A and closes that batch.
func resetUnderNAPlan(t *testing.T, root string) []string {
	t.Helper()
	ids, c := blockedGroup(t, root)
	recoverMember(t, root, c, ids[0])
	gatePlan(t, root, ids, noReviewRequirements())
	if _, err := deadEndClose(t, root, "batch", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	return ids
}

func TestCompletedMemberDoesNotBlockTheOthers(t *testing.T) {
	for _, final := range []string{"done", "archived"} {
		t.Run(final, func(t *testing.T) {
			root := tempBoard(t)
			ids := resetUnderNAPlan(t, root)
			p := readOwedPlan(t, root, "plan-"+ids[0])
			extendOwed(t, root, p, ReviewPlanExtension{ReleaseObligation: &ReviewObligationRelease{Kind: releaseRecovery, TaskIDs: ids[:1]}, Decision: "user-decision"})
			if err := deadEndDone(t, root, ids[0]); err != nil {
				t.Fatal(err)
			}
			if final == "archived" {
				deadEndExit(t, root, ids[0], final)
			}
			deadEndReclaim(t, root, ids[1])
			p = appendBatch(t, root, deadEndRebind(t, root, ids[1]), ids[1], "b-only", archiveRequirements())
			progress, err := ReviewTaskProgress(root, ids[1])
			if err != nil {
				t.Fatal(err)
			}
			if slices.ContainsFunc(progress.Pending, func(item string) bool { return strings.Contains(item, ids[0]) }) {
				t.Fatalf("a completed member stayed on the list: %+v", progress.Pending)
			}
			if slices.ContainsFunc(progress.NextActions, func(a ReviewNextAction) bool { return a.Action == "append-cycle-batch" && a.TaskID == ids[0] }) {
				t.Fatalf("a batch for a completed member: %+v", progress.NextActions)
			}
			closeActualBatch(t, root, p, ids[1], "b-only")
			if problems := CheckReviewGate(root, ids[:1]); len(problems) != 0 {
				t.Fatalf("a batch of another member failed the completed card: %+v", problems)
			}
			if err = deadEndDone(t, root, ids[1]); err != nil {
				t.Fatalf("a completed member blocked the other: %v", err)
			}
		})
	}
}

func TestOpenRecoveryStillBindsTheMember(t *testing.T) {
	root := tempBoard(t)
	ids := resetUnderNAPlan(t, root)
	if err := deadEndDone(t, root, ids[0]); err == nil || !strings.Contains(err.Error(), "recovery-review:"+ids[0]) {
		t.Fatalf("the member passed with its recovery open: %v", err)
	}
	if progress, err := ReviewTaskProgress(root, ids[1]); err != nil || !slices.Contains(progress.Pending, "recovery-review:"+ids[0]) {
		t.Fatalf("%+v %v", progress, err)
	}
}

// voidedFinding plans a batch of the leaving member alone and a closed N/A batch of
// the staying member on top of it, assigns one finding, and removes the member.
func voidedFinding(t *testing.T, root string, status string) (ids []string, run ReviewRun) {
	t.Helper()
	ids = []string{gateCard(t, root, "stays"), gateCard(t, root, "leaves")}
	a, b, c := owedCommit(0), owedCommit(1), owedCommit(2)
	plan := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-voided", Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: ids, Batches: []ReviewPlanBatch{
		{BatchID: "voided", TaskIDs: ids[1:], Base: a, TargetCommit: b, Requirements: archiveRequirements()},
		{BatchID: "rest", PreviousBatchID: "voided", TaskIDs: ids[:1], Base: b, TargetCommit: c, Requirements: noReviewRequirements()},
	}}
	if err := CreateReviewPlan(root, plan); err != nil {
		t.Fatal(err)
	}
	in := archiveInput(ids[1:], "voided-run", "PMQA")
	in.BatchID, in.Base, in.Commit = "voided", a, b
	run = gateRun(t, root, in, deadEndFindings())
	assignGate(t, root, run, map[string][]string{"PMQA-01": ids[1:]})
	if status != "" {
		gateRecord(t, root, run, ids[1], deadEndFinding(), status)
	}
	deadEndExit(t, root, ids[1], "trash")
	return ids, run
}

func owedItem(run ReviewRun, id string) string {
	return "withdrawal:" + run.RunID + "/PMQA-01/" + id
}

func TestVoidBatchOwesItsWithdrawnRecords(t *testing.T) {
	for _, status := range []string{"", "confirmed", "unverifiable"} {
		t.Run("status "+status, func(t *testing.T) {
			root := tempBoard(t)
			ids, run := voidedFinding(t, root, status)
			if _, err := deadEndClose(t, root, "rest", map[string]ReviewRoleConclusion{}, nil); err != nil {
				t.Fatal(err)
			}
			err := deadEndDone(t, root, ids[0])
			if err == nil || !strings.Contains(err.Error(), owedItem(run, ids[1])) || !strings.Contains(err.Error(), "withdrawn") {
				t.Fatalf("done over an owed withdrawal: %v", err)
			}
			progress, err := ReviewTaskProgress(root, ids[0])
			if err != nil || !slices.Contains(progress.Pending, owedItem(run, ids[1])) {
				t.Fatalf("%+v %v", progress, err)
			}
			gone, err := ReviewTaskProgress(root, ids[1])
			if err != nil || gone.Exclusion == nil || !slices.Contains(gone.Pending, owedItem(run, ids[1])) {
				t.Fatalf("%+v %v", gone, err)
			}
			d := withdrawal(ids[1], run, gone.Exclusion.ExclusionID)
			if status != "" {
				d.PreviousRecordID = "record-" + ids[1]
			}
			if err = SubmitReviewDisposition(root, d, transactionSnapshot(t, root, ids[1]).Revision); err != nil {
				t.Fatalf("the void batch refused the record: %v", err)
			}
			if err = deadEndDone(t, root, ids[0]); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOpenBatchListsAWithdrawalOverAnUnsettledRecord(t *testing.T) {
	for _, status := range []string{"confirmed", "unverifiable"} {
		t.Run(status, func(t *testing.T) {
			root := tempBoard(t)
			ids := []string{gateCard(t, root, "stays"), gateCard(t, root, "leaves")}
			gatePlan(t, root, ids, archiveRequirements())
			run := gateRun(t, root, archiveInput(ids, "open-run", "PMQA"), deadEndFindings())
			assignGate(t, root, run, map[string][]string{"PMQA-01": ids[1:]})
			gateRecord(t, root, run, ids[1], deadEndFinding(), status)
			deadEndExit(t, root, ids[1], "trash")
			progress, err := ReviewTaskProgress(root, ids[0])
			if err != nil || !slices.Contains(progress.Pending, owedItem(run, ids[1])) {
				t.Fatalf("%+v %v", progress, err)
			}
		})
	}
}

// The ledger is written directly: fixed, deferred and waived each need their own
// evidence, and the rule reads only the latest status.
func TestWithdrawalsOwedReadsTheLatestStatus(t *testing.T) {
	cases := map[string]bool{"": true, "confirmed": true, "unverifiable": true, "fixed": false, "rejected": false, "deferred": false, "waived": false, "withdrawn": false}
	for status, want := range cases {
		t.Run("status "+status, func(t *testing.T) {
			root := tempBoard(t)
			ids, run := voidedFinding(t, root, "")
			err := WithTransaction(root, reviewScope(ids, false), func(tx *Transaction) error {
				var ledger dispositionLedger
				if _, e := readReviewJSON(tx, ledgerName(run.RunID), &ledger); e != nil {
					return e
				}
				if status != "" {
					ledger.Records = []ReviewDisposition{{FindingID: "PMQA-01", TaskID: ids[1], Status: "confirmed"}, {FindingID: "PMQA-01", TaskID: ids[1], Status: status}}
				}
				return tx.PutGroup(reviewControlGroup, ledgerName(run.RunID), reviewJSON(ledger))
			})
			if err != nil {
				t.Fatal(err)
			}
			err = WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
				var b ReviewBatch
				if _, e := readReviewJSON(tx, reviewBatchName("voided"), &b); e != nil {
					return e
				}
				owed, e := withdrawalsOwed(tx, b)
				if got := slices.Contains(owed, owedItem(run, ids[1])); got != want || e != nil {
					t.Fatalf("owed %v, want %v: %v", owed, want, e)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLaterExclusionLeavesADeliveredCardValid(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "delivered"), gateCard(t, root, "later")}
	gatePlan(t, root, ids, noReviewRequirements())
	if _, err := deadEndClose(t, root, "batch", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := deadEndDone(t, root, ids[0]); err != nil {
		t.Fatal(err)
	}
	deadEndReclaim(t, root, ids[1])
	p := appendBatch(t, root, deadEndRebind(t, root, ids[1]), ids[1], "later", archiveRequirements())
	b := p.Batches[len(p.Batches)-1]
	in := archiveInput(ids[1:], "later-run", "PMQA")
	in.BatchID, in.Base, in.Commit = b.BatchID, b.Base, b.TargetCommit
	run := gateRun(t, root, in, deadEndFindings())
	assignGate(t, root, run, map[string][]string{"PMQA-01": ids[1:]})
	deadEndExit(t, root, ids[1], "trash")
	if problems := CheckReviewGate(root, ids[:1]); len(problems) != 0 {
		t.Fatalf("a later exit failed the delivered card: %+v", problems)
	}
	if progress, err := ReviewTaskProgress(root, ids[1]); err != nil || !slices.Contains(progress.Pending, owedItem(run, ids[1])) {
		t.Fatalf("%+v %v", progress, err)
	}
}
