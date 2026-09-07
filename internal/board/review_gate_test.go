package board

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func gatePlan(t *testing.T, root string, ids []string, requirements map[string]string) ReviewPlan {
	t.Helper()
	p := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-" + ids[0], Author: "coordinator", Basis: "测试契约明确指定适用角色", CWD: "/repo", ReportLanguage: "en", TaskIDs: ids, Batches: []ReviewPlanBatch{{BatchID: "batch", TaskIDs: ids, Base: strings.Repeat("a", 40), TargetCommit: strings.Repeat("b", 40), Requirements: requirements}}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	return p
}
func noReviewRequirements() map[string]string {
	return map[string]string{"PM": "N/A: fixture; lifecycle-only contract", "QA": "N/A: fixture; lifecycle-only contract", "CSA": "N/A: fixture; no security review", "Hacker": "N/A: fixture; no external surface"}
}
func gateClose(t *testing.T, root string, roles map[string]ReviewRoleConclusion) (ReviewClosure, error) {
	t.Helper()
	v, err := ReadReviewBatchView(root, "batch")
	if err != nil {
		return ReviewClosure{}, err
	}
	r := ReviewCloseRequest{BatchID: "batch", ExpectedRevision: v.Batch.Revision, ViewHash: ReviewViewDigest(v), Author: "coordinator", Roles: roles}
	edges, _, err := ReviewClosureEdges(v, r)
	if err != nil {
		return ReviewClosure{}, err
	}
	return CloseReviewBatch(root, r, ReviewGitEvidence{CWD: "/repo", Head: v.Batch.TargetCommit, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges})
}

// Lifecycle tests use an explicit, machine-readable N/A plan. This helper does
// not run Git and is only a fixture for the board's pure structural contract.
func exemptReviewFixture(t *testing.T, root, id string) {
	t.Helper()
	p := ReviewPlan{Schema: 1, Sealed: true, PlanID: "exempt-" + id, Author: "fixture", Basis: "lifecycle-only test", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{{BatchID: "exempt-" + id, TaskIDs: []string{id}, Base: strings.Repeat("a", 40), TargetCommit: strings.Repeat("b", 40), Requirements: noReviewRequirements()}}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	v, err := ReadReviewBatchView(root, p.Batches[0].BatchID)
	if err != nil {
		t.Fatal(err)
	}
	r := ReviewCloseRequest{BatchID: v.Batch.BatchID, ExpectedRevision: v.Batch.Revision, ViewHash: ReviewViewDigest(v), Author: "fixture", Roles: map[string]ReviewRoleConclusion{}}
	edges, _, err := ReviewClosureEdges(v, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = CloseReviewBatch(root, r, ReviewGitEvidence{CWD: "/repo", Head: v.Batch.TargetCommit, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges}); err != nil {
		t.Fatal(err)
	}
}

func gateCard(t *testing.T, root, slug string) string {
	t.Helper()
	id := archiveCard(t, root, slug)
	s := transactionSnapshot(t, root, id)
	text, err := setMetadata(s.Text, FieldOwner, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err = WithTransaction(root, LockScope{Tasks: []string{id}}, func(tx *Transaction) error { return tx.Put(id, "spec.md", text) }); err != nil {
		t.Fatal(err)
	}
	return id
}
func structuredReport(f ReviewFindings) []byte {
	return []byte("分析中的 QA-999 不是条目。\n```kander-findings\n" + reviewJSON(f) + "```\n")
}
func gateRun(t *testing.T, root string, input ReviewInput, f ReviewFindings) ReviewRun {
	t.Helper()
	run, _, err := PrepareReviewRun(root, input, nil, nil, archiveOriginals(), "test")
	if err != nil {
		t.Fatal(err)
	}
	run.LaunchStatus = "started"
	run.ExecutionStatus = "ok"
	run, err = FinalizeReviewRun(root, run, structuredReport(f))
	if err != nil {
		t.Fatal(err)
	}
	return publishRun(t, root, run.RunID)
}
func assignGate(t *testing.T, root string, run ReviewRun, items map[string][]string) {
	t.Helper()
	if err := AssignReviewFindings(root, ReviewAssignment{RunID: run.RunID, BatchID: run.BatchID, Author: "coordinator", Basis: "按任务范围明确归属", Items: items}); err != nil {
		t.Fatal(err)
	}
}
func emptyFindings() ReviewFindings {
	return ReviewFindings{Findings: []ReviewFinding{}, NonBlocking: []ReviewFinding{}}
}
func gateRecord(t *testing.T, root string, run ReviewRun, id string, f ReviewFinding, status string) ReviewDisposition {
	t.Helper()
	d := ReviewDisposition{RecordID: "record-" + id, RunID: run.RunID, FindingID: f.ID, BatchID: run.BatchID, TaskID: id, Author: "codex", ReportHash: run.Hashes["report.md"], Original: f.Text, Status: status, Basis: "原目标提交源代码与测试记录已逐项验证"}
	s := transactionSnapshot(t, root, id)
	if err := SubmitReviewDisposition(root, d, s.Revision); err != nil {
		t.Fatal(err)
	}
	return d
}
func passRole(run ReviewRun) ReviewRoleConclusion {
	return ReviewRoleConclusion{RunID: run.RunID, PassedAt: run.Commit, Basis: "已独立核查所有结论及跨角色修复范围"}
}
func TestReviewMissingPlanCannotComplete(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "no-plan")
	s := transactionSnapshot(t, root, id)
	updateSnapshot(t, root, s, strings.Replace(s.Text, "## SUMMARY\n\n<FILL_IN>", "## SUMMARY\n\n完成", 1))
	s = transactionSnapshot(t, root, id)
	if _, err := MoveWithOptions(s.Entry, root, "done", MoveOptions{Result: "completed"}); err == nil || !strings.Contains(err.Error(), "plan required") {
		t.Fatalf("missing plan passed: %v", err)
	}
	p, err := ReviewTaskProgress(root, id)
	if err != nil || p.Status != "requirements-needed" {
		t.Fatalf("%+v %v", p, err)
	}
}
func TestReviewMissingAndFailedRequiredRoles(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "never-run", true: "all-failed"}[failed], func(t *testing.T) {
			root := tempBoard(t)
			id := gateCard(t, root, "required")
			gatePlan(t, root, []string{id}, archiveRequirements())
			if failed {
				input := archiveInput([]string{id}, "failed-pm", "PM")
				run, _, err := PrepareReviewRun(root, input, nil, nil, archiveOriginals(), "test")
				if err != nil {
					t.Fatal(err)
				}
				run.ExecutionStatus = "failed"
				run.LaunchStatus = "started"
				run.ExitCode = 1
				run.FailureReason = "failed fixture"
				if _, err = FinalizeReviewRun(root, run, nil); err != nil {
					t.Fatal(err)
				}
				publishRun(t, root, run.RunID)
			}
			if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{}); err == nil {
				t.Fatal("missing roles closed")
			}
			p, err := ReviewTaskProgress(root, id)
			if err != nil || p.Status != "pending" {
				t.Fatalf("valid in-progress state: %+v %v", p, err)
			}
		})
	}
}
func TestStructuredFindingsRejectMissingDuplicateAndProseIDs(t *testing.T) {
	good := emptyFindings()
	f, err := ParseReviewFindings(structuredReport(good))
	if err != nil || len(f.all()) != 0 {
		t.Fatalf("prose ID inferred: %+v %v", f, err)
	}
	for _, report := range []string{"PASS QA-01", "```kander-findings\n{}\n```", "```kander-findings\n{\"FINDINGS\":[],\"NON_BLOCKING\":null}\n```", "```kander-findings\n{\"FINDINGS\":[],\"NON_BLOCKING\":[],\"extra\":1}\n```"} {
		if _, err = ParseReviewFindings([]byte(report)); err == nil {
			t.Fatalf("invalid passed: %s", report)
		}
	}
	item := ReviewFinding{ID: "QA-01", Tier: "medium", Text: "缺陷", Evidence: "x.go:1"}
	good.Findings = []ReviewFinding{item, item}
	if _, err = ParseReviewFindings(structuredReport(good)); err == nil {
		t.Fatal("duplicate ID passed")
	}
}
func TestSharedFindingRequiresEachAuthorAndNoFindingMemberNeedsNoRecord(t *testing.T) {
	root := tempBoard(t)
	a := gateCard(t, root, "author-a")
	b := gateCard(t, root, "author-b")
	c := gateCard(t, root, "no-findings")
	ids := []string{a, b, c}
	gatePlan(t, root, ids, archiveRequirements())
	f := ReviewFinding{ID: "PM-01", Tier: "medium", Text: "共同问题", Evidence: "x.go:10"}
	findings := emptyFindings()
	findings.Findings = []ReviewFinding{f}
	pm := gateRun(t, root, archiveInput(ids, "pm", "PM"), findings)
	qa := gateRun(t, root, archiveInput(ids, "qa", "QA"), emptyFindings())
	assignGate(t, root, pm, map[string][]string{f.ID: {a, b}})
	assignGate(t, root, qa, map[string][]string{})
	gateRecord(t, root, pm, a, f, "rejected")
	if _, err := ReadReviewBatchView(root, "batch"); err == nil {
		t.Fatal("missing cross-card author passed")
	}
	gateRecord(t, root, pm, b, f, "rejected")
	before := transactionSnapshot(t, root, c)
	closed, err := gateClose(t, root, map[string]ReviewRoleConclusion{"PM": passRole(pm), "QA": passRole(qa)})
	if err != nil {
		t.Fatal(err)
	}
	after := transactionSnapshot(t, root, c)
	if after.Entry.State != before.Entry.State || closed.RoleStatuses["PM"] != "PASS" {
		t.Fatalf("no-finding card dispatched or wrong result: %+v", closed)
	}
	for _, id := range ids {
		p, e := ReviewTaskProgress(root, id)
		if e != nil || p.Status != "closed" {
			t.Fatalf("%+v %v", p, e)
		}
	}
	// Removing a member's closure must fail even when checking another member.
	if err = os.Remove(filepath.Join(after.Entry.Path, "reviews", "batches", "batch", "closed.json")); err != nil {
		t.Fatal(err)
	}
	if _, err = ReviewTaskProgress(root, a); err == nil {
		t.Fatal("partial closure publication passed")
	}
}
func TestDispositionOwnershipIdentityAndImmutableOriginal(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "owner")
	other := gateCard(t, root, "other")
	ids := []string{id, other}
	gatePlan(t, root, ids, archiveRequirements())
	f := ReviewFinding{ID: "PM-01", Tier: "medium", Text: "原文", Evidence: "x:1"}
	findings := emptyFindings()
	findings.Findings = []ReviewFinding{f}
	run := gateRun(t, root, archiveInput(ids, "pm", "PM"), findings)
	assignGate(t, root, run, map[string][]string{f.ID: {id}})
	d := ReviewDisposition{RecordID: "one", RunID: run.RunID, FindingID: f.ID, BatchID: run.BatchID, TaskID: id, Author: "codex", ReportHash: run.Hashes["report.md"], Original: f.Text, Status: "rejected", Basis: "具体代码与契约证据"}
	for name, mutate := range map[string]func(*ReviewDisposition){"author": func(d *ReviewDisposition) { d.Author = "coordinator" }, "card": func(d *ReviewDisposition) { d.TaskID = other }, "round": func(d *ReviewDisposition) { d.FindingID = "PM-OLD" }, "original": func(d *ReviewDisposition) { d.Original = "改写" }, "empty-basis": func(d *ReviewDisposition) { d.Basis = "" }, "delegated": func(d *ReviewDisposition) { d.Status = "delegated" }, "pm-waiver": func(d *ReviewDisposition) {
		d.Status = "waived"
		d.Waiver = &ReviewWaiver{Policy: "accepted-risk", Decision: "用户决定"}
	}} {
		t.Run(name, func(t *testing.T) {
			bad := d
			mutate(&bad)
			s := transactionSnapshot(t, root, bad.TaskID)
			if err := SubmitReviewDisposition(root, bad, s.Revision); err == nil {
				t.Fatal("invalid disposition accepted")
			}
		})
	}
	s := transactionSnapshot(t, root, id)
	if err := SubmitReviewDisposition(root, d, s.Revision); err != nil {
		t.Fatal(err)
	}
	original := transactionSnapshot(t, root, id)
	d.Basis = "覆盖作者结论"
	if err := SubmitReviewDisposition(root, d, original.Revision); err == nil {
		t.Fatal("author original overwritten")
	}
}
func TestMechanicalFixAndNonMechanicalRerunGate(t *testing.T) {
	for _, mechanical := range []bool{false, true} {
		t.Run(map[bool]string{false: "logic", true: "documentation"}[mechanical], func(t *testing.T) {
			root := tempBoard(t)
			id := gateCard(t, root, "mechanical")
			requirements := archiveRequirements()
			requirements["QA"] = "N/A: fixture role-only"
			gatePlan(t, root, []string{id}, requirements)
			f := ReviewFinding{ID: "PM-01", Tier: "medium", Text: "缺陷", Evidence: "x:1"}
			findings := emptyFindings()
			findings.Findings = []ReviewFinding{f}
			run := gateRun(t, root, archiveInput([]string{id}, "pm", "PM"), findings)
			assignGate(t, root, run, map[string][]string{f.ID: {id}})
			d := ReviewDisposition{RecordID: "fix", RunID: run.RunID, FindingID: f.ID, BatchID: run.BatchID, TaskID: id, Author: "codex", ReportHash: run.Hashes["report.md"], Original: f.Text, Status: "fixed", Basis: "实际修复", FixCommit: strings.Repeat("c", 40), Verification: "逐句核对与引用检索"}
			if mechanical {
				d.Mechanical = "documentation"
			}
			s := transactionSnapshot(t, root, id)
			if err := SubmitReviewDisposition(root, d, s.Revision); err != nil {
				t.Fatal(err)
			}
			if err := AdvanceReviewBatch(root, ReviewBatchAdvance{BatchID: "batch", ExpectedRevision: 1, Advance: ReviewAdvance{PreviousTarget: run.Commit, Target: d.FixCommit, Reason: "member fix", Deliveries: map[string]string{d.FixCommit: id}}}); err != nil {
				t.Fatal(err)
			}
			conclusion := passRole(run)
			conclusion.PassedAt = d.FixCommit
			_, err := gateClose(t, root, map[string]ReviewRoleConclusion{"PM": conclusion})
			if mechanical && err != nil || !mechanical && err == nil {
				t.Fatalf("mechanical=%v err=%v", mechanical, err)
			}
		})
	}
}
func TestExplicitNAAndLegacyCompletedCard(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "exempt")
	exemptReviewFixture(t, root, id)
	p, err := ReviewTaskProgress(root, id)
	if err != nil || p.Status != "closed" {
		t.Fatalf("%+v %v", p, err)
	}
	legacy := gateCard(t, root, "legacy")
	if err = WithTransaction(root, LockScope{Tasks: []string{legacy}, ExclusiveBoard: true}, func(tx *Transaction) error { return tx.Relocate(legacy, "done") }); err != nil {
		t.Fatal(err)
	}
	p, err = ReviewTaskProgress(root, legacy)
	if err != nil || p.Status != "legacy-untracked" {
		t.Fatalf("%+v %v", p, err)
	}
}
func TestLegacyMappingRequiresOriginalLocations(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "legacy-map")
	run := finalizedRun(t, root, archiveInput([]string{id}, "old", "PM"))
	publishRun(t, root, run.RunID)
	m := LegacyFindingMap{RunID: run.RunID, ReportHash: run.Hashes["report.md"], Author: "human", Basis: "完整逐行人工映射", Complete: true, Findings: emptyFindings()}
	if err := MapLegacyReview(root, m); err == nil {
		t.Fatal("empty legacy map accepted")
	}
	m.Findings.NonBlocking = []ReviewFinding{{ID: "OLD-01", Tier: "suggest", Text: "PASS", Evidence: "report.md:1"}}
	m.Locations = []LegacyFindingLocation{{FindingID: "OLD-01", StartLine: 1, EndLine: 1, Quote: "wrong"}}
	if err := MapLegacyReview(root, m); err == nil {
		t.Fatal("false original location accepted")
	}
	m.Locations[0].Quote = "PASS"
	if err := MapLegacyReview(root, m); err != nil {
		t.Fatal(err)
	}
	bytes, err := ReadReviewOriginal(root, run.RunID, "report.md")
	if err != nil || string(bytes) != "PASS\n" {
		t.Fatalf("original changed %q %v", bytes, err)
	}
}
func TestPlanExtensionUsesClosedCommitNotArbitraryRolePass(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "chain")
	p := gatePlan(t, root, []string{id}, noReviewRequirements())
	// Build an unsealed fixture through the normal creation input on a fresh board.
	root = tempBoard(t)
	id = gateCard(t, root, "chain-open")
	p.PlanID = "open"
	p.TaskIDs = []string{id}
	p.Batches[0].TaskIDs = []string{id}
	p.Sealed = false
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	next := ReviewPlanBatch{BatchID: "second", PreviousBatchID: "batch", TaskIDs: []string{id}, Base: strings.Repeat("b", 40), TargetCommit: strings.Repeat("c", 40), Requirements: noReviewRequirements()}
	x := ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: 1, Batch: &next, Seal: true, Author: "coordinator", Basis: "下一批交付"}
	if err := ExtendReviewPlan(root, x); err == nil {
		t.Fatal("unclosed predecessor accepted")
	}
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{}); err != nil {
		t.Fatal(err)
	}
	next.Base = strings.Repeat("a", 40)
	if err := ExtendReviewPlan(root, x); err == nil {
		t.Fatal("old role/base commit accepted")
	}
	next.Base = strings.Repeat("b", 40)
	if err := ExtendReviewPlan(root, x); err != nil {
		t.Fatal(err)
	}
	progress, err := ReviewTaskProgress(root, id)
	if err != nil || !reflect.DeepEqual(progress.Pending, []string{"second"}) {
		t.Fatalf("%+v %v", progress, err)
	}
}
func TestReviewJSONRoundTrip(t *testing.T) {
	// Unknown fields cannot silently become an empty disposition or plan.
	var d ReviewDisposition
	if DecodeReviewJSON([]byte(`{"status":"rejected","unknown":1}`), &d) == nil {
		t.Fatal("unknown field ignored")
	}
	b, _ := json.Marshal(emptyFindings())
	var f ReviewFindings
	if err := DecodeReviewJSON(b, &f); err != nil {
		t.Fatal(err)
	}
}

func TestDuplicateJSONKeysCannotEraseFindings(t *testing.T) {
	report := []byte("```kander-findings\n{\"FINDINGS\":[{\"id\":\"PM-01\",\"tier\":\"medium\",\"text\":\"bug\",\"evidence\":\"x:1\"}],\"FINDINGS\":[],\"NON_BLOCKING\":[]}\n```")
	if _, err := ParseReviewFindings(report); err == nil {
		t.Fatal("duplicate key erased a finding")
	}
}
func TestPlanCannotHideUnplannedRunsOrDeletedPointer(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "unplanned")
	run := finalizedRun(t, root, archiveInput([]string{id}, "unplanned", "PM"))
	publishRun(t, root, run.RunID)
	exemptReviewFixture(t, root, id)
	if _, err := ReviewTaskProgress(root, id); err == nil {
		t.Fatal("N/A plan hid existing unplanned runs")
	}
	if err := os.Remove(control(root, "groups", reviewControlGroup, "task-plans", id+".json")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReviewTaskProgress(root, id); err == nil {
		t.Fatal("deleted plan pointer became legacy")
	}
}
func TestConcurrentDispositionAppendUsesRevisionAndPreservesOriginals(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "concurrent-disposition")
	gatePlan(t, root, []string{id}, archiveRequirements())
	f := ReviewFinding{ID: "PM-01", Tier: "medium", Text: "原始发现", Evidence: "x:1"}
	findings := emptyFindings()
	findings.Findings = []ReviewFinding{f}
	run := gateRun(t, root, archiveInput([]string{id}, "pm", "PM"), findings)
	assignGate(t, root, run, map[string][]string{f.ID: {id}})
	s := transactionSnapshot(t, root, id)
	d := ReviewDisposition{RunID: run.RunID, BatchID: run.BatchID, FindingID: f.ID, TaskID: id, Author: "codex", ReportHash: run.Hashes["report.md"], Original: f.Text, Status: "rejected", Basis: "具体触发条件经复核不成立"}
	results := make(chan error, 2)
	for _, record := range []string{"first", "second"} {
		go func(record string) {
			copy := d
			copy.RecordID = record
			results <- SubmitReviewDisposition(root, copy, s.Revision)
		}(record)
	}
	count := 0
	for i := 0; i < 2; i++ {
		if <-results == nil {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("CAS winners=%d", count)
	}
	view, err := ReadReviewBatchView(root, "batch")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Runs[0].Records) != 1 {
		t.Fatal("lost or duplicate author records")
	}
}

func TestFailedRunRequiresExplicitSuccessfulReplacement(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "failed-replaced")
	requirements := archiveRequirements()
	requirements["QA"] = "N/A: fixture"
	gatePlan(t, root, []string{id}, requirements)
	input := archiveInput([]string{id}, "failed", "PM")
	run, _, err := PrepareReviewRun(root, input, nil, nil, archiveOriginals(), "test")
	if err != nil {
		t.Fatal(err)
	}
	run.ExecutionStatus = "failed"
	run.LaunchStatus = "started"
	run.ExitCode = 1
	run.FailureReason = "失败原件"
	if _, err = FinalizeReviewRun(root, run, nil); err != nil {
		t.Fatal(err)
	}
	publishRun(t, root, run.RunID)
	success := gateRun(t, root, archiveInput([]string{id}, "replacement", "PM"), emptyFindings())
	assignGate(t, root, success, map[string][]string{})
	view, err := ReadReviewBatchView(root, "batch")
	if err != nil {
		t.Fatal(err)
	}
	r := ReviewCloseRequest{BatchID: "batch", ExpectedRevision: view.Batch.Revision, ViewHash: ReviewViewDigest(view), Author: "coordinator", Roles: map[string]ReviewRoleConclusion{"PM": passRole(success)}}
	if _, _, err = ReviewClosureEdges(view, r); err == nil {
		t.Fatal("failed run silently ignored")
	}
	r.ResolvedFailures = map[string]string{"failed": "replacement"}
	if _, _, err = ReviewClosureEdges(view, r); err != nil {
		t.Fatal(err)
	}
}
func TestSecurityWaiverPreservesNonPassStatus(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "security-waiver")
	requirements := noReviewRequirements()
	requirements["CSA"] = "required"
	gatePlan(t, root, []string{id}, requirements)
	f := ReviewFinding{ID: "CSA-01", Tier: "high", Text: "已证实的安全问题", Evidence: "x:1"}
	findings := emptyFindings()
	findings.Findings = []ReviewFinding{f}
	run := gateRun(t, root, archiveInput([]string{id}, "csa", "CSA"), findings)
	assignGate(t, root, run, map[string][]string{f.ID: {id}})
	d := ReviewDisposition{RecordID: "accepted-risk", RunID: run.RunID, BatchID: run.BatchID, FindingID: f.ID, TaskID: id, Author: "codex", ReportHash: run.Hashes["report.md"], Original: f.Text, Status: "waived", Basis: "已独立确认触发及影响", Waiver: &ReviewWaiver{Policy: "accepted-risk", Decision: "用户针对本项明确接受风险的记录"}}
	s := transactionSnapshot(t, root, id)
	if err := SubmitReviewDisposition(root, d, s.Revision); err != nil {
		t.Fatal(err)
	}
	c, err := gateClose(t, root, map[string]ReviewRoleConclusion{"CSA": passRole(run)})
	if err != nil || c.RoleStatuses["CSA"] != "accepted-risk" {
		t.Fatalf("waiver became PASS %+v %v", c.RoleStatuses, err)
	}
}
func TestPartialRunPublicationCannotCloseEvenWithNoFindings(t *testing.T) {
	root := tempBoard(t)
	a := gateCard(t, root, "partial-a")
	b := gateCard(t, root, "partial-b")
	gatePlan(t, root, []string{a, b}, archiveRequirements())
	run := gateRun(t, root, archiveInput([]string{a, b}, "pm", "PM"), emptyFindings())
	assignGate(t, root, run, map[string][]string{})
	s := transactionSnapshot(t, root, b)
	if err := os.Remove(filepath.Join(s.Entry.Path, "reviews", run.RunID, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadReviewBatchView(root, "batch"); err == nil {
		t.Fatal("partial archive accepted")
	}
}

func TestIncrementalIDsRequireActualPredecessorLineage(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "lineage")
	gatePlan(t, root, []string{id}, archiveRequirements())
	f := ReviewFinding{ID: "PM-01", Tier: "medium", Text: "原始 finding", Evidence: "x:1"}
	findings := emptyFindings()
	findings.Findings = []ReviewFinding{f}
	first := gateRun(t, root, archiveInput([]string{id}, "first", "PM"), findings)
	input := archiveInput([]string{id}, "next", "PM")
	input.PreviousRunID = first.RunID
	input.ReviewedCommit = first.Commit
	input.Commit = strings.Repeat("c", 40)
	if err := AdvanceReviewBatch(root, ReviewBatchAdvance{BatchID: "batch", ExpectedRevision: 1, Advance: ReviewAdvance{PreviousTarget: first.Commit, Target: input.Commit, Reason: "fix", Deliveries: map[string]string{input.Commit: id}}}); err != nil {
		t.Fatal(err)
	}
	next := gateRun(t, root, input, findings)
	if err := AssignReviewFindings(root, ReviewAssignment{RunID: next.RunID, BatchID: "batch", Author: "coordinator", Basis: "范围", Items: map[string][]string{f.ID: {id}}}); err == nil {
		t.Fatal("reused unlinked ID accepted")
	}
	if err := WithTransaction(root, reviewScope([]string{id}, true), func(tx *Transaction) error {
		f.Lineage = &FindingRef{RunID: "foreign", FindingID: "PM-01"}
		findings.Findings = []ReviewFinding{f}
		if validateFindingLineage(tx, next, findings) == nil {
			t.Fatal("cross-run lineage accepted")
		}
		f.Lineage = &FindingRef{RunID: first.RunID, FindingID: "PM-missing"}
		findings.Findings = []ReviewFinding{f}
		if validateFindingLineage(tx, next, findings) == nil {
			t.Fatal("prose/missing ID lineage accepted")
		}
		f.Lineage = &FindingRef{RunID: first.RunID, FindingID: "PM-01"}
		findings.Findings = []ReviewFinding{f}
		return validateFindingLineage(tx, next, findings)
	}); err != nil {
		t.Fatal(err)
	}
}
