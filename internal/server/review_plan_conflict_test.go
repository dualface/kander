package server_test

import (
	"net/http"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

// A plan that reuses the batch ID of another plan is refused as board_rejected
// with HTTP 422, and the message names the batch, the plan that holds it, and the
// fields in which the new plan differs.
func TestReviewPlanBatchIDConflictNamesItsHolder(t *testing.T) {
	english(t)
	project := newFixture(t).project("host-a")
	first := todoTask(t, project, "plan-conflict-first")
	second := todoTask(t, project, "plan-conflict-second")
	grant := func(id, execution string) client.Claim {
		g, err := project.Claim(t.Context(), "", id, api.ClaimRequest{ClientID: "host-a", ExecutionID: execution, Owner: "codex"})
		if err != nil {
			t.Fatal(err)
		}
		return client.Claim{Token: g.Token, Generation: g.Claim.Generation}
	}
	firstProof, secondProof := grant(first.TaskID, "exec-first"), grant(second.TaskID, "exec-second")
	requirements := map[string]string{"PMQA": "N/A: conflict fixture", "Security": "N/A: repository policy"}
	plan := func(planID, task string) board.ReviewPlan {
		return board.ReviewPlan{Schema: 1, PlanID: planID, Author: "coordinator", Basis: "conflict fixture", CWD: "project:" + project.ID(), ReportLanguage: "en", TaskIDs: []string{task}, Batches: []board.ReviewPlanBatch{{BatchID: "shared-batch", TaskIDs: []string{task}, Base: "N/A", TargetCommit: "N/A", Requirements: requirements}}}
	}
	if _, err := project.ReviewPlan(t.Context(), "plan-first", plan("plan-first", first.TaskID), &firstProof); err != nil {
		t.Fatal(err)
	}
	_, err := project.ReviewPlan(t.Context(), "plan-second", plan("plan-second", second.TaskID), &secondProof)
	refusal(t, err, api.CodeBoardRejected, http.StatusUnprocessableEntity, "board.review_evidence_invalid",
		"existing batch/plan mismatch", "batch shared-batch", "plan plan-first (members: "+first.TaskID+")", "differs from it in: task_ids")
}
