// Package boardtest builds review evidence on a board for tests of the
// packages that consume it: a plan, a published review run with findings, and
// the assignment of those findings to a card. It uses the exported board
// entrances only.
package boardtest

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

// Reviewed names the evidence ReviewedFinding built.
type Reviewed struct {
	BatchID   string
	RunID     string
	FindingID string
	// Commit is the commit of the run and the current target of the batch; it is
	// the base of a fix dispatch on this evidence.
	Commit string
}

// ReviewedFinding gives card task, which must be in working or review and have
// the report language en, a sealed review plan with one batch, one successful
// PMQA run that reports one finding, and the assignment of that finding to the
// card. Every ID is derived from the task ID.
func ReviewedFinding(t testing.TB, root, task string) Reviewed {
	t.Helper()
	r := Reviewed{BatchID: "batch-" + task, RunID: "run-" + task, FindingID: "PM-01", Commit: strings.Repeat("b", 40)}
	base := strings.Repeat("a", 40)
	err := board.CreateReviewPlan(root, board.ReviewPlan{
		Schema: 1, Sealed: true, PlanID: "plan-" + task, Author: "coordinator", Basis: "fixture of review evidence", CWD: "/repo", ReportLanguage: "en",
		TaskIDs: []string{task},
		Batches: []board.ReviewPlanBatch{{BatchID: r.BatchID, TaskIDs: []string{task}, Base: base, TargetCommit: r.Commit, Requirements: map[string]string{"PMQA": "required", "Security": "N/A: fixture"}}},
	})
	if err != nil {
		t.Fatalf("review plan: %v", err)
	}
	input := board.ReviewInput{RunID: r.RunID, BatchID: r.BatchID, TaskIDs: []string{task}, Role: "PMQA", Reviewer: "codex", Model: "model", Effort: "high", CWD: "/repo", Base: base, Commit: r.Commit, ReportLanguage: "en"}
	originals := map[string][]byte{"task-context.md": []byte("task\n"), "review-context.md": []byte("context\n")}
	run, _, err := board.PrepareReviewRun(root, input, nil, nil, originals, "test")
	if err != nil {
		t.Fatalf("prepare run: %v", err)
	}
	run.LaunchStatus, run.ExecutionStatus = "started", "ok"
	findings := board.ReviewFindings{Findings: []board.ReviewFinding{{ID: r.FindingID, Tier: "medium", Text: "needs a fix", Evidence: "file.go:1"}}, NonBlocking: []board.ReviewFinding{}}
	data, err := json.MarshalIndent(findings, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if run, err = board.FinalizeReviewRun(root, run, []byte("```kander-findings\n"+string(data)+"\n```\n")); err != nil {
		t.Fatalf("finalize run: %v", err)
	}
	if _, failures, err := board.PublishReviewRun(root, run.RunID); err != nil || len(failures) > 0 {
		t.Fatalf("publish run: %v %v", err, failures)
	}
	assignment := board.ReviewAssignment{RunID: r.RunID, BatchID: r.BatchID, Author: "coordinator", Basis: "assigned by scope", Items: map[string][]string{r.FindingID: {task}}}
	if err = board.AssignReviewFindings(root, assignment); err != nil {
		t.Fatalf("assign findings: %v", err)
	}
	return r
}
