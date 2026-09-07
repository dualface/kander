//go:build unix

package review

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

const emptyStructuredReview = "```kander-findings\n{\"FINDINGS\":[],\"NON_BLOCKING\":[]}\n```"

func dispositionJSON(t *testing.T, h *reviewHarness, name string, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.root, name+".json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestDispositionCLIClosesOnlyCompleteRolesAtActualHead(t *testing.T) {
	h, root, args := archiveHarness(t)
	id := "20260907-archive-test-task"
	requirements := map[string]string{"PM": "required", "QA": "required", "CSA": "N/A: project", "Hacker": "N/A: project"}
	p := board.ReviewPlan{Schema: 1, Sealed: true, PlanID: "cycle", Author: "coordinator", Basis: "本轮任务契约与仓库规则", CWD: h.repo, ReportLanguage: "zh-CN", TaskIDs: []string{id}, Batches: []board.ReviewPlanBatch{{BatchID: "batch", TaskIDs: []string{id}, Base: h.base, TargetCommit: h.head, Requirements: requirements}}}
	code, _, stderr := captureRun(t, []string{"plan", h.repo, dispositionJSON(t, h, "plan", p)})
	if code != 0 {
		t.Fatalf("plan %d %s", code, stderr)
	}
	t.Setenv("FAKE_CODEX_REPORT", emptyStructuredReview)
	code, _, stderr = captureRun(t, args)
	if code != 0 {
		t.Fatalf("run %d %s", code, stderr)
	}
	assign := func(run string) {
		t.Helper()
		a := board.ReviewAssignment{RunID: run, BatchID: "batch", Author: "coordinator", Basis: "完整报告无 finding", Items: map[string][]string{}}
		code, _, stderr := captureRun(t, []string{"assign", h.repo, dispositionJSON(t, h, run+"-assignment", a)})
		if code != 0 {
			t.Fatalf("assign %d %s", code, stderr)
		}
	}
	assign("stable")
	v, err := board.ReadReviewBatchView(root, "batch")
	if err != nil {
		t.Fatal(err)
	}
	r := board.ReviewCloseRequest{BatchID: "batch", ExpectedRevision: v.Batch.Revision, ViewHash: board.ReviewViewDigest(v), Author: "coordinator", Roles: map[string]board.ReviewRoleConclusion{"PM": {RunID: "stable", PassedAt: h.head, Basis: "已独立验证"}}}
	code, _, stderr = captureRun(t, []string{"close", h.repo, dispositionJSON(t, h, "close", r)})
	if code == 0 {
		t.Fatal("missing QA closed")
	}
	for i := range args {
		if args[i] == "stable" {
			args[i] = "qa"
		}
		if args[i] == "PM" {
			args[i] = "QA"
		}
	}
	code, _, stderr = captureRun(t, args)
	if code != 0 {
		t.Fatalf("QA %d %s", code, stderr)
	}
	assign("qa")
	v, err = board.ReadReviewBatchView(root, "batch")
	if err != nil {
		t.Fatal(err)
	}
	r.ViewHash = board.ReviewViewDigest(v)
	r.Roles["QA"] = board.ReviewRoleConclusion{RunID: "qa", PassedAt: h.head, Basis: "已核查架构、行为与验证"}
	file := dispositionJSON(t, h, "close", r)
	if err = os.WriteFile(filepath.Join(h.repo, "uncommitted"), []byte("user change"), 0600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr = captureRun(t, []string{"close", h.repo, file})
	if code == 0 || !strings.Contains(stderr, "clean worktree") {
		t.Fatalf("dirty head accepted: %d %s", code, stderr)
	}
	if err = os.Remove(filepath.Join(h.repo, "uncommitted")); err != nil {
		t.Fatal(err)
	}
	code, out, stderr := captureRun(t, []string{"close", h.repo, file})
	if code != 0 || !strings.Contains(out, `"PM": "PASS"`) {
		t.Fatalf("close %d %s %s", code, out, stderr)
	}
	var closure board.ReviewClosure
	if err = json.Unmarshal([]byte(out), &closure); err != nil {
		t.Fatal(err)
	}
	commitFile(t, h.repo, "later.txt", "later delivery", "later delivery")
	if err = VerifyClosedReviewGit(context.Background(), h.repo, closure); err != nil {
		t.Fatal("historical closure failed at later HEAD", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = VerifyClosedReviewGit(ctx, h.repo, closure); err == nil {
		t.Fatal("canceled recovery accepted")
	}
	code, out, stderr = captureRun(t, []string{"progress", h.repo, id})
	if code != 0 || !strings.Contains(out, `"status": "closed"`) {
		t.Fatalf("progress %d %s %s", code, out, stderr)
	}
}
func TestIncrementalTaskReviewReadsFailingAuthorEvidenceAndRejectsWrongSources(t *testing.T) {
	h, root, args := archiveHarness(t)
	id := "20260907-archive-test-task"
	s, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = board.WithTransaction(root, board.LockScope{Tasks: []string{id}}, func(tx *board.Transaction) error { return tx.Put(id, "spec.md", s.Text+"\n- OWNER: codex\n") }); err != nil {
		t.Fatal(err)
	}
	f := board.ReviewFinding{ID: "PM-01", Tier: "medium", Text: "必须修复的原始缺陷", Evidence: "base.txt:1"}
	report := board.ReviewFindings{Findings: []board.ReviewFinding{f}, NonBlocking: []board.ReviewFinding{}}
	bytes, _ := json.Marshal(report)
	t.Setenv("FAKE_CODEX_REPORT", "```kander-findings\n"+string(bytes)+"\n```")
	code, _, stderr := captureRun(t, args)
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	if err = board.AssignReviewFindings(root, board.ReviewAssignment{RunID: "stable", BatchID: "batch", Author: "coordinator", Basis: "按范围归属", Items: map[string][]string{f.ID: {id}}}); err != nil {
		t.Fatal(err)
	}
	run, err := board.ReadReviewRun(root, "stable")
	if err != nil {
		t.Fatal(err)
	}
	d := board.ReviewDisposition{RecordID: "author-confirmed", RunID: run.RunID, BatchID: run.BatchID, FindingID: f.ID, TaskID: id, Author: "codex", ReportHash: run.Hashes["report.md"], Original: f.Text, Status: "confirmed", Basis: "执行端核实具体触发路径"}
	s, err = board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	code, _, stderr = captureRun(t, []string{"disposition", h.repo, dispositionJSON(t, h, "author", d), itoa(int(s.Revision))})
	if code != 0 {
		t.Fatalf("author %d %s", code, stderr)
	}
	context, err := board.ReviewIncrementalContext(root, "stable")
	if err != nil || !strings.Contains(string(context), d.Basis) {
		t.Fatalf("semantic FAIL context rejected: %s %v", context, err)
	}
	next := commitFile(t, h.repo, "fix.txt", "fix", "fix")
	advance := board.ReviewAdvance{PreviousTarget: h.head, Target: next, Reason: "本批任务修复", Deliveries: map[string]string{next: id}}
	advanceFile := dispositionJSON(t, h, "advance", advance)
	t.Setenv("FAKE_CODEX_REPORT", emptyStructuredReview)
	nextArgs := []string{"codex", "--task", id, "--batch-id", "batch", "--run-id", "next", "--previous-run-id", "stable", "--advance-file", advanceFile, h.repo, h.base, next, "PM", "原始目标", "实现由其他 Agent 完成。\n独立核对补充材料。"}
	code, _, stderr = captureRun(t, nextArgs)
	if code != 0 {
		t.Fatalf("automatic incremental %d %s", code, stderr)
	}
	prompt, err := board.ReadReviewOriginal(root, "next", "prompt.txt")
	if err != nil || !strings.Contains(string(prompt), f.Text) || !strings.Contains(string(prompt), d.Basis) || !strings.Contains(string(prompt), "PREVIOUS_RUN_ID: stable") || !strings.Contains(string(prompt), nextArgs[len(nextArgs)-1]) {
		t.Fatalf("missing verbatim evidence %s %v", prompt, err)
	}
	if strings.Contains(string(prompt), "KANDER_AUTOMATIC_CONTEXT_BYTES:") || strings.Contains(string(prompt), "Caller supplemental context (verbatim;") {
		t.Fatal("internal framing exposed in prompt")
	}
	frozen, err := board.ReadReviewOriginal(root, "next", "review-context.md")
	if err != nil || !strings.HasPrefix(string(frozen), "KANDER_AUTOMATIC_CONTEXT_BYTES:") {
		t.Fatal("frozen context framing changed")
	}
	// A retry uses the original context, even though the batch now contains next.
	code, _, stderr = captureRun(t, nextArgs)
	if code != 0 {
		t.Fatalf("incremental replay %d %s", code, stderr)
	}
	changedSupplement := append([]string{}, nextArgs...)
	changedSupplement[len(changedSupplement)-1] = "changed caller context"
	if code, _, _ := captureRun(t, changedSupplement); code == 0 {
		t.Fatal("changed supplemental context replay accepted")
	}
	wrong := append([]string{}, nextArgs...)
	wrong[4] = "wrong-batch"
	if code, _, _ := captureRun(t, wrong); code == 0 {
		t.Fatal("wrong identity accepted")
	}
	s, err = board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(s.Entry.Path, "reviews", "stable", "dispositions", d.RecordID+".json"), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = board.ReviewIncrementalContext(root, "stable"); err == nil {
		t.Fatal("tampered author original accepted")
	}
}
func TestClosureRejectsUnrelatedGitFixCommit(t *testing.T) {
	h := newCodexHarness(t)
	if err := verifyGitEdge(h.repo, board.ReviewGitEdge{Ancestor: h.head, Descendant: h.base}); err == nil {
		t.Fatal("reversed ancestry accepted")
	}
	if err := verifyGitEdge(h.repo, board.ReviewGitEdge{Ancestor: strings.Repeat("f", 40), Descendant: h.head}); err == nil {
		t.Fatal("missing fix object accepted")
	}
}

func TestNonGitExplicitNAPlanCanCompleteReviewGate(t *testing.T) {
	h, root, _ := archiveHarness(t)
	id := "20260907-archive-test-task"
	cwd := filepath.Join(h.root, "non-git")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	requirements := map[string]string{"PM": "N/A: review disabled by user", "QA": "N/A: review disabled by user", "CSA": "N/A: review disabled by user", "Hacker": "N/A: review disabled by user"}
	p := board.ReviewPlan{Schema: 1, Sealed: true, PlanID: "non-git", Author: "owner", Basis: "用户关闭审核，非 Git 项目", CWD: cwd, ReportLanguage: "zh-CN", TaskIDs: []string{id}, Batches: []board.ReviewPlanBatch{{BatchID: "non-git", TaskIDs: []string{id}, Base: "N/A", TargetCommit: "N/A", Requirements: requirements}}}
	if code, _, stderr := captureRun(t, []string{"plan", cwd, dispositionJSON(t, h, "non-git-plan", p)}); code != 0 {
		t.Fatalf("plan %d %s", code, stderr)
	}
	view, err := board.ReadReviewBatchView(root, "non-git")
	if err != nil {
		t.Fatal(err)
	}
	r := board.ReviewCloseRequest{BatchID: "non-git", ExpectedRevision: view.Batch.Revision, ViewHash: board.ReviewViewDigest(view), Author: "owner", Roles: map[string]board.ReviewRoleConclusion{}}
	code, out, stderr := captureRun(t, []string{"close", cwd, dispositionJSON(t, h, "non-git-close", r)})
	if code != 0 || !strings.Contains(out, `"not_applicable"`) {
		t.Fatalf("non-Git N/A failed: %d %s %s", code, out, stderr)
	}
}
