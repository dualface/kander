package board

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// twinRuns publishes two parallel successful PMQA runs on the same commit and
// reviewer, both without findings.
func twinRuns(t *testing.T, root string, id string) (ReviewRun, ReviewRun) {
	t.Helper()
	gatePlan(t, root, []string{id}, archiveRequirements())
	a := gateRun(t, root, archiveInput([]string{id}, "twin-a", "PMQA"), emptyFindings())
	assignGate(t, root, a, map[string][]string{})
	b := gateRun(t, root, archiveInput([]string{id}, "twin-b", "PMQA"), emptyFindings())
	assignGate(t, root, b, map[string][]string{})
	return a, b
}

func supersedeRequest(t *testing.T, root string, conclusion ReviewRun, superseded map[string]ReviewSupersededRun) (ReviewBatchView, ReviewCloseRequest) {
	t.Helper()
	v, err := ReadReviewBatchView(root, "batch")
	if err != nil {
		t.Fatal(err)
	}
	return v, ReviewCloseRequest{BatchID: "batch", ExpectedRevision: v.Batch.Revision, ViewHash: ReviewViewDigest(v), Author: "coordinator", Roles: map[string]ReviewRoleConclusion{"PMQA": passRole(conclusion)}, SupersededRuns: superseded}
}

func closeWith(t *testing.T, root string, v ReviewBatchView, r ReviewCloseRequest) (ReviewClosure, []ReviewGitEdge, error) {
	t.Helper()
	edges, _, err := ReviewClosureEdges(v, r)
	if err != nil {
		return ReviewClosure{}, nil, err
	}
	c, err := CloseReviewBatch(root, r, ReviewGitEvidence{CWD: "/repo", Head: v.Batch.TargetCommit, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges})
	return c, edges, err
}

func TestSupersededTwinRunClosesBatch(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "twin-close")
	a, b := twinRuns(t, root, id)

	v, r := supersedeRequest(t, root, a, nil)
	_, _, err := ReviewClosureEdges(v, r)
	if err == nil {
		t.Fatal("unlinked twin closed without superseded_runs")
	}
	for _, want := range []string{b.RunID, a.RunID, "superseded_runs"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("close error lacks %q: %v", want, err)
		}
	}

	v, r = supersedeRequest(t, root, a, map[string]ReviewSupersededRun{b.RunID: {Replacement: a.RunID, Basis: "parallel duplicate of twin-a"}})
	_, edges, err := closeWith(t, root, v, r)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(edges, ReviewGitEdge{Ancestor: b.Commit, Descendant: a.Commit}) {
		t.Fatalf("superseded edge missing: %v", edges)
	}
	p, err := ReviewTaskProgress(root, id)
	if err != nil || p.Status != "closed" {
		t.Fatalf("progress after supersede close: %+v %v", p, err)
	}
}

func TestSupersededRunsRejectInvalidEntries(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "twin-reject")
	a, b := twinRuns(t, root, id)
	other := gateCard(t, root, "twin-other")
	p := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-other", Author: "coordinator", Basis: "second batch fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{other}, Batches: []ReviewPlanBatch{{BatchID: "other", TaskIDs: []string{other}, Base: strings.Repeat("a", 40), TargetCommit: strings.Repeat("b", 40), Requirements: archiveRequirements()}}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	foreignInput := archiveInput([]string{other}, "foreign", "PMQA")
	foreignInput.BatchID = "other"
	foreign := gateRun(t, root, foreignInput, emptyFindings())
	failedInput := archiveInput([]string{id}, "failed", "PMQA")
	failed, _, err := PrepareReviewRun(root, failedInput, nil, nil, archiveOriginals(), "test")
	if err != nil {
		t.Fatal(err)
	}
	failed.LaunchStatus, failed.ExecutionStatus, failed.ExitCode, failed.FailureReason = "started", "failed", 1, "fixture failure"
	if _, err = FinalizeReviewRun(root, failed, nil); err != nil {
		t.Fatal(err)
	}
	publishRun(t, root, failed.RunID)
	third := gateRun(t, root, archiveInput([]string{id}, "twin-third", "PMQA"), emptyFindings())
	assignGate(t, root, third, map[string][]string{})
	cases := map[string]map[string]ReviewSupersededRun{
		"other batch":           {b.RunID: {Replacement: a.RunID, Basis: "dup"}, foreign.RunID: {Replacement: a.RunID, Basis: "dup"}},
		"failed run":            {b.RunID: {Replacement: a.RunID, Basis: "dup"}, third.RunID: {Replacement: a.RunID, Basis: "dup"}, failed.RunID: {Replacement: a.RunID, Basis: "dup"}},
		"chain run":             {b.RunID: {Replacement: a.RunID, Basis: "dup"}, third.RunID: {Replacement: a.RunID, Basis: "dup"}, a.RunID: {Replacement: a.RunID, Basis: "dup"}},
		"replacement off chain": {b.RunID: {Replacement: third.RunID, Basis: "dup"}, third.RunID: {Replacement: a.RunID, Basis: "dup"}},
		"empty basis":           {b.RunID: {Replacement: a.RunID, Basis: " "}, third.RunID: {Replacement: a.RunID, Basis: "dup"}},
	}
	for name, superseded := range cases {
		t.Run(name, func(t *testing.T) {
			v, r := supersedeRequest(t, root, a, superseded)
			r.ResolvedFailures = map[string]string{failed.RunID: a.RunID}
			if _, _, err := ReviewClosureEdges(v, r); err == nil {
				t.Fatal("invalid superseded_runs accepted")
			}
		})
	}
	v, r := supersedeRequest(t, root, a, map[string]ReviewSupersededRun{b.RunID: {Replacement: a.RunID, Basis: "dup"}, third.RunID: {Replacement: a.RunID, Basis: "dup"}})
	r.ResolvedFailures = map[string]string{failed.RunID: a.RunID}
	if _, _, err := ReviewClosureEdges(v, r); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
}

func TestSupersededRunRequiresParallelTwinAndRole(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "twin-required")
	gatePlan(t, root, []string{id}, map[string]string{"PMQA": "required", "Security": "required"})
	a := gateRun(t, root, archiveInput([]string{id}, "pm-a", "PMQA"), emptyFindings())
	assignGate(t, root, a, map[string][]string{})
	other := archiveInput([]string{id}, "pm-other-reviewer", "PMQA")
	other.Reviewer = "claude"
	b := gateRun(t, root, other, emptyFindings())
	assignGate(t, root, b, map[string][]string{})
	sec := gateRun(t, root, archiveInput([]string{id}, "sec", "Security"), emptyFindings())
	assignGate(t, root, sec, map[string][]string{})
	secTwin := gateRun(t, root, archiveInput([]string{id}, "sec-twin", "Security"), emptyFindings())
	assignGate(t, root, secTwin, map[string][]string{})
	v, err := ReadReviewBatchView(root, "batch")
	if err != nil {
		t.Fatal(err)
	}
	r := ReviewCloseRequest{BatchID: "batch", ExpectedRevision: v.Batch.Revision, ViewHash: ReviewViewDigest(v), Author: "coordinator", Roles: map[string]ReviewRoleConclusion{"PMQA": passRole(a), "Security": passRole(sec)}}
	r.SupersededRuns = map[string]ReviewSupersededRun{b.RunID: {Replacement: a.RunID, Basis: "dup"}, secTwin.RunID: {Replacement: sec.RunID, Basis: "dup"}}
	if _, _, err = ReviewClosureEdges(v, r); err == nil || !strings.Contains(err.Error(), "twin") {
		t.Fatalf("reviewer switch accepted as duplicate: %v", err)
	}
	r.Roles["PMQA"] = passRole(a)
	r.SupersededRuns = map[string]ReviewSupersededRun{secTwin.RunID: {Replacement: a.RunID, Basis: "dup"}}
	if _, _, err = ReviewClosureEdges(v, r); err == nil {
		t.Fatal("cross-role replacement accepted")
	}
}

func TestSupersededTwinWithFixedMustFixClosesThroughIncrementalRun(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "twin-fixed")
	gatePlan(t, root, []string{id}, archiveRequirements())
	a := gateRun(t, root, archiveInput([]string{id}, "twin-a", "PMQA"), emptyFindings())
	assignGate(t, root, a, map[string][]string{})
	f := ReviewFinding{ID: "PM-01", Tier: "high", Text: "缺陷", Evidence: "x:1"}
	findings := emptyFindings()
	findings.Findings = []ReviewFinding{f}
	b := gateRun(t, root, archiveInput([]string{id}, "twin-b", "PMQA"), findings)

	if _, err := ReadReviewBatchView(root, "batch"); err == nil || !strings.Contains(err.Error(), b.RunID) {
		t.Fatalf("superseded run without assignment aggregated: %v", err)
	}

	assignGate(t, root, b, map[string][]string{f.ID: {id}})
	fix := strings.Repeat("c", 40)
	d := ReviewDisposition{RecordID: "fix", RunID: b.RunID, FindingID: f.ID, BatchID: "batch", TaskID: id, Author: "codex", ReportHash: b.Hashes["report.md"], Original: f.Text, Status: "fixed", Basis: "实际修复", FixCommit: fix, Verification: "复现测试通过"}
	if err := SubmitReviewDisposition(root, d, transactionSnapshot(t, root, id).Revision); err != nil {
		t.Fatal(err)
	}
	if err := AdvanceReviewBatch(root, ReviewBatchAdvance{BatchID: "batch", ExpectedRevision: 1, Advance: ReviewAdvance{PreviousTarget: a.Commit, Target: fix, Reason: "member fix", Deliveries: map[string]string{fix: id}}}); err != nil {
		t.Fatal(err)
	}
	v, r := supersedeRequest(t, root, a, map[string]ReviewSupersededRun{b.RunID: {Replacement: a.RunID, Basis: "dup"}})
	// Concluding with the twin claims the fix is an ancestor of the reviewed
	// commit, which Git verification refuses; a later run must conclude.
	if edges, _, err := ReviewClosureEdges(v, r); err != nil || !slices.Contains(edges, ReviewGitEdge{Ancestor: fix, Descendant: a.Commit}) {
		t.Fatalf("fixed must-fix lost its ancestry claim: %v %v", edges, err)
	}

	input := archiveInput([]string{id}, "twin-c", "PMQA")
	input.PreviousRunID, input.ReviewedCommit, input.Commit = a.RunID, a.Commit, fix
	c := gateRun(t, root, input, emptyFindings())
	assignGate(t, root, c, map[string][]string{})
	v, r = supersedeRequest(t, root, c, map[string]ReviewSupersededRun{b.RunID: {Replacement: c.RunID, Basis: "parallel duplicate of twin-a"}})
	if _, _, err := closeWith(t, root, v, r); err != nil {
		t.Fatal(err)
	}
}

func TestClosureWithoutSupersededRunsKeepsStoredBytes(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "closure-bytes")
	gatePlan(t, root, []string{id}, archiveRequirements())
	run := gateRun(t, root, archiveInput([]string{id}, "only", "PMQA"), emptyFindings())
	assignGate(t, root, run, map[string][]string{})
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{"PMQA": passRole(run)}); err != nil {
		t.Fatal(err)
	}
	s := transactionSnapshot(t, root, id)
	data, err := os.ReadFile(filepath.Join(s.Entry.Path, "reviews", "batches", "batch", "closed.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "superseded_runs") {
		t.Fatal("closure without superseded runs changed its stored form")
	}
	p, err := ReviewTaskProgress(root, id)
	if err != nil || p.Status != "closed" || p.NextActions != nil {
		t.Fatalf("progress %+v %v", p, err)
	}
	if problems := CheckReviewGate(root, []string{id}); len(problems) > 0 {
		t.Fatalf("gate problems %v", problems)
	}
	if problems, err := CheckReviewEvidence(root, []string{id}); err != nil || len(problems) > 0 {
		t.Fatalf("evidence problems %v %v", problems, err)
	}
}

func TestProgressReportsDuplicateChainHeads(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "progress-heads")
	gatePlan(t, root, []string{id}, archiveRequirements())
	a := gateRun(t, root, archiveInput([]string{id}, "head-a", "PMQA"), emptyFindings())
	p, err := ReviewTaskProgress(root, id)
	if err != nil || p.NextActions != nil {
		t.Fatalf("single head reported: %+v %v", p, err)
	}
	b := gateRun(t, root, archiveInput([]string{id}, "head-b", "PMQA"), emptyFindings())
	p, err = ReviewTaskProgress(root, id)
	if err != nil {
		t.Fatal(err)
	}
	want := []ReviewNextAction{{Action: "supersede-duplicate-runs", BatchID: "batch", Role: "PMQA", RunIDs: []string{a.RunID, b.RunID}}}
	if !reflect.DeepEqual(p.NextActions, want) {
		t.Fatalf("next actions %+v", p.NextActions)
	}
}

func TestUnlinkedReviewRunsWarnOnlyForOpenDuplicates(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "start-warning")
	gatePlan(t, root, []string{id}, archiveRequirements())
	failedInput := archiveInput([]string{id}, "failed", "PMQA")
	failed, _, err := PrepareReviewRun(root, failedInput, nil, nil, archiveOriginals(), "test")
	if err != nil {
		t.Fatal(err)
	}
	failed.LaunchStatus, failed.ExecutionStatus, failed.ExitCode, failed.FailureReason = "started", "failed", 1, "fixture failure"
	if _, err = FinalizeReviewRun(root, failed, nil); err != nil {
		t.Fatal(err)
	}
	publishRun(t, root, failed.RunID)
	retry, _, err := PrepareReviewRun(root, archiveInput([]string{id}, "retry", "PMQA"), nil, nil, archiveOriginals(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if unlinked, err := UnlinkedReviewRuns(root, retry); err != nil || len(unlinked) != 0 {
		t.Fatalf("failed run warned on retry: %v %v", unlinked, err)
	}
	retry.LaunchStatus, retry.ExecutionStatus = "started", "ok"
	if _, err = FinalizeReviewRun(root, retry, structuredReport(emptyFindings())); err != nil {
		t.Fatal(err)
	}
	publishRun(t, root, retry.RunID)

	parallel, _, err := PrepareReviewRun(root, archiveInput([]string{id}, "parallel", "PMQA"), nil, nil, archiveOriginals(), "test")
	if err != nil {
		t.Fatal(err)
	}
	unlinked, err := UnlinkedReviewRuns(root, parallel)
	if err != nil || len(unlinked) != 1 || unlinked[0].RunID != retry.RunID {
		t.Fatalf("parallel duplicate not reported: %v %v", unlinked, err)
	}
	if warning := UnlinkedReviewRunsWarning(parallel, unlinked); !strings.Contains(warning, retry.RunID) {
		t.Fatalf("warning lacks run ID: %q", warning)
	}

	input := archiveInput([]string{id}, "linked", "PMQA")
	input.PreviousRunID, input.ReviewedCommit = retry.RunID, retry.Commit
	linked, _, err := PrepareReviewRun(root, input, nil, nil, archiveOriginals(), "test")
	if err != nil {
		t.Fatal(err)
	}
	unlinked, err = UnlinkedReviewRuns(root, linked)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range unlinked {
		if r.RunID == retry.RunID {
			t.Fatal("linked predecessor reported as unlinked")
		}
	}
}
