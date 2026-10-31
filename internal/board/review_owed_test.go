package board

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

func owedCommit(n int) string { return strings.Repeat(string(rune('a'+n)), 40) }

// owedPlan is a sealed plan of two batches: the first closes with a real review and
// the second, which requires PMQA, never runs.
func owedPlan(t *testing.T, root string, ids ...string) ReviewPlan {
	t.Helper()
	a, b, c := owedCommit(0), owedCommit(1), owedCommit(2)
	p := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-" + ids[0], Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: ids, Batches: []ReviewPlanBatch{
		{BatchID: "batch", TaskIDs: ids, Base: a, TargetCommit: b, Requirements: archiveRequirements()},
		{BatchID: "second", PreviousBatchID: "batch", TaskIDs: ids, Base: b, TargetCommit: c, Requirements: archiveRequirements()},
	}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	run := gateRun(t, root, archiveInput(ids, "first-"+ids[0], "PMQA"), emptyFindings())
	assignGate(t, root, run, map[string][]string{})
	if _, err := deadEndClose(t, root, "batch", map[string]ReviewRoleConclusion{"PMQA": passRole(run)}, nil); err != nil {
		t.Fatal(err)
	}
	return readOwedPlan(t, root, p.PlanID)
}

func readOwedPlan(t *testing.T, root, id string) ReviewPlan {
	t.Helper()
	p, err := ReadReviewPlan(root, id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func extendOwed(t *testing.T, root string, p ReviewPlan, x ReviewPlanExtension) ReviewPlan {
	t.Helper()
	if err := tryExtendOwed(root, p, x); err != nil {
		t.Fatal(err)
	}
	return readOwedPlan(t, root, p.PlanID)
}

func tryExtendOwed(root string, p ReviewPlan, x ReviewPlanExtension) error {
	x.PlanID, x.ExpectedRevision, x.Author, x.Basis = p.PlanID, p.Revision, "coordinator", "fixture"
	return ExtendReviewPlan(root, x)
}

func droppedOwed(t *testing.T, root string, ids ...string) ReviewPlan {
	t.Helper()
	return extendOwed(t, root, owedPlan(t, root, ids...), ReviewPlanExtension{DropBatches: []string{"second"}})
}

func owedBatch(p ReviewPlan, id string, requirements map[string]string, ids ...string) ReviewPlanBatch {
	last := p.Batches[len(p.Batches)-1]
	return ReviewPlanBatch{BatchID: id, PreviousBatchID: last.BatchID, TaskIDs: ids, Base: last.TargetCommit, TargetCommit: owedCommit(5), Requirements: requirements}
}

func closeOwedBatch(t *testing.T, root string, b ReviewPlanBatch) {
	t.Helper()
	in := archiveInput(b.TaskIDs, b.BatchID+"-run", "PMQA")
	in.BatchID, in.Base, in.Commit = b.BatchID, b.Base, b.TargetCommit
	run := gateRun(t, root, in, emptyFindings())
	assignGate(t, root, run, map[string][]string{})
	if _, err := deadEndClose(t, root, b.BatchID, map[string]ReviewRoleConclusion{"PMQA": passRole(run)}, nil); err != nil {
		t.Fatal(err)
	}
}

func historyDecision(t *testing.T, root string, p ReviewPlan) string {
	t.Helper()
	var history struct {
		Previous   ReviewPlan          `json:"previous"`
		Request    ReviewPlanExtension `json:"request"`
		RecordedAt string              `json:"recorded_at"`
	}
	err := WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
		_, e := readReviewJSON(tx, "plan-history/"+p.PlanID+"/"+strconv.FormatUint(p.Revision, 10)+".json", &history)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	return history.Request.Decision
}

func TestSealRefusesADroppedRequiredBatch(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "seal-refused")
	p := droppedOwed(t, root, id)
	err := tryExtendOwed(root, p, ReviewPlanExtension{Seal: true})
	if err == nil || !strings.Contains(err.Error(), id+"/PMQA") || !strings.Contains(err.Error(), "release_obligation") {
		t.Fatalf("seal after a drop: %v", err)
	}
}

func TestDoneRefusesADroppedRequiredBatch(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "done-refused")
	droppedOwed(t, root, id)
	err := deadEndDone(t, root, id)
	if err == nil || !strings.Contains(err.Error(), "dropped-review:"+id+"/PMQA") || !strings.Contains(err.Error(), "extend-plan") {
		t.Fatalf("done after a drop: %v", err)
	}
	progress, err := ReviewTaskProgress(root, id)
	if err != nil || !slices.Contains(progress.Pending, "dropped-review:"+id+"/PMQA") {
		t.Fatalf("%+v %v", progress, err)
	}
	if !slices.ContainsFunc(progress.NextActions, func(a ReviewNextAction) bool {
		return a.Action == "append-required-batch" && a.TaskID == id && a.Role == "PMQA"
	}) {
		t.Fatalf("%+v", progress.NextActions)
	}
}

func TestSealAcceptsAPlannedBatchThatRequiresTheRole(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "seal-planned")
	p := droppedOwed(t, root, id)
	b := owedBatch(p, "again", archiveRequirements(), id)
	p = extendOwed(t, root, p, ReviewPlanExtension{Batch: &b})
	if p = extendOwed(t, root, p, ReviewPlanExtension{Seal: true}); !p.Sealed {
		t.Fatal("not sealed")
	}
	if err := deadEndDone(t, root, id); err == nil || !strings.Contains(err.Error(), "dropped-review:"+id+"/PMQA") {
		t.Fatalf("a planned batch discharged the obligation: %v", err)
	}
}

func TestBatchAndSealInOneRequestDischargeTheObligation(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "seal-together")
	p := droppedOwed(t, root, id)
	b := owedBatch(p, "again", archiveRequirements(), id)
	extendOwed(t, root, p, ReviewPlanExtension{Batch: &b, Seal: true})
	closeOwedBatch(t, root, b)
	if err := deadEndDone(t, root, id); err != nil {
		t.Fatal(err)
	}
}

func TestNABatchDoesNotDischargeADroppedBatch(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "drop-na")
	p := droppedOwed(t, root, id)
	b := owedBatch(p, "na", noReviewRequirements(), id)
	if err := tryExtendOwed(root, p, ReviewPlanExtension{Batch: &b, Seal: true}); err == nil {
		t.Fatal("an N/A batch sealed over a dropped requirement")
	}
	p = extendOwed(t, root, p, ReviewPlanExtension{Batch: &b})
	if _, err := deadEndClose(t, root, "na", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := tryExtendOwed(root, p, ReviewPlanExtension{Seal: true}); err == nil {
		t.Fatal("a closed N/A batch sealed over a dropped requirement")
	}
}

func TestLaterBatchWithoutAMemberLeavesItOwed(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "owed-a"), gateCard(t, root, "owed-b")}
	p := droppedOwed(t, root, ids...)
	b := owedBatch(p, "only-a", archiveRequirements(), ids[0])
	p = extendOwed(t, root, p, ReviewPlanExtension{Batch: &b})
	closeOwedBatch(t, root, b)
	progress, err := ReviewTaskProgress(root, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(progress.Pending, "dropped-review:"+ids[1]+"/PMQA") || slices.Contains(progress.Pending, "dropped-review:"+ids[0]+"/PMQA") {
		t.Fatalf("%+v", progress.Pending)
	}
	err = tryExtendOwed(root, p, ReviewPlanExtension{Seal: true})
	if err == nil || !strings.Contains(err.Error(), ids[1]+"/PMQA") || strings.Contains(err.Error(), ids[0]+"/PMQA") {
		t.Fatalf("%v", err)
	}
}

func TestExcludedMemberOwesNoDroppedReview(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "stay"), gateCard(t, root, "leave")}
	p := droppedOwed(t, root, ids...)
	deadEndExit(t, root, ids[1], "trash")
	b := owedBatch(p, "only-stay", archiveRequirements(), ids[0])
	extendOwed(t, root, readOwedPlan(t, root, p.PlanID), ReviewPlanExtension{Batch: &b, Seal: true})
	closeOwedBatch(t, root, b)
	if err := deadEndDone(t, root, ids[0]); err != nil {
		t.Fatal(err)
	}
}

func TestDropDecisionLeavesNoObligation(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "drop-decided")
	p := extendOwed(t, root, owedPlan(t, root, id), ReviewPlanExtension{DropBatches: []string{"second"}, Decision: "  user-decision-1 "})
	if len(p.DropObligations) != 0 {
		t.Fatalf("%+v", p.DropObligations)
	}
	if got := historyDecision(t, root, p); got != "user-decision-1" {
		t.Fatalf("history %q", got)
	}
	extendOwed(t, root, p, ReviewPlanExtension{Seal: true})
	if err := deadEndDone(t, root, id); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseObligationNeedsADecisionAndWorksOnASealedPlan(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "release")
	p := droppedOwed(t, root, id)
	b := owedBatch(p, "again", archiveRequirements(), id)
	p = extendOwed(t, root, p, ReviewPlanExtension{Batch: &b, Seal: true})
	release := &ReviewObligationRelease{Kind: releaseDropped, TaskIDs: []string{id}}
	for _, decision := range []string{"", " \t\n"} {
		if err := tryExtendOwed(root, p, ReviewPlanExtension{ReleaseObligation: release, Decision: decision}); err == nil || !strings.Contains(err.Error(), "decision") {
			t.Fatalf("blank decision %q: %v", decision, err)
		}
	}
	p = extendOwed(t, root, p, ReviewPlanExtension{ReleaseObligation: release, Decision: "user-decision-2"})
	if len(p.DropObligations) != 0 || !p.Sealed {
		t.Fatalf("%+v", p)
	}
	if got := historyDecision(t, root, p); got != "user-decision-2" {
		t.Fatalf("history %q", got)
	}
	if err := tryExtendOwed(root, p, ReviewPlanExtension{ReleaseObligation: release, Decision: "again"}); err == nil {
		t.Fatal("a release with nothing to release passed")
	}
}

func TestBlankDropDecisionKeepsTheObligation(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "drop-blank")
	p := extendOwed(t, root, owedPlan(t, root, id), ReviewPlanExtension{DropBatches: []string{"second"}, Decision: "   "})
	if len(p.DropObligations) != 1 {
		t.Fatalf("%+v", p.DropObligations)
	}
}

func TestDecisionBelongsToADropOrARelease(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "decision-alone")
	p := droppedOwed(t, root, id)
	if err := tryExtendOwed(root, p, ReviewPlanExtension{Seal: true, Decision: "user-decision"}); err == nil || !strings.Contains(err.Error(), "decision") {
		t.Fatalf("%v", err)
	}
}

func TestDropDecisionKeepsTheBatchANewCycleOwes(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "drop-cycle")
	a, b, c := owedCommit(0), owedCommit(1), owedCommit(2)
	p := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-drop-cycle", Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{
		{BatchID: "old", TaskIDs: []string{id}, Base: a, TargetCommit: b, Requirements: noReviewRequirements()},
		{BatchID: "open", PreviousBatchID: "old", TaskIDs: []string{id}, Base: b, TargetCommit: c, Requirements: archiveRequirements()},
	}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	if _, err := deadEndClose(t, root, "old", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	deadEndReclaim(t, root, id)
	rebound := deadEndRebind(t, root, id)
	extendOwed(t, root, extendOwed(t, root, rebound, ReviewPlanExtension{DropBatches: []string{"open"}, Decision: "user-decision"}), ReviewPlanExtension{Seal: true})
	if err := deadEndDone(t, root, id); err == nil || !strings.Contains(err.Error(), "cycle-batch:"+id) {
		t.Fatalf("the decision released the batch of the new cycle: %v", err)
	}
}

func TestDropDecisionKeepsTheRecovery(t *testing.T) {
	root := tempBoard(t)
	id, p := droppedTailPlan(t, root)
	if len(p.DropObligations) != 0 {
		t.Fatalf("%+v", p.DropObligations)
	}
	progress, err := ReviewTaskProgress(root, id)
	if err != nil || !slices.Contains(progress.Pending, "recovery-review:"+id) {
		t.Fatalf("%+v %v", progress, err)
	}
	release := ReviewPlanExtension{ReleaseObligation: &ReviewObligationRelease{Kind: releaseRecovery, TaskIDs: []string{id}}, Decision: "user-decision"}
	if err = tryExtendOwed(root, p, release); err == nil {
		t.Fatal("a decision released a recovery of a planned member")
	}
}

func TestDroppingAnNABatchLeavesNoObligation(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "drop-only-na")
	a, b, c := owedCommit(0), owedCommit(1), owedCommit(2)
	p := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-drop-na", Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{
		{BatchID: "keep", TaskIDs: []string{id}, Base: a, TargetCommit: b, Requirements: noReviewRequirements()},
		{BatchID: "tail", PreviousBatchID: "keep", TaskIDs: []string{id}, Base: b, TargetCommit: c, Requirements: noReviewRequirements()},
	}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	if stored := extendOwed(t, root, readOwedPlan(t, root, p.PlanID), ReviewPlanExtension{DropBatches: []string{"tail"}}); len(stored.DropObligations) != 0 {
		t.Fatalf("%+v", stored.DropObligations)
	}
}

func TestTwoDropsKeepBothObligations(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "drop-twice")
	a, b, c, d := owedCommit(0), owedCommit(1), owedCommit(2), owedCommit(3)
	p := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-drop-twice", Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{
		{BatchID: "keep", TaskIDs: []string{id}, Base: a, TargetCommit: b, Requirements: noReviewRequirements()},
		{BatchID: "middle", PreviousBatchID: "keep", TaskIDs: []string{id}, Base: b, TargetCommit: c, Requirements: archiveRequirements()},
		{BatchID: "tail", PreviousBatchID: "middle", TaskIDs: []string{id}, Base: c, TargetCommit: d, Requirements: archiveRequirements()},
	}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	stored := extendOwed(t, root, readOwedPlan(t, root, p.PlanID), ReviewPlanExtension{DropBatches: []string{"tail"}})
	stored = extendOwed(t, root, stored, ReviewPlanExtension{DropBatches: []string{"middle"}})
	got := []string{}
	for _, o := range stored.DropObligations {
		got = append(got, o.BatchID)
		if o.FromBatch != 1 {
			t.Fatalf("%+v", o)
		}
	}
	if !slices.Equal(got, []string{"tail", "middle"}) {
		t.Fatalf("%v", got)
	}
}

func TestPlanInputCannotForgeDropObligations(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "forged-obligation")
	p := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-forged-drop", Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, DropObligations: []ReviewDropObligation{{BatchID: "x", TaskIDs: []string{id}, Roles: []string{"PMQA"}}}, Batches: []ReviewPlanBatch{{BatchID: "batch", TaskIDs: []string{id}, Base: owedCommit(0), TargetCommit: owedCommit(1), Requirements: noReviewRequirements()}}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	if stored := readOwedPlan(t, root, p.PlanID); stored.DropObligations != nil {
		t.Fatalf("%+v", stored.DropObligations)
	}
}
