package server_test

import (
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
)

func TestLiveHolderCanRestartOnlyItsFreshlyStoppedExecutor(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	card := todoTask(t, a, "owned-restart")
	grant, err := a.Claim(t.Context(), "", card.TaskID, api.ClaimRequest{ClientID: "host-a", ExecutionID: "first-executor", Owner: "claude", Start: &api.StartInput{Session: "claude original", Window: "tmux:$1:@1:%1"}})
	if err != nil {
		t.Fatal(err)
	}
	proof := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	if _, err := a.Execution(t.Context(), "", card.TaskID, api.ExecutionUpdate{Action: api.ExecutionConfirm}, proof); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ObserveAgent(t.Context(), "stopped", card.TaskID, api.AgentObservation{Status: "stopped"}, proof); err != nil {
		t.Fatal(err)
	}
	state, err := a.ExecutionState(t.Context(), card.TaskID, proof)
	if err != nil || state.Task.Claim.Status != api.ClaimActive {
		t.Fatalf("presence ended the task lease: %+v %v", state, err)
	}
	input := api.ClaimRequest{ClientID: "host-a", ExecutionID: "second-executor", Owner: "claude", Decision: "holder explicitly restarts the stopped executor", ExpectedGeneration: grant.Claim.Generation, ExpectedRevision: state.Task.Revision, ObservationTicket: state.ObservationTicket, PreviousPresence: &api.AgentObservation{Status: "stopped"}, Start: &api.StartInput{Session: "claude continued", Window: "tmux:$2:@2:%2"}}
	_, err = b.Handoff(t.Context(), "foreign-restart", card.TaskID, input, proof)
	expectCode(t, err, api.CodeHeldByOther)
	alive := input
	alive.PreviousPresence = &api.AgentObservation{Status: "alive"}
	_, err = a.Handoff(t.Context(), "alive-restart", card.TaskID, alive, proof)
	expectCode(t, err, api.CodeEvidenceIncomplete)
	f.clock.advance(31 * time.Second)
	_, err = a.Handoff(t.Context(), "stale-proof", card.TaskID, input, proof)
	expectCode(t, err, api.CodeEvidenceIncomplete)
	state, err = a.ExecutionState(t.Context(), card.TaskID, proof)
	if err != nil {
		t.Fatal(err)
	}
	input.ObservationTicket = state.ObservationTicket
	next, err := a.Handoff(t.Context(), "owned-restart", card.TaskID, input, proof)
	if err != nil || next.Claim.Generation != grant.Claim.Generation+1 || next.Claim.ExecutionID != input.ExecutionID {
		t.Fatalf("restart: %+v %v", next, err)
	}
	if _, err := a.Renew(t.Context(), card.TaskID, proof); !api.IsCode(err, api.CodeLeaseInvalid) && !api.IsCode(err, api.CodeHeldByOther) {
		t.Fatalf("old execution renewed after owned restart: %v", err)
	}
	replay, err := a.Handoff(t.Context(), "owned-restart", card.TaskID, input, proof)
	if err != nil || replay.Token != next.Token || replay.Task.Revision != next.Task.Revision {
		t.Fatalf("handoff replay: %+v %v", replay, err)
	}
}
