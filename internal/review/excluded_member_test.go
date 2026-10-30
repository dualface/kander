//go:build unix

package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

// The review command itself, not only the board, continues a batch for the
// members that remain: the --task set shrinks, and the advance may name a
// delivery of the member that left.
func TestIncrementalReviewContinuesAfterAMemberLeft(t *testing.T) {
	h, root, _ := archiveHarness(t)
	ids := excludedMemberPlan(t, h, root)
	commandOK(t, "codex", "--task", ids[0], "--task", ids[1], "--batch-id", "batch", "--run-id", "first", h.repo, h.base, h.head, "PMQA", "Frozen lineage goal")
	commandOK(t, "assign", h.repo, dispositionJSON(t, h, "first-assignment", board.ReviewAssignment{RunID: "first", BatchID: "batch", Author: "main", Basis: "no findings", Items: map[string][]string{}}))
	commandOK(t, "aggregate", h.repo, "batch")

	s, err := board.ReadSnapshot(root, ids[1])
	if err != nil {
		t.Fatal(err)
	}
	if _, err = board.MoveWithOptions(s.Entry, root, "trash", board.MoveOptions{Result: "trashed", Reason: "cancelled", Decision: "decision-1"}); err != nil {
		t.Fatal(err)
	}
	target := commitFile(t, h.repo, "fix.txt", "fix scope\n", "fix")
	advance := filepath.Join(t.TempDir(), "advance.json")
	data, err := json.Marshal(board.ReviewAdvance{PreviousTarget: h.head, Target: target, Reason: "delivery of the member that left", Deliveries: map[string]string{target: ids[1]}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(advance, data, 0o600); err != nil {
		t.Fatal(err)
	}
	commandOK(t, "codex", "--task", ids[0], "--batch-id", "batch", "--run-id", "second", "--previous-run-id", "first", "--advance-file", advance, h.repo, h.base, target, "PMQA", "Frozen lineage goal")
	run, err := board.ReadReviewRun(root, "second")
	if err != nil || run.ExecutionStatus != "ok" || len(run.TaskIDs) != 1 {
		t.Fatalf("%+v %v", run, err)
	}
}

func excludedMemberPlan(t *testing.T, h *reviewHarness, root string) []string {
	t.Helper()
	ids := []string{"20260907-archive-test-task", "20260907-other-test-task"}
	for _, id := range ids {
		dir := filepath.Join(root, "working", id)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		text := "# Lineage fixture\n\n- TYPE: Chore\n- SIZE: small\n- LANGUAGE: zh-CN\n- TASK_GROUP: 20260907-lineage-group\n- OWNER: codex\n- STARTED_AT: 2026-01-01 00:00\n- TASK_BRANCH: fixture\n\n## GOAL\n\nVerify lineage recovery.\n\n## SUMMARY\n\nFixture delivery verified.\n"
		if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	requirements := map[string]string{"PMQA": "required", "Security": "N/A: fixture"}
	p := board.ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan", Author: "main", Basis: "lineage recovery", CWD: h.repo, ReportLanguage: "zh-CN", TaskIDs: ids, Batches: []board.ReviewPlanBatch{{BatchID: "batch", TaskIDs: ids, Base: h.base, TargetCommit: h.head, Requirements: requirements}}}
	commandOK(t, "plan", h.repo, dispositionJSON(t, h, "plan", p))
	t.Setenv("FAKE_CODEX_REPORT", "```kander-findings\n{\"FINDINGS\":[],\"NON_BLOCKING\":[]}\n```")
	return ids
}

// A run prepared before the exit recovers with its own frozen members.
func TestInterruptedIncrementalRunRecoversAfterAMemberLeft(t *testing.T) {
	h, root, _ := archiveHarness(t)
	ids := excludedMemberPlan(t, h, root)
	first := []string{"codex", "--task", ids[0], "--task", ids[1], "--batch-id", "batch", "--run-id", "first", h.repo, h.base, h.head, "PMQA", "Frozen lineage goal"}
	commandOK(t, first...)
	commandOK(t, "assign", h.repo, dispositionJSON(t, h, "first-assignment", board.ReviewAssignment{RunID: "first", BatchID: "batch", Author: "main", Basis: "no findings", Items: map[string][]string{}}))
	target := commitFile(t, h.repo, "fix.txt", "fix scope\n", "fix")
	commandOK(t, "advance", h.repo, dispositionJSON(t, h, "advance", board.ReviewBatchAdvance{BatchID: "batch", ExpectedRevision: 1, Advance: board.ReviewAdvance{PreviousTarget: h.head, Target: target, Reason: "member delivery", Deliveries: map[string]string{target: ids[0]}}}))

	args := []string{"codex", "--task", ids[0], "--task", ids[1], "--batch-id", "batch", "--run-id", "second", "--previous-run-id", "first", h.repo, h.base, target, "PMQA", "Frozen lineage goal"}
	options, remaining, err := parseArchiveOptions(args)
	if err != nil {
		t.Fatal(err)
	}
	agent, rest, err := splitAgentArgs(remaining)
	if err != nil {
		t.Fatal(err)
	}
	if rest, err = hydrateIncremental(root, options, rest, false); err != nil {
		t.Fatal(err)
	}
	ctx, err := validateContextMode(agent, rest, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if fresh, err := archiveInvocation(&ctx, options, rest, root, board.FindingsSchemaReceiver); err != nil || !fresh {
		t.Fatalf("%v %v", fresh, err)
	}
	if err = ctx.archive.phase("launching", "unknown"); err != nil {
		t.Fatal(err)
	}
	s, err := board.ReadSnapshot(root, ids[1])
	if err != nil {
		t.Fatal(err)
	}
	if _, err = board.MoveWithOptions(s.Entry, root, "trash", board.MoveOptions{Result: "trashed", Reason: "cancelled", Decision: "decision-1"}); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := captureRun(t, args); code != 2 || strings.Contains(stderr, "incremental batch/members mismatch") {
		t.Fatalf("%d %s", code, stderr)
	}
	run, err := board.ReadReviewRun(root, "second")
	if err != nil || run.ExecutionStatus != "interrupted" || !slices.Equal(run.SkippedMembers, ids[1:]) {
		t.Fatalf("%+v %v", run, err)
	}
}
