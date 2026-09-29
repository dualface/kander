//go:build windows

package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

const windowsEmptyStructuredReview = "```kander-findings\n{\"FINDINGS\":[],\"NON_BLOCKING\":[]}\n```"

func windowsEvidenceJSON(t *testing.T, h *windowsHarness, name string, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.root, name+".json")
	if err = os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func windowsEvidenceOK(t *testing.T, args ...string) string {
	t.Helper()
	code, out, stderr := captureRun(t, args)
	if code != 0 {
		t.Fatalf("%v: %d %s", args, code, stderr)
	}
	return out
}

// windowsCWDSpellings returns other spellings of one Windows directory: forward slashes, a lower-case
// drive letter, and a trailing separator.
func windowsCWDSpellings(dir string) (slash, lower, trailing string) {
	slash = filepath.ToSlash(dir)
	lower = strings.ToLower(dir[:1]) + dir[1:]
	return slash, lower, dir + `\`
}

func TestWindowsReviewEvidenceAcceptsCWDSpellings(t *testing.T) {
	h := newWindowsHarness(t, "codex")
	root := filepath.Join(h.root, "board")
	for _, state := range board.States {
		if err := os.MkdirAll(filepath.Join(root, state), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(board.EnvBoardDir, root)
	id := "20260929-plan-cwd-task"
	if err := os.Mkdir(filepath.Join(root, "working", id), 0o700); err != nil {
		t.Fatal(err)
	}
	text := "# CWD\n\n- TYPE: Bug\n- SIZE: small\n- LANGUAGE: en\n- OWNER: codex\n- TASK_BRANCH: task\n\n## GOAL\n\nGoal\n"
	if err := os.WriteFile(filepath.Join(root, "working", id, "spec.md"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(h.repo)
	if err != nil {
		t.Fatal(err)
	}
	slash, lower, trailing := windowsCWDSpellings(canonical)

	requirements := map[string]string{"PMQA": "required", "Security": "N/A: project"}
	p := board.ReviewPlan{Schema: 1, Sealed: true, PlanID: "cwd-plan", Author: "coordinator", Basis: "slash spelling", CWD: slash, ReportLanguage: "en", TaskIDs: []string{id}, Batches: []board.ReviewPlanBatch{{BatchID: "batch", TaskIDs: []string{id}, Base: h.base, TargetCommit: h.head, Requirements: requirements}}}
	windowsEvidenceOK(t, "plan", lower, windowsEvidenceJSON(t, h, "plan", p))
	stored, err := board.ReadReviewPlan(root, "cwd-plan")
	if err != nil || stored.CWD != canonical {
		t.Fatalf("plan CWD = %q, want canonical %q (%v)", stored.CWD, canonical, err)
	}

	t.Setenv("FAKE_REVIEW_REPORT", windowsEmptyStructuredReview)
	windowsEvidenceOK(t, "codex", "--task", id, "--run-id", "stable", "--batch-id", "batch", slash, h.base, h.head, "PMQA", "original goal")
	windowsEvidenceOK(t, "assign", trailing, windowsEvidenceJSON(t, h, "assign", board.ReviewAssignment{RunID: "stable", BatchID: "batch", Author: "coordinator", Basis: "explicit zero findings", Items: map[string][]string{}}))
	windowsEvidenceOK(t, "aggregate", slash, "batch")
	v, err := board.ReadReviewBatchView(root, "batch")
	if err != nil {
		t.Fatal(err)
	}
	r := board.ReviewCloseRequest{BatchID: "batch", ExpectedRevision: v.Batch.Revision, ViewHash: board.ReviewViewDigest(v), Author: "coordinator", Roles: map[string]board.ReviewRoleConclusion{"PMQA": {RunID: "stable", PassedAt: h.head, Basis: "verified original report"}}}
	windowsEvidenceOK(t, "close", trailing, windowsEvidenceJSON(t, h, "close", r))
	if problems := board.CheckReviewGate(root, []string{id}); len(problems) > 0 {
		t.Fatalf("gate after close: %+v", problems)
	}

	other := filepath.Join(h.root, "other")
	if err := os.Mkdir(other, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRepo(t, other, "init", "-q", "-b", "main")
	p.PlanID, p.Batches[0].BatchID = "cwd-other", "batch-other"
	code, _, stderr := captureRun(t, []string{"plan", other, windowsEvidenceJSON(t, h, "plan-other", p)})
	if code == 0 || !strings.Contains(stderr, "plan CWD mismatch") {
		t.Fatalf("plan for another directory accepted: %d %s", code, stderr)
	}
}
