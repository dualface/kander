package board

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/config"
)

func deadEndFinding() ReviewFinding {
	return ReviewFinding{ID: "PMQA-01", Tier: "medium", Text: "原始发现", Evidence: "x.go:1"}
}

func deadEndFindings() ReviewFindings {
	f := emptyFindings()
	f.Findings = []ReviewFinding{deadEndFinding()}
	return f
}

func deadEndClose(t *testing.T, root, batch string, roles map[string]ReviewRoleConclusion, superseded map[string]ReviewSupersededRun) (ReviewClosure, error) {
	t.Helper()
	v, err := ReadReviewBatchView(root, batch)
	if err != nil {
		return ReviewClosure{}, err
	}
	r := ReviewCloseRequest{BatchID: batch, ExpectedRevision: v.Batch.Revision, ViewHash: ReviewViewDigest(v), Author: "coordinator", Roles: roles, SupersededRuns: superseded}
	edges, _, err := ReviewClosureEdges(v, r)
	if err != nil {
		return ReviewClosure{}, err
	}
	return CloseReviewBatch(root, r, ReviewGitEvidence{CWD: "/repo", Head: v.Batch.TargetCommit, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges})
}

func deadEndReady(t *testing.T, root, id string) {
	t.Helper()
	s := transactionSnapshot(t, root, id)
	text := strings.Replace(s.Text, "## SUMMARY\n\n<FILL_IN>", "## SUMMARY\n\nFixture completion.", 1)
	if err := WithTransaction(root, LockScope{Tasks: []string{id}}, func(tx *Transaction) error { return tx.Put(id, "spec.md", text) }); err != nil {
		t.Fatal(err)
	}
}

func deadEndDone(t *testing.T, root, id string) error {
	t.Helper()
	deadEndReady(t, root, id)
	_, err := MoveWithOptions(transactionSnapshot(t, root, id).Entry, root, "done", MoveOptions{Result: "completed"})
	return err
}

func deadEndExit(t *testing.T, root, id, state string) {
	t.Helper()
	result := map[string]string{"archived": "cancelled", "trash": "trashed"}[state]
	if s := transactionSnapshot(t, root, id); s.Entry.State == "done" && state == "archived" {
		result = "completed"
	}
	if _, err := MoveWithOptions(transactionSnapshot(t, root, id).Entry, root, state, MoveOptions{Result: result, Reason: "用户取消", Decision: "decision-1"}); err != nil {
		t.Fatal(err)
	}
}

func deadEndExclusion(t *testing.T, root, id string) (e ReviewExclusion, exists bool) {
	t.Helper()
	err := WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
		var err error
		e, exists, err = readMemberExclusion(tx, id)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return e, exists
}

func deadEndReclaim(t *testing.T, root, id string) {
	t.Helper()
	moved, err := MoveWithOptions(transactionSnapshot(t, root, id).Entry, root, "review", MoveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = MoveWithOptions(moved, root, "working", MoveOptions{Owner: "claude", Decision: "user-reclaim"}); err != nil {
		t.Fatal(err)
	}
}

func deadEndRebind(t *testing.T, root, id string) ReviewPlan {
	t.Helper()
	progress, err := ReviewTaskProgress(root, id)
	if err != nil || len(progress.RebindCycles) == 0 {
		t.Fatalf("%+v %v", progress, err)
	}
	p, err := ReadReviewPlan(root, progress.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if err = ExtendReviewPlan(root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: p.Revision, RebindCycles: progress.RebindCycles, Author: "coordinator", Basis: "rebind"}); err != nil {
		t.Fatal(err)
	}
	if p, err = ReadReviewPlan(root, p.PlanID); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReviewRunOutsidePlanRefusedBeforeTheBatchExists(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "stray-refused")
	p := gatePlan(t, root, []string{id}, archiveRequirements())
	input := archiveInput([]string{id}, "stray-run", "PMQA")
	input.BatchID = "stray"
	_, _, err := PrepareReviewRun(root, input, archiveRequirements(), nil, archiveOriginals(), "test")
	if err == nil || !strings.Contains(err.Error(), "extend-plan") || !strings.Contains(err.Error(), p.PlanID) {
		t.Fatalf("stray batch: %v", err)
	}
	if _, err := ReadReviewBatch(root, "stray"); err == nil {
		t.Fatal("refused run left a batch")
	}
	if _, exists, err := LookupReviewRun(root, "stray-run"); err != nil || exists {
		t.Fatalf("refused run persisted: %v %v", exists, err)
	}
}

func TestSupersedeRecordClosesTwoRootRuns(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "two-roots")
	gatePlan(t, root, []string{id}, archiveRequirements())
	old := archiveInput([]string{id}, "root-old", "PMQA")
	first := gateRun(t, root, old, deadEndFindings())
	assignGate(t, root, first, map[string][]string{"PMQA-01": {id}})
	next := archiveInput([]string{id}, "root-new", "PMQA")
	next.Reviewer = "claude"
	second := gateRun(t, root, next, emptyFindings())
	assignGate(t, root, second, map[string][]string{})
	roles := map[string]ReviewRoleConclusion{"PMQA": {RunID: second.RunID, PassedAt: second.Commit, Basis: "换 reviewer 后的结论"}}
	entry := ReviewSupersededRun{Replacement: second.RunID, Basis: "reviewer 不再可用"}

	// The finding of the retired run still needs its author record.
	if _, err := deadEndClose(t, root, "batch", roles, map[string]ReviewSupersededRun{first.RunID: entry}); err == nil {
		t.Fatal("closed with an undisposed finding of a retired run")
	}
	s := transactionSnapshot(t, root, id)
	d := ReviewDisposition{RecordID: "record-root", RunID: first.RunID, FindingID: "PMQA-01", BatchID: "batch", TaskID: id, Author: "codex", ReportHash: first.Hashes["report.md"], Original: "原始发现", Status: "rejected", Basis: "事实依据"}
	if err := SubmitReviewDisposition(root, d, s.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := deadEndClose(t, root, "batch", roles, map[string]ReviewSupersededRun{first.RunID: entry}); err == nil || !strings.Contains(err.Error(), "parallel twin") {
		t.Fatalf("root run retired with no decision: %v", err)
	}
	blank := entry
	blank.Decision = "  "
	if _, err := deadEndClose(t, root, "batch", roles, map[string]ReviewSupersededRun{first.RunID: blank}); err == nil {
		t.Fatal("blank decision accepted")
	}
	entry.Decision = "user-decision-7"
	closure, err := deadEndClose(t, root, "batch", roles, map[string]ReviewSupersededRun{first.RunID: entry})
	if err != nil {
		t.Fatal(err)
	}
	if got := closure.Request.SupersededRuns[first.RunID].Decision; got != "user-decision-7" {
		t.Fatalf("decision lost: %q", got)
	}
	if err = deadEndDone(t, root, id); err != nil {
		t.Fatal(err)
	}
}

func TestSupersedeDecisionCoversARootAndItsChainOnly(t *testing.T) {
	chain := func(id, previous, commit string) ReviewRunView {
		return ReviewRunView{Run: ReviewRun{ReviewInput: ReviewInput{RunID: id, PreviousRunID: previous, Commit: commit, Reviewer: "codex"}, ExecutionStatus: "ok"}}
	}
	head, runs := chain("head", "", "a"), map[string]ReviewRunView{}
	for _, v := range []ReviewRunView{head, chain("fork", "head", "b"), chain("root", "", "c"), chain("tail", "root", "d")} {
		runs[v.Run.RunID] = v
	}
	decided := func(id string) ReviewSupersededRun {
		return ReviewSupersededRun{Replacement: "head", Basis: "replaced", Decision: "decision-" + id}
	}
	onChain := map[string]bool{"head": true}
	if _, err := supersededReplacement(runs, onChain, head.Run, runs["root"].Run, map[string]ReviewSupersededRun{"root": decided("root")}); err != nil {
		t.Fatal(err)
	}
	if _, err := supersededReplacement(runs, onChain, head.Run, runs["tail"].Run, map[string]ReviewSupersededRun{"root": decided("root"), "tail": decided("tail")}); err != nil {
		t.Fatal(err)
	}
	if _, err := supersededReplacement(runs, onChain, head.Run, runs["fork"].Run, map[string]ReviewSupersededRun{"fork": decided("fork")}); err == nil {
		t.Fatal("a decision retired a run that continues the conclusion chain")
	}
}

func TestPlanRefusesAGapInTheBatchChain(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "chain-gap")
	batch := func(name, previous, base, target string) ReviewPlanBatch {
		return ReviewPlanBatch{BatchID: name, PreviousBatchID: previous, TaskIDs: []string{id}, Base: base, TargetCommit: target, Requirements: noReviewRequirements()}
	}
	a, b, c := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	p := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-gap", Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{batch("one", "", a, b), batch("two", "one", c, a)}}
	if err := CreateReviewPlan(root, p); err == nil || !strings.Contains(err.Error(), "two") {
		t.Fatalf("gap accepted: %v", err)
	}
	p.Batches[1] = batch("two", "one", b, c)
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
}

func TestAdvanceRefusesPlannedSuccessorAndDropBatchesLeavesOneWayOut(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "successor")
	a, b, c, d := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40), strings.Repeat("d", 40)
	planned := func(name, previous, base, target string) ReviewPlanBatch {
		return ReviewPlanBatch{BatchID: name, PreviousBatchID: previous, TaskIDs: []string{id}, Base: base, TargetCommit: target, Requirements: noReviewRequirements()}
	}
	p := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-successor", Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{planned("first", "", a, b), planned("second", "first", b, c)}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	advance := ReviewBatchAdvance{BatchID: "first", ExpectedRevision: 1, Advance: ReviewAdvance{PreviousTarget: b, Target: d, Reason: "fix", Deliveries: map[string]string{d: id}}}
	err := AdvanceReviewBatch(root, advance)
	if err == nil || !strings.Contains(err.Error(), "extend-plan") || !strings.Contains(err.Error(), "second") {
		t.Fatalf("advance over a successor: %v", err)
	}
	if err = ExtendReviewPlan(root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: 1, DropBatches: []string{"first"}, Author: "coordinator", Basis: "wrong batch"}); err == nil {
		t.Fatal("a drop that is not the tail passed")
	}
	if err = ExtendReviewPlan(root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: 1, DropBatches: []string{"second"}, Author: "coordinator", Basis: "replan"}); err != nil {
		t.Fatal(err)
	}
	stored, err := ReadReviewPlan(root, p.PlanID)
	if err != nil || stored.Sealed || len(stored.Batches) != 1 {
		t.Fatalf("%+v %v", stored, err)
	}
	if err = AdvanceReviewBatch(root, advance); err != nil {
		t.Fatal(err)
	}
	// The record of the dropped batch stays, so its ID cannot return with a new base.
	again := planned("second", "first", d, c)
	if err = ExtendReviewPlan(root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: stored.Revision + 1, Batch: &again, Author: "coordinator", Basis: "again"}); err == nil {
		t.Fatal("dropped ID came back")
	}
}

func TestDropBatchesRefusesABatchThatRan(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "drop-ran")
	base, target := archiveInput(nil, "", "PMQA").Base, archiveInput(nil, "", "PMQA").Commit
	head := strings.Repeat("e", 40)
	p := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-drop-ran", Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{
		{BatchID: "keep", TaskIDs: []string{id}, Base: head, TargetCommit: base, Requirements: noReviewRequirements()},
		{BatchID: "batch", PreviousBatchID: "keep", TaskIDs: []string{id}, Base: base, TargetCommit: target, Requirements: archiveRequirements()},
	}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	if _, err := deadEndClose(t, root, "keep", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	gateRun(t, root, archiveInput([]string{id}, "ran", "PMQA"), emptyFindings())
	if err := ExtendReviewPlan(root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: 1, DropBatches: []string{"batch"}, Author: "coordinator", Basis: "drop"}); err == nil || !strings.Contains(err.Error(), "no run") {
		t.Fatalf("dropped a batch with a run: %v", err)
	}
}

func TestMemberExclusionFollowsTheTerminalMove(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "exclude-a"), gateCard(t, root, "exclude-b"), gateCard(t, root, "exclude-c"), gateCard(t, root, "exclude-d")}
	gatePlan(t, root, ids, noReviewRequirements())
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{}); err != nil {
		t.Fatal(err)
	}
	deadEndExit(t, root, ids[0], "archived")
	e, ok := deadEndExclusion(t, root, ids[0])
	if !ok || e.FromState != "working" || e.State != "archived" || e.Result != "cancelled" || e.Decision != "decision-1" || e.PlanID == "" || e.TaskGroup == "" {
		t.Fatalf("%+v %v", e, ok)
	}
	deadEndExit(t, root, ids[1], "trash")
	if e, ok = deadEndExclusion(t, root, ids[1]); !ok || e.Result != "trashed" {
		t.Fatalf("%+v %v", e, ok)
	}
	// done is not excluded, done then archived is not either, and trash after done is.
	for _, id := range ids[2:] {
		if err := deadEndDone(t, root, id); err != nil {
			t.Fatal(err)
		}
		if _, ok = deadEndExclusion(t, root, id); ok {
			t.Fatal("done member excluded")
		}
	}
	deadEndExit(t, root, ids[2], "archived")
	if _, ok = deadEndExclusion(t, root, ids[2]); ok {
		t.Fatal("member archived after done excluded")
	}
	deadEndExit(t, root, ids[3], "trash")
	if e, ok = deadEndExclusion(t, root, ids[3]); !ok || e.FromState != "done" {
		t.Fatalf("%+v %v", e, ok)
	}
}

func TestExclusionNeedsAPlanOrAGroup(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "plain-card", false)
	id := s.Entry.TaskID
	if _, err := MoveWithOptions(s.Entry, root, "trash", MoveOptions{Result: "trashed", Reason: "清理", Decision: "decision"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := deadEndExclusion(t, root, id); ok {
		t.Fatal("a card of no plan and no group was excluded")
	}
}

func TestPlanWritesSkipMembersThatLeft(t *testing.T) {
	for _, state := range []string{"archived", "trash"} {
		t.Run(state, func(t *testing.T) {
			root := tempBoard(t)
			ids := []string{gateCard(t, root, "stays"), gateCard(t, root, "leaves")}
			gatePlan(t, root, ids, noReviewRequirements())
			deadEndExit(t, root, ids[1], state)
			gone := transactionSnapshot(t, root, ids[1])
			read := func() (copied string) {
				err := WithTransaction(root, reviewScope(ids, true), func(tx *Transaction) error { var e error; copied, e = tx.Read(ids[1], "reviews/plan.json"); return e })
				if err != nil {
					t.Fatal(err)
				}
				return copied
			}
			old := read()
			target := strings.Repeat("f", 40)
			advance := ReviewBatchAdvance{BatchID: "batch", ExpectedRevision: 1, Advance: ReviewAdvance{PreviousTarget: strings.Repeat("b", 40), Target: target, Reason: "fix", Deliveries: map[string]string{target: ids[0]}}}
			if err := AdvanceReviewBatch(root, advance); err != nil {
				t.Fatal(err)
			}
			if read() != old || transactionSnapshot(t, root, ids[1]).Revision != gone.Revision {
				t.Fatal("a plan write reached the member that left")
			}
			if _, err := ReviewTaskProgress(root, ids[0]); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMemberArchivedAfterDoneKeepsThePlanWritable(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "late-stays"), gateCard(t, root, "late-done")}
	p := gatePlan(t, root, ids, noReviewRequirements())
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{}); err != nil {
		t.Fatal(err)
	}
	if err := deadEndDone(t, root, ids[1]); err != nil {
		t.Fatal(err)
	}
	deadEndExit(t, root, ids[1], "archived")
	deadEndReclaim(t, root, ids[0])
	stored := deadEndRebind(t, root, ids[0])
	if stored.PlanID != p.PlanID || len(stored.CycleBatchFrom) != 1 {
		t.Fatalf("%+v", stored)
	}
	// The copy of the archived member stays behind; no gate treats that as damage.
	if progress, err := ReviewTaskProgress(root, ids[0]); err != nil || progress.Status != "pending" {
		t.Fatalf("%+v %v", progress, err)
	}
}

func TestDoneGateCoversOnlyMembersNotExcluded(t *testing.T) {
	for _, state := range []string{"archived", "trash"} {
		t.Run(state, func(t *testing.T) {
			root := tempBoard(t)
			ids := []string{gateCard(t, root, "survivor"), gateCard(t, root, "gone")}
			gatePlan(t, root, ids, noReviewRequirements())
			if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{}); err != nil {
				t.Fatal(err)
			}
			// The cycle of the member changes, and it leaves before any rebind.
			deadEndReclaim(t, root, ids[1])
			deadEndExit(t, root, ids[1], state)
			if err := deadEndDone(t, root, ids[0]); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRunNamesTheMembersThatRemain(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "run-stays"), gateCard(t, root, "run-leaves")}
	gatePlan(t, root, ids, archiveRequirements())
	first := gateRun(t, root, archiveInput(ids, "run-both", "PMQA"), emptyFindings())
	assignGate(t, root, first, map[string][]string{})
	in := archiveInput(ids[:1], "run-early", "PMQA")
	in.PreviousRunID, in.ReviewedCommit = first.RunID, first.Commit
	if _, _, err := PrepareReviewRun(root, in, nil, nil, archiveOriginals(), "test"); err == nil {
		t.Fatal("a run dropped a member that still takes part")
	}
	deadEndExit(t, root, ids[1], "archived")
	second := gateRun(t, root, archiveInput(ids[:1], "run-rest", "PMQA"), emptyFindings())
	assignGate(t, root, second, map[string][]string{})
	roles := map[string]ReviewRoleConclusion{"PMQA": {RunID: second.RunID, PassedAt: second.Commit, Basis: "结论"}}
	superseded := map[string]ReviewSupersededRun{first.RunID: {Replacement: second.RunID, Basis: "并行副本"}}
	if _, err := deadEndClose(t, root, "batch", roles, superseded); err != nil {
		t.Fatal(err)
	}
	if err := deadEndDone(t, root, ids[0]); err != nil {
		t.Fatal(err)
	}
}

// The incremental round names fewer members than its predecessor, and holds no
// lock of the member that left.
func TestIncrementalRunAfterAMemberLeft(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "next-stays"), gateCard(t, root, "next-leaves")}
	gatePlan(t, root, ids, archiveRequirements())
	first := gateRun(t, root, archiveInput(ids, "next-first", "PMQA"), deadEndFindings())
	assignGate(t, root, first, map[string][]string{"PMQA-01": {ids[1]}})
	gateRecord(t, root, first, ids[1], deadEndFinding(), "rejected")
	target := strings.Repeat("f", 40)
	advance := ReviewBatchAdvance{BatchID: "batch", ExpectedRevision: 1, Advance: ReviewAdvance{PreviousTarget: first.Commit, Target: target, Reason: "fix", Deliveries: map[string]string{target: ids[0]}}}
	if err := AdvanceReviewBatch(root, advance); err != nil {
		t.Fatal(err)
	}
	deadEndExit(t, root, ids[1], "trash")
	in := archiveInput(ids[:1], "next-second", "PMQA")
	in.Commit, in.PreviousRunID, in.ReviewedCommit = target, first.RunID, first.Commit
	second := gateRun(t, root, in, emptyFindings())
	assignGate(t, root, second, map[string][]string{})
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{"PMQA": {RunID: second.RunID, PassedAt: second.Commit, Basis: "结论"}}); err != nil {
		t.Fatal(err)
	}
	if err := deadEndDone(t, root, ids[0]); err != nil {
		t.Fatal(err)
	}
}

func TestIncrementalContextAfterAPublishedViewAndAnExit(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "view-stays"), gateCard(t, root, "view-leaves")}
	gatePlan(t, root, ids, archiveRequirements())
	first := gateRun(t, root, archiveInput(ids, "view-first", "PMQA"), emptyFindings())
	assignGate(t, root, first, map[string][]string{})
	deadEndExit(t, root, ids[1], "trash")
	if _, err := PublishReviewDisposition(root, "batch"); err != nil {
		t.Fatal(err)
	}
	if _, err := ReviewIncrementalContext(root, first.RunID); err != nil {
		t.Fatal(err)
	}
	remaining, err := ReviewRemainingMembers(root, first.TaskIDs)
	if err != nil || !slices.Equal(remaining, ids[:1]) {
		t.Fatalf("%v %v", remaining, err)
	}
}

func TestReplayFinishesARunAfterAMemberLeft(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "replay-stays"), gateCard(t, root, "replay-leaves")}
	gatePlan(t, root, ids, archiveRequirements())
	in := archiveInput(ids, "replay-run", "PMQA")
	if _, fresh, err := PrepareReviewRun(root, in, nil, nil, archiveOriginals(), "test"); err != nil || !fresh {
		t.Fatalf("%v %v", fresh, err)
	}
	deadEndExit(t, root, ids[1], "archived")
	if _, fresh, err := PrepareReviewRun(root, in, nil, nil, archiveOriginals(), "test"); err != nil || fresh {
		t.Fatalf("replay: %v %v", fresh, err)
	}
}

func TestPublicationSkipsAMemberThatLeftDuringTheRun(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "pub-stays"), gateCard(t, root, "pub-leaves")}
	gatePlan(t, root, ids, archiveRequirements())
	run, _, err := PrepareReviewRun(root, archiveInput(ids, "pub-run", "PMQA"), nil, nil, archiveOriginals(), "test")
	if err != nil {
		t.Fatal(err)
	}
	run.LaunchStatus, run.ExecutionStatus = "started", "ok"
	if run, err = FinalizeReviewRun(root, run, structuredReport(emptyFindings())); err != nil {
		t.Fatal(err)
	}
	deadEndExit(t, root, ids[1], "archived")
	published := publishRun(t, root, run.RunID)
	if !allPublished(published) || !slices.Equal(published.SkippedMembers, ids[1:]) {
		t.Fatalf("%+v %v", published.Published, published.SkippedMembers)
	}
	if problems, err := CheckReviewEvidence(root, ids); err != nil || len(problems) != 0 {
		t.Fatalf("%+v %v", problems, err)
	}
	assignGate(t, root, published, map[string][]string{})
	if _, err = gateClose(t, root, map[string]ReviewRoleConclusion{"PMQA": {RunID: run.RunID, PassedAt: run.Commit, Basis: "结论"}}); err != nil {
		t.Fatal(err)
	}
	if err = deadEndDone(t, root, ids[0]); err != nil {
		t.Fatal(err)
	}
}

func TestBatchWithNoMemberLeftNeitherWaitsNorBreaksTheChain(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "void-stays"), gateCard(t, root, "void-leaves")}
	a, b, c := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	p := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-void", Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: ids, Batches: []ReviewPlanBatch{
		{BatchID: "void", TaskIDs: ids[1:], Base: a, TargetCommit: b, Requirements: archiveRequirements()},
		{BatchID: "rest", PreviousBatchID: "void", TaskIDs: ids[:1], Base: b, TargetCommit: c, Requirements: noReviewRequirements()},
	}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	deadEndExit(t, root, ids[1], "trash")
	if _, err := deadEndClose(t, root, "rest", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := deadEndDone(t, root, ids[0]); err != nil {
		t.Fatal(err)
	}
}

func withdrawal(id string, run ReviewRun, exclusion string) ReviewDisposition {
	return ReviewDisposition{RecordID: "withdrawn-" + id, RunID: run.RunID, FindingID: "PMQA-01", BatchID: run.BatchID, TaskID: id, Author: "coordinator", ReportHash: run.Hashes["report.md"], Original: "原始发现", Status: "withdrawn", Basis: "成员已退出", Withdrawal: &ReviewWithdrawal{ExclusionID: exclusion, Decision: "decision-2"}}
}

func TestWithdrawnClosesTheFindingOfAnExcludedMember(t *testing.T) {
	for _, state := range []string{"archived", "trash"} {
		t.Run(state, func(t *testing.T) {
			root := tempBoard(t)
			ids := []string{gateCard(t, root, "author"), gateCard(t, root, "orphan")}
			gatePlan(t, root, ids, archiveRequirements())
			run := gateRun(t, root, archiveInput(ids, "finding-run", "PMQA"), deadEndFindings())
			assignGate(t, root, run, map[string][]string{"PMQA-01": {ids[1]}})
			live := transactionSnapshot(t, root, ids[1])
			if err := SubmitReviewDisposition(root, withdrawal(ids[1], run, "any"), live.Revision); err == nil || !strings.Contains(err.Error(), "excluded member") {
				t.Fatalf("withdrawn on a live member: %v", err)
			}
			deadEndExit(t, root, ids[1], state)
			gone := transactionSnapshot(t, root, ids[1])
			progress, err := ReviewTaskProgress(root, ids[1])
			if err != nil || progress.Status != "excluded" || progress.Exclusion == nil || len(progress.Pending) != 1 {
				t.Fatalf("%+v %v", progress, err)
			}
			if _, err = PublishReviewDisposition(root, "batch"); err == nil || !strings.Contains(err.Error(), "withdrawn") {
				t.Fatalf("aggregate before the withdrawal: %v", err)
			}
			if err = SubmitReviewDisposition(root, withdrawal(ids[1], run, "wrong"), gone.Revision); err == nil {
				t.Fatal("wrong exclusion accepted")
			}
			noDecision := withdrawal(ids[1], run, progress.Exclusion.ExclusionID)
			noDecision.Withdrawal.Decision = " "
			if err = SubmitReviewDisposition(root, noDecision, gone.Revision); err == nil {
				t.Fatal("blank decision accepted")
			}
			d := withdrawal(ids[1], run, progress.Exclusion.ExclusionID)
			if err = SubmitReviewDisposition(root, d, gone.Revision); err != nil {
				t.Fatal(err)
			}
			if err = SubmitReviewDisposition(root, d, gone.Revision); err != nil {
				t.Fatalf("replay: %v", err)
			}
			if transactionSnapshot(t, root, ids[1]).Revision != gone.Revision {
				t.Fatal("the withdrawal wrote the card")
			}
			v, err := PublishReviewDisposition(root, "batch")
			if err != nil {
				t.Fatal(err)
			}
			if len(v.Runs) != 1 || len(v.Runs[0].Withdrawn) != 1 || v.Runs[0].Withdrawn[0].Status != "withdrawn" {
				t.Fatalf("%+v", v.Runs)
			}
			if _, err = gateClose(t, root, map[string]ReviewRoleConclusion{"PMQA": {RunID: run.RunID, PassedAt: run.Commit, Basis: "结论"}}); err != nil {
				t.Fatal(err)
			}
			if err = deadEndDone(t, root, ids[0]); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAssignmentRefusesAnExcludedMember(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "assign-stays"), gateCard(t, root, "assign-gone")}
	gatePlan(t, root, ids, archiveRequirements())
	run := gateRun(t, root, archiveInput(ids, "assign-run", "PMQA"), deadEndFindings())
	deadEndExit(t, root, ids[1], "trash")
	a := ReviewAssignment{RunID: run.RunID, BatchID: run.BatchID, Author: "coordinator", Basis: "分配", Items: map[string][]string{"PMQA-01": {ids[1]}}}
	if err := AssignReviewFindings(root, a); err == nil || !strings.Contains(err.Error(), ids[1]) {
		t.Fatalf("assigned to an excluded member: %v", err)
	}
	a.Items["PMQA-01"] = []string{ids[0]}
	if err := AssignReviewFindings(root, a); err != nil {
		t.Fatal(err)
	}
}

func TestReboundCycleNeedsABatchBeforeDone(t *testing.T) {
	cases := []struct {
		name  string
		batch func(p ReviewPlan, id string) ReviewPlanBatch
		close func(t *testing.T, root, id string)
	}{
		{"not applicable", func(p ReviewPlan, id string) ReviewPlanBatch {
			return ReviewPlanBatch{BatchID: "cycle", PreviousBatchID: "batch", TaskIDs: []string{id}, Base: p.Batches[0].TargetCommit, TargetCommit: strings.Repeat("f", 40), Requirements: noReviewRequirements()}
		}, func(t *testing.T, root, _ string) {
			if _, err := deadEndClose(t, root, "cycle", map[string]ReviewRoleConclusion{}, nil); err != nil {
				t.Fatal(err)
			}
		}},
		{"actual review", func(p ReviewPlan, id string) ReviewPlanBatch {
			return ReviewPlanBatch{BatchID: "cycle", PreviousBatchID: "batch", TaskIDs: []string{id}, Base: p.Batches[0].TargetCommit, TargetCommit: strings.Repeat("f", 40), Requirements: archiveRequirements()}
		}, func(t *testing.T, root, id string) {
			in := archiveInput([]string{id}, "cycle-run", "PMQA")
			in.BatchID, in.Base, in.Commit = "cycle", archiveInput(nil, "", "PMQA").Commit, strings.Repeat("f", 40)
			run := gateRun(t, root, in, emptyFindings())
			assignGate(t, root, run, map[string][]string{})
			roles := map[string]ReviewRoleConclusion{"PMQA": {RunID: run.RunID, PassedAt: run.Commit, Basis: "结论"}}
			if _, err := deadEndClose(t, root, "cycle", roles, nil); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := tempBoard(t)
			ids := []string{gateCard(t, root, "rebound"), gateCard(t, root, "delivered")}
			gatePlan(t, root, ids, noReviewRequirements())
			if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{}); err != nil {
				t.Fatal(err)
			}
			if err := deadEndDone(t, root, ids[1]); err != nil {
				t.Fatal(err)
			}
			deadEndReclaim(t, root, ids[0])
			p := deadEndRebind(t, root, ids[0])
			if err := deadEndDone(t, root, ids[0]); err == nil || !strings.Contains(err.Error(), "cycle-batch:"+ids[0]) || !strings.Contains(err.Error(), "extend-plan") {
				t.Fatalf("rebind alone passed done: %v", err)
			}
			progress, err := ReviewTaskProgress(root, ids[0])
			if err != nil || !slices.ContainsFunc(progress.NextActions, func(a ReviewNextAction) bool { return a.Action == "append-cycle-batch" && a.TaskID == ids[0] }) {
				t.Fatalf("%+v %v", progress, err)
			}
			batch := c.batch(p, ids[0])
			foreign := batch
			foreign.TaskIDs = ids[1:]
			if err = ExtendReviewPlan(root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: p.Revision, Batch: &foreign, Author: "coordinator", Basis: "wrong member"}); err == nil {
				t.Fatal("a sealed plan took a batch no rebound cycle needs")
			}
			if err = ExtendReviewPlan(root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: p.Revision, Batch: &batch, Author: "coordinator", Basis: "new cycle"}); err != nil {
				t.Fatal(err)
			}
			if progress, err = ReviewTaskProgress(root, ids[1]); err != nil || progress.Status != "closed" {
				t.Fatalf("the done member reads a later batch: %+v %v", progress, err)
			}
			c.close(t, root, ids[0])
			if err = deadEndDone(t, root, ids[0]); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPlanInputCannotForgeCycleBatches(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "forged-from")
	p := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-forged", Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, CycleBatchFrom: map[string]int{id: 0}, Batches: []ReviewPlanBatch{{BatchID: "batch", TaskIDs: []string{id}, Base: strings.Repeat("a", 40), TargetCommit: strings.Repeat("b", 40), Requirements: noReviewRequirements()}}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	if stored, err := ReadReviewPlan(root, p.PlanID); err != nil || stored.CycleBatchFrom != nil {
		t.Fatalf("%+v %v", stored, err)
	}
}

func TestSealedPlanTakesNoBatchWithoutAReboundCycle(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "sealed")
	p := gatePlan(t, root, []string{id}, noReviewRequirements())
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{}); err != nil {
		t.Fatal(err)
	}
	batch := ReviewPlanBatch{BatchID: "more", PreviousBatchID: "batch", TaskIDs: []string{id}, Base: p.Batches[0].TargetCommit, TargetCommit: strings.Repeat("f", 40), Requirements: noReviewRequirements()}
	if err := ExtendReviewPlan(root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: p.Revision, Batch: &batch, Author: "c", Basis: "more"}); err == nil {
		t.Fatal("sealed plan took a batch")
	}
}

func TestDroppingTheOpenBatchOfAReboundCycleOwesItAgain(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "drop-owed")
	a, b, c := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	p := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-drop-owed", Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{
		{BatchID: "old", TaskIDs: []string{id}, Base: a, TargetCommit: b, Requirements: noReviewRequirements()},
		{BatchID: "open", PreviousBatchID: "old", TaskIDs: []string{id}, Base: b, TargetCommit: c, Requirements: noReviewRequirements()},
	}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	if _, err := deadEndClose(t, root, "old", map[string]ReviewRoleConclusion{}, nil); err != nil {
		t.Fatal(err)
	}
	deadEndReclaim(t, root, id)
	rebound := deadEndRebind(t, root, id)
	if err := ExtendReviewPlan(root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: rebound.Revision, DropBatches: []string{"open"}, Author: "coordinator", Basis: "drop"}); err != nil {
		t.Fatal(err)
	}
	if err := ExtendReviewPlan(root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: rebound.Revision + 1, Seal: true, Author: "coordinator", Basis: "seal"}); err != nil {
		t.Fatal(err)
	}
	if err := deadEndDone(t, root, id); err == nil || !strings.Contains(err.Error(), "cycle-batch:"+id) {
		t.Fatalf("the old closure passed done for the new cycle: %v", err)
	}
}

func TestReboundWhileABatchIsOpenOwesNoNewBatch(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "open-rebind")
	gatePlan(t, root, []string{id}, noReviewRequirements())
	deadEndReclaim(t, root, id)
	deadEndRebind(t, root, id)
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{}); err != nil {
		t.Fatal(err)
	}
	if err := deadEndDone(t, root, id); err != nil {
		t.Fatal(err)
	}
}

func TestPruneRemovesExclusionWithItsPlan(t *testing.T) {
	root := tempBoard(t)
	ids := []string{gateCard(t, root, "prune-a"), gateCard(t, root, "prune-b")}
	gatePlan(t, root, ids, noReviewRequirements())
	name := func(id string) string { return control(root, "groups", reviewControlGroup, "exclusions", id+".json") }
	deadEndExit(t, root, ids[0], "archived")
	if _, err := os.Stat(name(ids[0])); err != nil {
		t.Fatalf("exclusion removed with a live plan: %v", err)
	}
	deadEndExit(t, root, ids[1], "archived")
	for _, id := range ids {
		if _, err := os.Stat(name(id)); !os.IsNotExist(err) {
			t.Fatalf("exclusion kept: %v", err)
		}
	}
}

func deadEndReconcile(t *testing.T, root string, c CoordinatorCheckpoint) error {
	t.Helper()
	_, err := ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), nil)
	return err
}

func TestCoordinatorTerminalBranchNeedsTheExclusionRecord(t *testing.T) {
	root := tempBoard(t)
	ids := []string{recoveryMember(t, root, "term-a"), recoveryMember(t, root, "term-b")}
	c := coordinatorClaim(t, root, ids...)
	c, err := ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), nil)
	if err != nil {
		t.Fatal(err)
	}
	deadEndExit(t, root, ids[1], "trash")
	path := control(root, "groups", reviewControlGroup, "exclusions", ids[1]+".json")
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = deadEndReconcile(t, root, c); err == nil || !strings.Contains(err.Error(), "no exclusion record") {
		t.Fatalf("missing exclusion passed: %v", err)
	}
	var e ReviewExclusion
	if err = json.Unmarshal(saved, &e); err != nil {
		t.Fatal(err)
	}
	e.Result = "cancelled"
	if err = os.WriteFile(path, []byte(reviewJSON(e)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = deadEndReconcile(t, root, c); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("forged exclusion passed: %v", err)
	}
	if err = os.WriteFile(path, saved, 0o600); err != nil {
		t.Fatal(err)
	}
	if err = deadEndReconcile(t, root, c); err != nil {
		t.Fatal(err)
	}
}

func TestCoordinatorLegacyTerminalMemberKeepsResultOnlyCheck(t *testing.T) {
	root := tempBoard(t)
	id := coordinatorCard(t, root, "legacy-member").Entry.TaskID
	coordinatorCard(t, root, "legacy-peer")
	deadEndExit(t, root, id, "archived")
	if err := os.Remove(control(root, "groups", reviewControlGroup, "exclusions", id+".json")); err != nil {
		t.Fatal(err)
	}
	s := transactionSnapshot(t, root, id)
	err := WithTransaction(root, LockScope{Groups: []string{reviewControlGroup, taskStartGroup}, Tasks: []string{id}, ReadOnly: true}, func(tx *Transaction) error {
		return verifyTerminalExit(tx, s, executionLedger{task: id})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCoordinatorMemberArchivedAfterDoneNeedsNoExclusion(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "after-done")
	gatePlan(t, root, []string{id}, noReviewRequirements())
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{}); err != nil {
		t.Fatal(err)
	}
	if err := deadEndDone(t, root, id); err != nil {
		t.Fatal(err)
	}
	deadEndExit(t, root, id, "archived")
	s := transactionSnapshot(t, root, id)
	check := func(l executionLedger) error {
		return WithTransaction(root, LockScope{Groups: []string{reviewControlGroup}, Tasks: []string{id}, ReadOnly: true}, func(tx *Transaction) error {
			return verifyTerminalExit(tx, s, l)
		})
	}
	stored := executionLedger{task: id, chain: []ExecutionTransition{{}}}
	if err := check(stored); err != nil {
		t.Fatal(err)
	}
	if s.Text = strings.Replace(s.Text, "- RESULT: completed", "- RESULT: cancelled", 1); check(stored) == nil {
		t.Fatal("a stored chain with no exclusion passed")
	}
}

func TestHintsNameTheRealWayOut(t *testing.T) {
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "1")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "en"})
	t.Run("T-04 stale dispatch", func(t *testing.T) {
		for _, id := range []string{"board.dispatch_conflict", "launch.dispatch_pending"} {
			if text := catalogText(id, "dispatch-1"); !strings.Contains(text, "kander dispatch fail") {
				t.Fatalf("%s: %s", id, text)
			}
		}
	})
	t.Run("T-05 wrap-up grant", func(t *testing.T) {
		text := catalogText("board.wrap_up_grant_hint")
		for _, want := range []string{"no renewal", "report.md", "kander dispatch fail"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s: %s", want, text)
			}
		}
	})
	t.Run("T-09 interrupted run", func(t *testing.T) {
		root := tempBoard(t)
		id := archiveCard(t, root, "input-conflict")
		in := archiveInput([]string{id}, "replay", "PMQA")
		if _, _, err := PrepareReviewRun(root, in, archiveRequirements(), nil, archiveOriginals(), "test"); err != nil {
			t.Fatal(err)
		}
		in.Model = "other"
		_, _, err := PrepareReviewRun(root, in, archiveRequirements(), nil, archiveOriginals(), "test")
		if err == nil || !strings.Contains(err.Error(), "input conflict") || !strings.Contains(err.Error(), "--run-id") {
			t.Fatalf("replay hint: %v", err)
		}
		if text := catalogText("board.review_task_context_frozen_no_snapshot", "batch"); strings.Contains(text, "start a new batch") {
			t.Fatalf("impossible hint: %s", text)
		}
	})
	t.Run("T-10 moved worktree", func(t *testing.T) {
		root := tempBoard(t)
		id := gateCard(t, root, "moved")
		gatePlan(t, root, []string{id}, archiveRequirements())
		in := archiveInput([]string{id}, "moved-run", "PMQA")
		in.CWD = "/moved"
		_, _, err := PrepareReviewRun(root, in, nil, nil, archiveOriginals(), "test")
		if err == nil || !strings.Contains(err.Error(), "recreate the worktree at /repo") {
			t.Fatalf("worktree hint: %v", err)
		}
	})
}

// catalogText reads a message by ID. A test's t shadows the package helper.
func catalogText(id string, args ...any) string { return t(id, args...) }
