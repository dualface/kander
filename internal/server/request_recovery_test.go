package server_test

import (
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
)

func TestCollaborationAnswerCASAndSettledStateSurviveRestart(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	task := todoTask(t, a, "request-cas")
	grant, err := claim(t, a, task.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	request, err := b.CreateRequest(t.Context(), "request-cas", task.TaskID, api.NewCollabRequest{Kind: api.RequestFix, Message: "fix the failing case", TargetGeneration: grant.Claim.Generation})
	if err != nil {
		t.Fatal(err)
	}
	proof := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	done, err := a.AnswerRequest(t.Context(), "finish-request", task.TaskID, request.ID, api.CollabAnswer{ExpectedRevision: request.Revision, Status: api.RequestDone, Note: "fixed and verified"}, proof)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.AnswerRequest(t.Context(), "late-delivery", task.TaskID, request.ID, api.CollabAnswer{ExpectedRevision: request.Revision, Status: api.RequestAccepted}, proof)
	expectCode(t, err, api.CodeRevisionConflict)
	_, err = a.AnswerRequest(t.Context(), "reopen", task.TaskID, request.ID, api.CollabAnswer{ExpectedRevision: done.Revision, Status: api.RequestAccepted}, proof)
	expectCode(t, err, api.CodeRevisionConflict)
	dir, clock := f.dir, f.clock
	f.stop()
	f = start(t, dir, clock)
	a = f.project("host-a")
	replay, err := a.AnswerRequest(t.Context(), "finish-request", task.TaskID, request.ID, api.CollabAnswer{ExpectedRevision: request.Revision, Status: api.RequestDone, Note: "fixed and verified"}, proof)
	if err != nil || replay.Revision != done.Revision || replay.Status != api.RequestDone {
		t.Fatalf("answer receipt changed: %+v %v", replay, err)
	}
}
func TestTakeoverSupersedesRequestsInTheClaimTransaction(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	task := todoTask(t, a, "request-handoff")
	grant, err := claim(t, a, task.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	request, err := b.CreateRequest(t.Context(), "", task.TaskID, api.NewCollabRequest{Kind: api.RequestReview, Message: "confirm material", TargetGeneration: grant.Claim.Generation})
	if err != nil {
		t.Fatal(err)
	}
	f.clock.advance(90 * time.Second)
	if _, err := b.Takeover(t.Context(), "", task.TaskID, api.ClaimRequest{ClientID: "host-b", ExecutionID: "successor", Owner: "codex", Decision: "test explicit handoff", ExpectedGeneration: grant.Claim.Generation}); err != nil {
		t.Fatal(err)
	}
	requests, err := b.Requests(t.Context(), task.TaskID)
	if err != nil || len(requests) != 1 || requests[0].ID != request.ID || requests[0].Status != api.RequestSuperseded || requests[0].Revision != request.Revision+1 {
		t.Fatalf("old actionable request survived handoff: %+v %v", requests, err)
	}
}
