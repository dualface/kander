//go:build unix

package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

func archiveHarness(t *testing.T) (*reviewHarness, string, []string) {
	t.Helper()
	return archiveHarnessFor(t, newCodexHarness(t), "codex")
}

func archiveHarnessFor(t *testing.T, h *reviewHarness, agent string) (*reviewHarness, string, []string) {
	t.Helper()
	root := filepath.Join(h.root, "board")
	for _, state := range board.States {
		if err := os.MkdirAll(filepath.Join(root, state), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(board.EnvBoardDir, root)
	id := "20260907-archive-test-task"
	dir := filepath.Join(root, "working", id)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	text := "# 审核\n\n- TYPE: Chore\n- SIZE: small\n- LANGUAGE: zh-CN\n- TASK_BRANCH: task\n\n## GOAL\n\n目标\n"
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	requirements := filepath.Join(h.root, "requirements.json")
	if err := os.WriteFile(requirements, []byte("{\"PM\":\"required\",\"QA\":\"required\",\"CSA\":\"N/A: project\",\"Hacker\":\"N/A: project\"}"), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{agent, "--task", id, "--task", id, "--run-id", "stable", "--batch-id", "batch", "--requirements-file", requirements, h.repo, h.base, h.head, "PM", "原始目标"}
	return h, root, args
}
func TestArchiveCLIOutputRetryAndLanguage(t *testing.T) {
	h, root, args := archiveHarness(t)
	t.Setenv("FAKE_CODEX_REPORT", "FAIL: 原始审核意见")
	code, out, stderr := captureRun(t, args)
	if code != 0 || out != "FAIL: 原始审核意见\n" {
		t.Fatalf("%d %q %s", code, out, stderr)
	}
	run, err := board.ReadReviewRun(root, "stable")
	if err != nil {
		t.Fatal(err)
	}
	if run.ExecutionStatus != "ok" || run.SemanticStatus != "unassessed" || run.ReportLanguage != "zh-CN" || len(run.TaskIDs) != 1 {
		t.Fatalf("%+v", run)
	}
	if err = board.ReviewPublicationComplete(root, "stable"); err != nil {
		t.Fatal(err)
	}
	raw, err := board.ReadReviewOriginal(root, "stable", "report.md")
	if err != nil || string(raw) != out {
		t.Fatalf("%q %v", raw, err)
	}
	prompt, err := board.ReadReviewOriginal(root, "stable", "prompt.txt")
	if err != nil || !strings.Contains(string(prompt), "zh-CN") {
		t.Fatalf("%s %v", prompt, err)
	}
	// The retry may run after the worktree advanced or the CLI disappeared.
	if err = os.Remove(h.fake); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(h.repo, "dirty.txt"), []byte("user changes"), 0600); err != nil {
		t.Fatal(err)
	}
	code, replayed, stderr := captureRun(t, args)
	if code != 0 || replayed != out {
		t.Fatalf("%d %q %s", code, replayed, stderr)
	}
	s, err := board.ReadSnapshot(root, run.TaskIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	indexes, err := board.ParseReviewIndexes(s.Text)
	if err != nil || len(indexes) != 1 {
		t.Fatalf("%v %v", indexes, err)
	}
	args[len(args)-1] = "changed input"
	code, _, _ = captureRun(t, args)
	if code == 0 {
		t.Fatal("same ID accepted changed input")
	}
}
func TestArchiveCLIFailurePreservesEvidence(t *testing.T) {
	_, root, args := archiveHarness(t)
	t.Setenv("FAKE_CODEX_FAIL", "1")
	code, _, stderr := captureRun(t, args)
	if code != 3 {
		t.Fatalf("%d %s", code, stderr)
	}
	run, err := board.ReadReviewRun(root, "stable")
	if err != nil {
		t.Fatal(err)
	}
	if run.ExecutionStatus != "failed" || run.LaunchStatus != "started" || run.FailureReason == "" {
		t.Fatalf("%+v", run)
	}
	raw, err := board.ReadReviewOriginal(root, "stable", "error.log")
	if err != nil || !strings.Contains(string(raw), "fake codex failure") {
		t.Fatalf("%q %v", raw, err)
	}
	if _, err = board.ReadReviewOriginal(root, "stable", "report.md"); err == nil {
		t.Fatal("fabricated report")
	}
	if err = board.ReviewPublicationComplete(root, "stable"); err != nil {
		t.Fatal(err)
	}
}
func TestArchiveCLITimeoutAndMalformedOutput(t *testing.T) {
	for _, kind := range []string{"timeout", "empty"} {
		t.Run(kind, func(t *testing.T) {
			h, root, args := archiveHarness(t)
			if kind == "timeout" {
				writeFake(t, h.fake, "#!/bin/sh\ncat >/dev/null\nsleep 5\n")
				t.Setenv("CODEX_REVIEW_MAX_RUNTIME_SECONDS", "1")
			} else {
				writeFake(t, h.fake, "#!/bin/sh\ncat >/dev/null\nexit 0\n")
			}
			code, _, stderr := captureRun(t, args)
			if code == 0 {
				t.Fatalf("accepted %s", stderr)
			}
			run, err := board.ReadReviewRun(root, "stable")
			if err != nil {
				t.Fatal(err)
			}
			if run.ExecutionStatus != "failed" || run.Phase != "finalized" {
				t.Fatalf("%+v", run)
			}
			if err = board.ReviewPublicationComplete(root, "stable"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestArchiveRejectsBeforeReviewerAndNoIntent(t *testing.T) {
	h, root, args := archiveHarness(t)
	args[2] = "20260907-missing-task"
	code, _, _ := captureRun(t, args)
	if code == 0 {
		t.Fatal("missing card accepted")
	}
	if _, err := os.Stat(h.argvLog); !os.IsNotExist(err) {
		t.Fatal("reviewer ran")
	}
	_, exists, err := board.LookupReviewRun(root, "stable")
	if err != nil || exists {
		t.Fatalf("false run %v %v", exists, err)
	}
}
func TestArchiveOptionsAndRequirementsValidation(t *testing.T) {
	for _, args := range [][]string{
		{"--run-id", "a", "/repo"}, {"--task", "20260907-a-task", "/repo"},
		{"--task", "20260907-a-task", "--batch-id", "../../escape", "/repo"},
		{"--task", "20260907-a-task", "--batch-id", "b", "--run-id", "a", "--run-id", "a", "/repo"},
		{"--unknown", "value", "/repo"},
	} {
		if _, _, err := parseArchiveOptions(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	h, root, args := archiveHarness(t)
	path := filepath.Join(h.root, "requirements.json")
	if err := os.WriteFile(path, []byte("{} broken"), 0600); err != nil {
		t.Fatal(err)
	}
	code, _, _ := captureRun(t, args)
	if code == 0 {
		t.Fatal("invalid requirements accepted")
	}
	_, exists, err := board.LookupReviewRun(root, "stable")
	if err != nil || exists {
		t.Fatalf("%v %v", exists, err)
	}
}
func TestArchiveExplicitRecoveryDoesNotRerun(t *testing.T) {
	h, root, args := archiveHarness(t)
	options, remaining, err := parseArchiveOptions(args)
	if err != nil {
		t.Fatal(err)
	}
	agent, rest, err := splitAgentArgs(remaining)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := validateContextMode(agent, rest, false)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := archiveInvocation(&ctx, options, rest, root)
	if err != nil || !fresh {
		t.Fatalf("%v %v", fresh, err)
	}
	if err = ctx.archive.phase("launching", "unknown"); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(h.fake); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := captureRun(t, args)
	if code != 2 {
		t.Fatalf("%d %s", code, stderr)
	}
	run, err := board.ReadReviewRun(root, "stable")
	if err != nil {
		t.Fatal(err)
	}
	if run.ExecutionStatus != "interrupted" || run.LaunchStatus != "unknown" {
		t.Fatalf("%+v", run)
	}
	if _, err = board.ReadReviewOriginal(root, "stable", "report.md"); err == nil {
		t.Fatal("recovery fabricated report")
	}
}
func TestArchiveBatchAdvanceRangeAttribution(t *testing.T) {
	h, root, args := archiveHarness(t)
	t.Setenv("FAKE_CODEX_REPORT", "```kander-findings\n{\"FINDINGS\":[],\"NON_BLOCKING\":[]}\n```")
	options, _, err := parseArchiveOptions(args)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := board.ReadSnapshot(root, options.tasks[0])
	if err != nil {
		t.Fatal(err)
	}
	args[len(args)-1] = snapshot.Entry.Document
	code, _, stderr := captureRun(t, args)
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	if err = board.AssignReviewFindings(root, board.ReviewAssignment{RunID: "stable", BatchID: "batch", Author: "coordinator", Basis: "empty structured report", Items: map[string][]string{}}); err != nil {
		t.Fatal(err)
	}
	next := commitFile(t, h.repo, "fix.txt", "fix", "fix")
	advance := board.ReviewAdvance{PreviousTarget: h.head, Target: next, Reason: "member fix", Deliveries: map[string]string{next: "20260907-foreign-task"}}
	path := filepath.Join(h.root, "advance.json")
	snapshot, err = board.ReadSnapshot(root, options.tasks[0])
	if err != nil {
		t.Fatal(err)
	}
	if err = board.UpdateDocument(root, options.tasks[0], board.UpdateOptions{Document: "spec.md", Text: snapshot.Text + "\n## IMPLEMENTATION\n\nFix delivery recorded\n", ExpectedRevision: snapshot.Revision}); err != nil {
		t.Fatal(err)
	}
	nextArgs := []string{"codex", "--task", options.tasks[0], "--batch-id", "batch", "--run-id", "fixed", "--previous-run-id", "stable", "--advance-file", path, h.repo, h.base, next, "PM", snapshot.Entry.Document, "fix context", h.head}
	writeAdvance := func() {
		t.Helper()
		data, err := json.Marshal(advance)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeAdvance()
	code, _, stderr = captureRun(t, nextArgs)
	if code != 2 || !strings.Contains(stderr, "unattributed or foreign delivery: "+next) {
		t.Fatalf("foreign delivery accepted: %d %s", code, stderr)
	}
	if _, exists, err := board.LookupReviewRun(root, "fixed"); err != nil || exists {
		t.Fatalf("foreign delivery persisted: %v %v", exists, err)
	}
	advance.Deliveries[next] = options.tasks[0]
	writeAdvance()
	code, _, stderr = captureRun(t, nextArgs)
	if code != 2 || !strings.Contains(stderr, "batch binding conflict") {
		t.Fatalf("changed live spec accepted: %d %s", code, stderr)
	}
	if _, exists, err := board.LookupReviewRun(root, "fixed"); err != nil || exists {
		t.Fatalf("changed spec persisted: %v %v", exists, err)
	}
	nextArgs[len(nextArgs)-3] = filepath.Join(snapshot.Entry.Path, "reviews", "stable", "task-context.md")
	code, _, stderr = captureRun(t, nextArgs)
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	if err = board.ReviewPublicationComplete(root, "fixed"); err != nil {
		t.Fatal(err)
	}
	run, err := board.ReadReviewRun(root, "fixed")
	if err != nil || run.Commit != next || run.PreviousRunID != "stable" || run.ReviewedCommit != h.head {
		t.Fatalf("%+v %v", run, err)
	}
}

func TestArchiveLaunchFailureRecordsNotStarted(t *testing.T) {
	h, root, args := archiveHarness(t)
	writeFake(t, h.fake, "#!/nonexistent/kander-test-interpreter\n")
	code, _, stderr := captureRun(t, args)
	if code != 127 {
		t.Fatalf("%d %s", code, stderr)
	}
	run, err := board.ReadReviewRun(root, "stable")
	if err != nil {
		t.Fatal(err)
	}
	if run.ExecutionStatus != "not_started" || run.LaunchStatus != "not_started" || run.FailureReason == "" {
		t.Fatalf("%+v", run)
	}
	if err = board.ReviewPublicationComplete(root, "stable"); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveWorktreeRejectionKeepsReportWithoutPassing(t *testing.T) {
	h, root, args := archiveHarness(t)
	t.Setenv("FAKE_CODEX_TAMPER", filepath.Join(h.repo, "a.txt"))
	code, out, stderr := captureRun(t, args)
	if code != 2 || !strings.Contains(out, "REPORT BODY") {
		t.Fatalf("%d %q %s", code, out, stderr)
	}
	run, err := board.ReadReviewRun(root, "stable")
	if err != nil {
		t.Fatal(err)
	}
	if run.ExecutionStatus != "failed" || run.SemanticStatus != "unassessed" || !strings.Contains(run.FailureReason, "modified") {
		t.Fatalf("%+v", run)
	}
	if err = board.ReviewPublicationComplete(root, "stable"); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveCleanupFailureCannotPublishOK(t *testing.T) {
	h, root, args := archiveHarness(t)
	script := strings.Replace(fakeCodex, "exit 0\n", `dir=${prompt%/*}
i=0
while [ "$i" -lt 4100 ]; do
    : > "$dir/cleanup-$i"
    i=$((i + 1))
done
exit 0
`, 1)
	writeFake(t, h.fake, script)
	code, out, stderr := captureRun(t, args)
	if code != 2 || !strings.Contains(out, "REPORT BODY") || !strings.Contains(stderr, "clean") {
		t.Fatalf("%d %q %s", code, out, stderr)
	}
	run, err := board.ReadReviewRun(root, "stable")
	if err != nil {
		t.Fatal(err)
	}
	if run.ExecutionStatus != "failed" || run.FailureReason == "" {
		t.Fatalf("%+v", run)
	}
	if err = board.ReviewPublicationComplete(root, "stable"); err != nil {
		t.Fatal(err)
	}
}

func TestStandaloneReviewIgnoresInvalidBoard(t *testing.T) {
	h := newCodexHarness(t)
	obstruction := filepath.Join(h.root, "not-a-board")
	if err := os.WriteFile(obstruction, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(board.EnvBoardDir, obstruction)
	code, _, stderr := h.review("codex", "PM", "goal")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	data, err := os.ReadFile(obstruction)
	if err != nil || string(data) != "unchanged" {
		t.Fatalf("%s %v", data, err)
	}
}

func TestArchiveLeftoverProcessesRejectButKeepRawOutput(t *testing.T) {
	h, root, args := archiveHarness(t)
	script := strings.Replace(fakeCodex, "exit 0\n", "(sleep 20) >/dev/null 2>&1 &\nexit 0\n", 1)
	writeFake(t, h.fake, script)
	code, out, stderr := captureRun(t, args)
	if code != 2 || !strings.Contains(out, "REPORT BODY") || !strings.Contains(stderr, "background") {
		t.Fatalf("%d %q %s", code, out, stderr)
	}
	run, err := board.ReadReviewRun(root, "stable")
	if err != nil {
		t.Fatal(err)
	}
	if run.ExecutionStatus != "failed" || !strings.Contains(run.FailureReason, "background") {
		t.Fatalf("%+v", run)
	}
	if err = board.ReviewPublicationComplete(root, "stable"); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveInvalidUTF8IsRawEvidenceNotValidReport(t *testing.T) {
	h, root, args := archiveHarness(t)
	script := strings.Replace(fakeCodex, "printf '%s\\n' \"$"+"{FAKE_CODEX_REPORT:-REPORT BODY}\" > \"$out\"", "printf '\\377' > \"$out\"", 1)
	writeFake(t, h.fake, script)
	code, out, stderr := captureRun(t, args)
	if code == 0 {
		t.Fatalf("invalid UTF-8 accepted: %q %s", out, stderr)
	}
	data, err := board.ReadReviewOriginal(root, "stable", "output.raw")
	if err != nil || len(data) != 1 || data[0] != 0xff {
		t.Fatalf("%v %v", data, err)
	}
	if _, err = board.ReadReviewOriginal(root, "stable", "report.md"); err == nil {
		t.Fatal("invalid UTF-8 report published")
	}
}

func TestArchiveJSONReviewerReplayBytes(t *testing.T) {
	for _, report := range []string{"original report", "original report\\n"} {
		t.Run(report, func(t *testing.T) {
			h, root, args := archiveHarnessFor(t, newClaudeHarness(t), "claude")
			t.Setenv("FAKE_CLAUDE_REPORT", report)
			code, first, stderr := captureRun(t, args)
			if code != 0 {
				t.Fatalf("%d %s", code, stderr)
			}
			saved, err := board.ReadReviewOriginal(root, "stable", "report.md")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.Remove(h.fake); err != nil {
				t.Fatal(err)
			}
			code, replay, stderr := captureRun(t, args)
			if code != 0 || first != replay || first != string(saved) {
				t.Fatalf("%d first=%q replay=%q saved=%q %s", code, first, replay, saved, stderr)
			}
		})
	}
}
