package worker

import (
	"context"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/remote"
)

func TestAgentExitIsReportedWithoutTreatingWorkerPresenceAsAgentLife(t *testing.T) {
	h := newHarness(t)
	project, credential := h.claimed("agent-exit")
	before, err := project.Show(t.Context(), credential.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	status := "alive"
	h.loop.o.Observe = func(context.Context, remote.Credential) api.AgentObservation {
		return api.AgentObservation{Status: status, Reason: "verified test process facts"}
	}
	h.advance(16 * time.Second)
	if live, err := h.loop.pass(t.Context()); err != nil || live != 1 {
		t.Fatalf("alive observation: %d %v", live, err)
	}
	alive, err := project.Show(t.Context(), credential.TaskID)
	if err != nil || alive.Claim.Observation == nil || alive.Claim.Observation.Status != "alive" || alive.Revision != before.Revision {
		t.Fatalf("observation mutated the card or was missing: %+v %v", alive, err)
	}
	status = "stopped"
	h.advance(16 * time.Second)
	if live, err := h.loop.pass(t.Context()); err != nil || live != 1 {
		t.Fatalf("stopped observation ended the worker lease: %d %v", live, err)
	}
	stopped, err := project.Show(t.Context(), credential.TaskID)
	if err != nil || stopped.Claim.Status != api.ClaimActive || stopped.Claim.Observation.Status != "stopped" || stopped.Revision != before.Revision {
		t.Fatalf("agent exit not reported independently: %+v %v", stopped, err)
	}
	successor := h.project("host-b")
	_, err = successor.Takeover(t.Context(), "no-preemption", credential.TaskID, api.ClaimRequest{ClientID: "host-b", ExecutionID: "premature", Owner: "codex", Decision: "foreign takeover fixture", ExpectedGeneration: credential.Generation})
	if !api.IsCode(err, api.CodeLeaseActive) {
		t.Fatalf("agent presence preempted a healthy worker: %v", err)
	}
	for range 7 {
		h.advance(16 * time.Second)
		if live, err := h.loop.pass(t.Context()); err != nil || live != 1 {
			t.Fatalf("stopped task lost renewal: %d %v", live, err)
		}
	}
	state, err := project.ExecutionState(t.Context(), credential.TaskID, *credential.Claim())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := project.Execution(t.Context(), "release-for-handoff", credential.TaskID, api.ExecutionUpdate{Action: api.ExecutionRelease, ExpectedRevision: state.Task.Revision, ObservationTicket: state.ObservationTicket, Decision: "holder explicitly releases the stopped executor", Presence: &api.AgentObservation{Status: "stopped"}}, *credential.Claim()); err != nil {
		t.Fatal(err)
	}
	grant, err := successor.Takeover(t.Context(), "", credential.TaskID, api.ClaimRequest{ClientID: "host-b", ExecutionID: "after-exit", Owner: "codex", Decision: "explicit takeover after confirmed exit", ExpectedGeneration: credential.Generation})
	if err != nil || grant.Claim.Generation != credential.Generation+1 {
		t.Fatalf("confirmed exit did not allow explicit takeover: %+v %v", grant, err)
	}
	_, err = project.ObserveAgent(t.Context(), "late-observation", credential.TaskID, api.AgentObservation{Status: "stopped"}, *credential.Claim())
	if !api.IsCode(err, api.CodeHeldByOther) && !api.IsCode(err, api.CodeLeaseInvalid) {
		t.Fatalf("old observation changed successor: %v", err)
	}
}
