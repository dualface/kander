//go:build unix

package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

func TestGroupReclaimRetainsPlanFailuresAndOriginalAuthorsThroughCLI(t *testing.T) {
	h, root, _ := archiveHarness(t)
	ids := []string{"20260907-archive-test-task", "20260907-other-test-task"}
	// Establish a two-member group before any producer records exist.
	for _, id := range ids {
		dir := filepath.Join(root, "working", id)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		text := "# Group review\n\n- TYPE: Chore\n- SIZE: small\n- LANGUAGE: zh-CN\n- TASK_GROUP: 20260907-review-test-group\n- TASK_BRANCH: task\n- OWNER: codex\n- STARTED_AT: 2026-01-01 00:00\n\n## GOAL\n\nReview recovery\n\n## SUMMARY\n\nFixture delivery verified.\n"
		if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	requirements := map[string]string{"PM": "required", "QA": "N/A: fixture", "CSA": "N/A: fixture", "Hacker": "N/A: fixture"}
	p := board.ReviewPlan{Schema: 1, Sealed: true, PlanID: "group-cycle", Author: "coordinator", Basis: "both cards share every review obligation", CWD: h.repo, ReportLanguage: "zh-CN", TaskIDs: ids, Batches: []board.ReviewPlanBatch{{BatchID: "batch", TaskIDs: ids, Base: h.base, TargetCommit: h.head, Requirements: requirements}}}
	commandOK(t, "plan", h.repo, dispositionJSON(t, h, "plan", p))
	oldPlan, err := board.ReadReviewPlan(root, p.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"codex", "--task", ids[0], "--task", ids[1], "--batch-id", "batch", "--run-id", "bad", h.repo, h.base, h.head, "PM", "group goal"}
	t.Setenv("FAKE_CODEX_REPORT", "invalid report")
	if code, _, _ := captureRun(t, args); code == 0 {
		t.Fatal("invalid report passed")
	}
	failed, err := board.ReadReviewRun(root, "bad")
	if err != nil {
		t.Fatal(err)
	}
	f := board.ReviewFinding{ID: "PM-01", Tier: "medium", Text: "Potential defect", Evidence: "base.txt:1"}
	data, _ := json.Marshal(board.ReviewFindings{Findings: []board.ReviewFinding{f}, NonBlocking: []board.ReviewFinding{}})
	t.Setenv("FAKE_CODEX_REPORT", "```kander-findings\n"+string(data)+"\n```")
	args[8] = "pm"
	commandOK(t, args...)
	commandOK(t, "assign", h.repo, dispositionJSON(t, h, "assign", board.ReviewAssignment{RunID: "pm", BatchID: "batch", Author: "coordinator", Basis: "finding concerns first member", Items: map[string][]string{f.ID: {ids[0]}}}))
	run, err := board.ReadReviewRun(root, "pm")
	if err != nil {
		t.Fatal(err)
	}
	d := board.ReviewDisposition{RecordID: "old-author", RunID: "pm", FindingID: f.ID, BatchID: "batch", TaskID: ids[0], Author: "codex", ReportHash: run.Hashes["report.md"], Original: f.Text, Status: "confirmed", Basis: "Original author investigation"}
	s, err := board.ReadSnapshot(root, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	commandOK(t, "disposition", h.repo, dispositionJSON(t, h, "old-author", d), itoa(int(s.Revision)))
	originalPath := filepath.Join(s.Entry.Path, "reviews", "pm", "dispositions", "old-author.json")
	original, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if board.RunMove([]string{id, "review"}) != 0 {
			t.Fatal("move review failed")
		}
	}
	if board.RunMove([]string{ids[0], "working", "--owner", "claude"}) != 0 {
		t.Fatal("supported reclaim failed")
	}
	var progress board.ReviewProgress
	for _, id := range ids {
		out := commandOK(t, "progress", h.repo, id)
		if err = json.Unmarshal([]byte(out), &progress); err != nil {
			t.Fatal(err)
		}
		if progress.Status != "requirements-needed" || len(progress.RebindCycles) != 1 {
			t.Fatalf("new cycle not recoverable: %+v", progress)
		}
		if board.RunMove([]string{id, "done", "--result", "completed"}) == 0 {
			t.Fatal("changed cycle completed before requirements rebind")
		}
	}
	reset := p
	reset.PlanID = "discard-failures"
	reset.Batches = []board.ReviewPlanBatch{{BatchID: "empty", TaskIDs: ids, Base: h.base, TargetCommit: h.head, Requirements: map[string]string{"PM": "N/A: reset", "QA": "N/A: reset", "CSA": "N/A: reset", "Hacker": "N/A: reset"}}}
	if code, _, _ := captureRun(t, []string{"plan", h.repo, dispositionJSON(t, h, "reset", reset)}); code == 0 {
		t.Fatal("new plan dropped unresolved earlier cycle")
	}
	x := board.ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: oldPlan.Revision, RebindCycles: progress.RebindCycles, Author: "coordinator", Basis: "Explicit owner handoff; carry every existing obligation"}
	commandOK(t, "extend-plan", h.repo, dispositionJSON(t, h, "rebind", x))
	afterPlan, err := board.ReadReviewPlan(root, p.PlanID)
	if err != nil || !reflect.DeepEqual(afterPlan.Batches, oldPlan.Batches) || !reflect.DeepEqual(afterPlan.TaskIDs, oldPlan.TaskIDs) || afterPlan.Revision != oldPlan.Revision+1 {
		t.Fatalf("rebind changed obligations: %+v %v", afterPlan, err)
	}
	x.ExpectedRevision = afterPlan.Revision
	if code, _, _ := captureRun(t, []string{"extend-plan", h.repo, dispositionJSON(t, h, "same-cycle", x)}); code == 0 {
		t.Fatal("same cycle rebind accepted")
	}
	for i, id := range ids {
		s, err = board.ReadSnapshot(root, id)
		if err != nil || s.Entry.State != []string{"working", "review"}[i] {
			t.Fatalf("rebind moved member: %+v %v", s.Entry, err)
		}
		if got := commandOK(t, "progress", h.repo, id); !strings.Contains(got, `"status": "pending"`) {
			t.Fatal(got)
		}
	}
	s, err = board.ReadSnapshot(root, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	oldAttempt := d
	oldAttempt.RecordID = "old-owner-again"
	oldAttempt.PreviousRecordID = d.RecordID
	if code, _, _ := captureRun(t, []string{"disposition", h.repo, dispositionJSON(t, h, "old-attempt", oldAttempt), itoa(int(s.Revision))}); code == 0 {
		t.Fatal("former owner wrote after handoff")
	}
	d.RecordID, d.PreviousRecordID, d.Author, d.Status, d.Basis = "new-author", "old-author", "claude", "rejected", "Successor independently verifies source and disproves initial claim"
	commandOK(t, "disposition", h.repo, dispositionJSON(t, h, "new-author", d), itoa(int(s.Revision)))
	v, err := board.ReadReviewBatchView(root, "batch")
	if err != nil {
		t.Fatal(err)
	}
	r := board.ReviewCloseRequest{BatchID: "batch", ExpectedRevision: v.Batch.Revision, ViewHash: board.ReviewViewDigest(v), Author: "coordinator", Roles: map[string]board.ReviewRoleConclusion{"PM": {RunID: "pm", PassedAt: h.head, Basis: "author evidence independently verified"}}}
	if code, _, _ := captureRun(t, []string{"close", h.repo, dispositionJSON(t, h, "close", r)}); code == 0 {
		t.Fatal("rebind discarded old failed run")
	}
	r.ResolvedFailures = map[string]string{"bad": "pm"}
	commandOK(t, "close", h.repo, dispositionJSON(t, h, "close", r))
	for _, id := range ids {
		if board.RunMove([]string{id, "done", "--result", "completed"}) != 0 {
			t.Fatal("group member cannot finish recovered cycle")
		}
	}
	s, err = board.ReadSnapshot(root, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(filepath.Join(s.Entry.Path, "reviews", "pm", "dispositions", "old-author.json"))
	if err != nil || string(saved) != string(original) {
		t.Fatal("handoff rewrote original author's conclusion")
	}
	afterFailure, err := board.ReadReviewRun(root, "bad")
	if err != nil || !reflect.DeepEqual(afterFailure, failed) {
		t.Fatal("handoff rewrote failed run")
	}
	if problems := board.CheckReviewGate(root, ids); len(problems) != 0 {
		t.Fatal(problems)
	}
}
