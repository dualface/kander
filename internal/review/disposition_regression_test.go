//go:build unix

package review

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

func commandOK(t *testing.T, args ...string) string {
	t.Helper()
	code, out, stderr := captureRun(t, args)
	if code != 0 {
		t.Fatalf("%v: %d %s", args, code, stderr)
	}
	return out
}

func TestMalformedReviewCanBeInterpretedThroughCLI(t *testing.T) {
	h, root, args := archiveHarness(t)
	id := "20260907-archive-test-task"
	snapshot, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = board.UpdateDocument(root, id, board.UpdateOptions{Document: "spec.md", ExpectedRevision: snapshot.Revision, Text: snapshot.Text + "\n## SUMMARY\n\nFixture delivery verified.\n"}); err != nil {
		t.Fatal(err)
	}
	if board.RunMove([]string{id, "review"}) != 0 || board.RunMove([]string{id, "working", "--owner", "codex"}) != 0 {
		t.Fatal("claim fixture")
	}
	requirements := map[string]string{"PMQA": "required", "Security": "N/A: project"}
	p := board.ReviewPlan{Schema: 1, Sealed: true, PlanID: "cycle", Author: "coordinator", Basis: "report interpretation", CWD: h.repo, ReportLanguage: "zh-CN", TaskIDs: []string{id}, Batches: []board.ReviewPlanBatch{{BatchID: "batch", TaskIDs: []string{id}, Base: h.base, TargetCommit: h.head, Requirements: requirements}}}
	commandOK(t, "plan", h.repo, dispositionJSON(t, h, "plan", p))
	t.Setenv("FAKE_CODEX_REPORT", emptyStructuredReview+"No findings in either section.")
	code, output, stderr := captureRun(t, args)
	if code != 0 || !strings.Contains(stderr, "interpret") || !strings.Contains(stderr, "stable") {
		t.Fatalf("formatting became execution failure: %d %s", code, stderr)
	}
	run, err := board.ReadReviewRun(root, "stable")
	if err != nil || run.ExecutionStatus != "ok" || run.ExitCode != 0 {
		t.Fatalf("execution facts: %+v %v", run, err)
	}
	if code, retry, _ := captureRun(t, args); code != 0 || retry != output {
		t.Fatal("same-ID retry lost original")
	}
	if out := commandOK(t, "aggregate", h.repo, "batch"); !strings.Contains(out, `"findings_status": "pending"`) {
		t.Fatal(out)
	}
	if out := commandOK(t, "progress", h.repo, id); !strings.Contains(out, "interpretation:stable") {
		t.Fatal(out)
	}
	if code := board.RunMove([]string{id, "done", "--result", "completed"}); code == 0 {
		t.Fatal("pending batch completed")
	}
	m := board.ReviewInterpretation{RunID: "stable", ReportHash: run.Hashes["report.md"], Author: "receiver", Basis: "Complete report read: both arrays are empty and the explicit conclusion states no findings.", Complete: true, Findings: board.ReviewFindings{Findings: []board.ReviewFinding{}, NonBlocking: []board.ReviewFinding{}}, NoFindings: &board.ReviewReportQuote{StartLine: 2, EndLine: 3, Quote: "{\"FINDINGS\":[],\"NON_BLOCKING\":[]}\n```No findings in either section."}}
	input := dispositionJSON(t, h, "interpret", m)
	commandOK(t, "interpret", h.repo, input)
	commandOK(t, "interpret", h.repo, input)
	commandOK(t, "assign", h.repo, dispositionJSON(t, h, "assignment", board.ReviewAssignment{RunID: "stable", BatchID: "batch", Author: "receiver", Basis: "explicit zero findings", Items: map[string][]string{}}))
	v, err := board.ReadReviewBatchView(root, "batch")
	if err != nil {
		t.Fatal(err)
	}
	r := board.ReviewCloseRequest{BatchID: "batch", ExpectedRevision: v.Batch.Revision, ViewHash: board.ReviewViewDigest(v), Author: "coordinator", Roles: map[string]board.ReviewRoleConclusion{"PMQA": {RunID: "stable", PassedAt: h.head, Basis: "verified original and interpretation"}}}
	commandOK(t, "close", h.repo, dispositionJSON(t, h, "close", r))
	if code := board.RunMove([]string{id, "done", "--result", "completed"}); code != 0 {
		t.Fatal("interpreted batch cannot finish")
	}
	after, err := board.ReadReviewRun(root, "stable")
	if err != nil || !reflect.DeepEqual(run, after) {
		t.Fatalf("original execution metadata rewritten: %v", err)
	}
	saved, err := board.ReadReviewOriginal(root, "stable", "report.md")
	if err != nil || string(saved) != output {
		t.Fatal("interpretation rewrote original report")
	}
}

func TestAdvanceAndExtensionRejectOtherWorktree(t *testing.T) {
	h, _, _ := archiveHarness(t)
	id := "20260907-archive-test-task"
	requirements := map[string]string{"PMQA": "N/A: fixture", "Security": "N/A: fixture"}
	p := board.ReviewPlan{Schema: 1, PlanID: "cycle", Author: "coordinator", Basis: "CWD binding", CWD: h.repo, ReportLanguage: "zh-CN", TaskIDs: []string{id}, Batches: []board.ReviewPlanBatch{{BatchID: "batch", TaskIDs: []string{id}, Base: h.base, TargetCommit: h.head, Requirements: requirements}}}
	commandOK(t, "plan", h.repo, dispositionJSON(t, h, "plan", p))
	other := filepath.Join(h.root, "other-worktree")
	if _, _, code, err := gitCommand([]string{"worktree", "add", "--detach", other, h.head}, h.repo, ""); err != nil || code != 0 {
		t.Fatalf("worktree: %d %v", code, err)
	}
	next := commitFile(t, other, "fix.txt", "fix", "fix")
	x := board.ReviewBatchAdvance{BatchID: "batch", ExpectedRevision: 1, Advance: board.ReviewAdvance{PreviousTarget: h.head, Target: next, Reason: "fix", Deliveries: map[string]string{next: id}}}
	for _, args := range [][]string{{"advance", other, dispositionJSON(t, h, "advance", x)}, {"extend-plan", other, dispositionJSON(t, h, "extension", board.ReviewPlanExtension{PlanID: "cycle", ExpectedRevision: 1, Seal: true, Author: "coordinator", Basis: "seal"})}} {
		code, _, stderr := captureRun(t, args)
		if code == 0 || !strings.Contains(stderr, "plan CWD mismatch") {
			t.Fatalf("other worktree accepted: %d %s", code, stderr)
		}
	}
}

func TestMechanicalAssessmentRequiresActualGitScope(t *testing.T) {
	h := newCodexHarness(t)
	next := commitFile(t, h.repo, "README.md", "Document the actual behavior.\n", "docs")
	item := board.ReviewFinding{ID: "PM-01", Tier: "medium", Text: "Documentation mismatch"}
	d := board.ReviewDisposition{RecordID: "fix", RunID: "pm", FindingID: item.ID, TaskID: "20260907-mechanical-task", Status: "fixed", Mechanical: "documentation", ReportHash: strings.Repeat("a", 64), FixCommit: next}
	v := board.ReviewBatchView{Runs: []board.ReviewRunView{{Run: board.ReviewRun{ReviewInput: board.ReviewInput{RunID: "pm", Commit: h.head, Role: "PM"}}, Findings: &board.ReviewFindings{Findings: []board.ReviewFinding{item}}, Records: []board.ReviewDisposition{d}}}}
	r := board.ReviewCloseRequest{Author: "main", Roles: map[string]board.ReviewRoleConclusion{"PM": {RunID: "pm"}}}
	if _, err := verifyMechanicalGit(h.repo, v, r); err == nil {
		t.Fatal("author tag alone bypassed re-review")
	}
	patch, _, code, err := gitCommand([]string{"diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--binary", "--full-index", "--no-color", h.head, next, "--", ":(literal)README.md"}, h.repo, "")
	if err != nil || code != 0 {
		t.Fatal(err)
	}
	a := board.ReviewMechanicalAssessment{RecordID: d.RecordID, Finding: board.FindingRef{RunID: "pm", FindingID: item.ID}, TaskID: d.TaskID, Author: "main", Category: "documentation", ReportedCategory: "", ReportHash: d.ReportHash, FixCommit: next, Basis: "Main agent confirms definition despite absent reviewer label", Facts: "Compared changed sentence with actual behavior; no logic change", Paths: []string{"README.md"}, DiffHash: board.ReviewDigest([]byte(patch))}
	r.Mechanical = []board.ReviewMechanicalAssessment{a}
	if _, err = verifyMechanicalGit(h.repo, v, r); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*board.ReviewMechanicalAssessment){func(a *board.ReviewMechanicalAssessment) { a.DiffHash = strings.Repeat("b", 64) }, func(a *board.ReviewMechanicalAssessment) { a.Paths = []string{"missing.md"} }, func(a *board.ReviewMechanicalAssessment) { a.Facts = "" }, func(a *board.ReviewMechanicalAssessment) { a.ReportedCategory = "documentation" }} {
		bad := a
		mutate(&bad)
		r.Mechanical = []board.ReviewMechanicalAssessment{bad}
		if _, err = verifyMechanicalGit(h.repo, v, r); err == nil {
			t.Fatal("unverified mechanical claim accepted")
		}
	}
	if _, err = os.Stat(filepath.Join(h.repo, "README.md")); err != nil {
		t.Fatal(err)
	}
}

func TestAdvanceWithoutPlanExplainsRecovery(t *testing.T) {
	h, _, args := archiveHarness(t)
	t.Setenv("FAKE_CODEX_REPORT", emptyStructuredReview)
	commandOK(t, args...)
	next := commitFile(t, h.repo, "fix.txt", "fix", "fix")
	x := board.ReviewBatchAdvance{BatchID: "batch", ExpectedRevision: 1, Advance: board.ReviewAdvance{PreviousTarget: h.head, Target: next, Reason: "member fix", Deliveries: map[string]string{next: "20260907-archive-test-task"}}}
	code, _, stderr := captureRun(t, []string{"advance", h.repo, dispositionJSON(t, h, "advance-unplanned", x)})
	if code == 0 || !strings.Contains(stderr, "batch has no review plan; create its plan before advancing") {
		t.Fatalf("%d %s", code, stderr)
	}
	id := "20260907-archive-test-task"
	p := board.ReviewPlan{Schema: 1, Sealed: true, PlanID: "adopt", Author: "main", Basis: "adopt existing batch before advance", CWD: h.repo, ReportLanguage: "zh-CN", TaskIDs: []string{id}, Batches: []board.ReviewPlanBatch{{BatchID: "batch", TaskIDs: []string{id}, Base: h.base, TargetCommit: h.head, Requirements: map[string]string{"PMQA": "required", "Security": "N/A: project"}}}}
	commandOK(t, "plan", h.repo, dispositionJSON(t, h, "adopt", p))
	commandOK(t, "advance", h.repo, dispositionJSON(t, h, "advance-planned", x))
}
