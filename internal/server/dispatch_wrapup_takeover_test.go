package server_test

import (
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

// retainedWrapUp is a card whose wrap-up round was accepted by generation 1
// against that generation's integration, then handed to generation 2 with the
// round retained.
type retainedWrapUp struct {
	f           *fixture
	a, b        *client.Project
	task        string
	member      api.IntegrationMember
	integration api.Integration
	oldProof    client.Claim
	newGrant    api.ClaimGrant
	newProof    client.Claim
	round       board.Dispatch
}

func wrapUpRound(t *testing.T, p *client.Project, member api.IntegrationMember, integration api.Integration, id string) board.DispatchInput {
	t.Helper()
	git := board.DispatchIntegration{
		IntegrationID: integration.ID, IntegrationExecution: integration.Request.ExecutionID, IntegrationGeneration: integration.Generation,
		DispatchID: id, TaskID: member.TaskID, CWD: "project:" + p.ID(),
		SourceCommit: integration.Request.Commit, ReviewTarget: integration.Request.Commit, ReviewBase: strings.Repeat("a", 40),
		TargetCommit: integration.ObservedTarget, TargetRef: integration.Request.TargetRef,
		Author: "coordinator", Basis: "wrap-up fixture", VerifiedAt: time.Now().UTC().Add(-time.Second),
	}
	binding := board.DispatchWrapUpBinding{Artifact: board.ArtifactReference{TaskID: member.TaskID, Path: "dispatches/" + id + "/integration.json"}, Git: git}
	return board.DispatchInput{ID: id, TaskID: member.TaskID, Kind: "wrap-up", Message: "Record completion and clean the execution workspace", Base: integration.Request.Commit, Evidence: board.DispatchEvidence{WrapUp: &binding}}
}

// integrate publishes the final integration of one generation through the
// push path and returns the integrated record.
func integrate(t *testing.T, p *client.Project, input api.IntegrationRequest, key string, owner client.Claim) api.Integration {
	t.Helper()
	grant, err := p.ClaimIntegration(t.Context(), key+"-claim", input, owner)
	if err != nil {
		t.Fatal(err)
	}
	proof := client.Claim{Token: grant.Token, Generation: grant.Integration.Generation}
	observation, err := p.IntegrationObservation(t.Context(), grant.Integration.ID)
	if err != nil {
		t.Fatal(err)
	}
	pushing, err := p.IntegrationAction(t.Context(), key+"-pushing", grant.Integration.ID, api.IntegrationCommand{Action: "pushing", ExpectedRevision: grant.Integration.Revision, ObservedTarget: input.ExpectedTarget, ObservationTicket: observation.Ticket}, proof)
	if err != nil {
		t.Fatal(err)
	}
	if observation, err = p.IntegrationObservation(t.Context(), grant.Integration.ID); err != nil {
		t.Fatal(err)
	}
	done, err := p.IntegrationAction(t.Context(), key+"-confirm", grant.Integration.ID, api.IntegrationCommand{Action: "confirm", ExpectedRevision: pushing.Revision, ObservedTarget: input.Commit, ObservedAt: time.Now().UTC(), ObservationTicket: observation.Ticket, Edges: []api.IntegrationEdge{{Ancestor: input.Commit, Descendant: input.Commit}}}, proof)
	if err != nil || done.Phase != "integrated" {
		t.Fatalf("integration: %+v %v", done, err)
	}
	return done
}

func retainWrapUp(t *testing.T, slug string) *retainedWrapUp {
	t.Helper()
	f := newFixture(t)
	r := &retainedWrapUp{f: f}
	r.a, r.b = f.project("host-a"), f.project("host-b")
	member, first := integrationTask(t, f, r.a, "host-a", slug)
	r.member, r.task = member, member.TaskID
	r.oldProof = client.Claim{Token: first.Token, Generation: first.Claim.Generation}
	r.integration = integrate(t, r.a, integrationInput(t, r.a, member, first.Claim.ExecutionID, "wrap-up-integration"), "g1", r.oldProof)
	current, err := r.a.Show(t.Context(), r.task)
	if err != nil {
		t.Fatal(err)
	}
	input := wrapUpRound(t, r.a, member, r.integration, slug+"-wrap-up")
	prepared, err := r.a.DispatchAction(t.Context(), "prepare-"+slug, r.task, "prepare", api.DispatchCommand{Input: &input, CardRevision: current.Revision}, r.oldProof)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.a.Move(t.Context(), "accept-"+slug, r.task, api.Move{State: "working", Authorization: prepared.Authorization}, &r.oldProof); err != nil {
		t.Fatal(err)
	}
	if r.round, err = r.a.Dispatch(t.Context(), r.task, input.ID); err != nil || r.round.State != board.DispatchAccepted {
		t.Fatalf("accepted round: %+v %v", r.round, err)
	}
	f.clock.advance(91 * time.Second)
	r.newGrant, err = r.b.Takeover(t.Context(), "", r.task, api.ClaimRequest{ClientID: "host-b", ExecutionID: "wrap-up-successor", Owner: "codex", Decision: "explicit takeover fixture", ExpectedGeneration: first.Claim.Generation, Dispatch: &board.DispatchTakeover{Authorization: r.round.Authorization, Revision: r.round.Revision, Mode: "retain"}})
	if err != nil {
		t.Fatal(err)
	}
	r.newProof = client.Claim{Token: r.newGrant.Token, Generation: r.newGrant.Claim.Generation}
	return r
}

func (r *retainedWrapUp) end(t *testing.T, key string, proof client.Claim, state board.DispatchState) error {
	t.Helper()
	_, err := r.b.DispatchAction(t.Context(), key, r.task, "end", api.DispatchCommand{DispatchID: r.round.Input.ID, Revision: r.round.Revision, State: state, Reason: "the executing agent was replaced", Decision: "user decision fixture"}, proof)
	return err
}

func TestSuccessorGenerationEndsRetainedWrapUpRound(t *testing.T) {
	for _, state := range []board.DispatchState{board.DispatchCancelled, board.DispatchFailed} {
		t.Run(string(state), func(t *testing.T) {
			r := retainWrapUp(t, "retained-"+string(state))
			ctx := t.Context()
			err := r.end(t, "old-generation-end", r.oldProof, state)
			if !api.IsCode(err, api.CodeHeldByOther) && !api.IsCode(err, api.CodeLeaseInvalid) {
				t.Fatalf("old generation ended the round: %v", err)
			}
			if kept, err := r.b.Dispatch(ctx, r.task, r.round.Input.ID); err != nil || kept.State != board.DispatchAccepted || kept.Revision != r.round.Revision {
				t.Fatalf("rejected end changed the round: %+v %v", kept, err)
			}
			if err := r.end(t, "successor-end", r.newProof, state); err != nil {
				t.Fatalf("successor could not end the retained round: %v", err)
			}
			ended, err := r.b.Dispatch(ctx, r.task, r.round.Input.ID)
			if err != nil || ended.State != state {
				t.Fatalf("round after end: %+v %v", ended, err)
			}
			card, err := r.b.Show(ctx, r.task)
			if err != nil || board.MetadataFrom(card.Text, "DISPATCH_ID") != "" || board.MetadataFrom(card.Text, "EXECUTION_EPOCH") != "" {
				t.Fatalf("binding survived the end: %v\n%s", err, card.Text)
			}
			if _, err := r.b.Execution(ctx, "", r.task, api.ExecutionUpdate{Action: api.ExecutionConfirm}, r.newProof); err != nil {
				t.Fatal(err)
			}
			card, err = r.b.Show(ctx, r.task)
			if err != nil {
				t.Fatal(err)
			}
			next := wrapUpRound(t, r.b, r.member, r.integration, r.round.Input.ID+"-again")
			_, err = r.b.DispatchAction(ctx, "prepare-old-integration", r.task, "prepare", api.DispatchCommand{Input: &next, CardRevision: card.Revision}, r.newProof)
			expectCode(t, err, api.CodeLeaseInvalid)
		})
	}
}

func TestSuccessorGenerationFinishesWrapUpAfterEndingRetainedRound(t *testing.T) {
	r := retainWrapUp(t, "recovered-wrap-up")
	ctx := t.Context()
	if err := r.end(t, "successor-end", r.newProof, board.DispatchCancelled); err != nil {
		t.Fatalf("successor could not end the retained round: %v", err)
	}
	if _, err := r.b.Execution(ctx, "", r.task, api.ExecutionUpdate{Action: api.ExecutionConfirm}, r.newProof); err != nil {
		t.Fatal(err)
	}
	commit := r.member.DeliveryCommit
	stageDelivery(t, r.b, r.newGrant, commit)
	reviewed, err := r.b.Move(ctx, "successor-review", r.task, api.Move{State: "review", DeliveryCommit: commit}, &r.newProof)
	if err != nil {
		t.Fatalf("successor could not return the card to review: %v", err)
	}
	member := api.IntegrationMember{TaskID: r.task, Revision: reviewed.Revision, Generation: r.newGrant.Claim.Generation, DeliveryCommit: commit}
	// The first generation already pushed the delivery, so the successor only
	// reconciles the target that contains it.
	request := integrationInput(t, r.b, member, r.newGrant.Claim.ExecutionID, "successor-integration")
	request.ExpectedTarget, request.BaseCommit, request.AlreadyContained = commit, commit, true
	request.Edges = []api.IntegrationEdge{{Ancestor: commit, Descendant: commit}}
	grant, err := r.b.ClaimIntegration(ctx, "successor-integration-claim", request, r.newProof)
	if err != nil {
		t.Fatalf("successor could not claim the final integration: %v", err)
	}
	observation, err := r.b.IntegrationObservation(ctx, grant.Integration.ID)
	if err != nil {
		t.Fatal(err)
	}
	integration, err := r.b.IntegrationAction(ctx, "successor-integration-confirm", grant.Integration.ID, api.IntegrationCommand{Action: "confirm", ExpectedRevision: grant.Integration.Revision, ObservedTarget: commit, ObservedAt: time.Now().UTC(), ObservationTicket: observation.Ticket, Edges: request.Edges}, client.Claim{Token: grant.Token, Generation: grant.Integration.Generation})
	if err != nil || integration.Phase != "integrated" {
		t.Fatalf("successor integration: %+v %v", integration, err)
	}
	input := wrapUpRound(t, r.b, member, integration, r.round.Input.ID+"-again")
	prepared, err := r.b.DispatchAction(ctx, "prepare-successor-wrap-up", r.task, "prepare", api.DispatchCommand{Input: &input, CardRevision: reviewed.Revision}, r.newProof)
	if err != nil {
		t.Fatalf("successor could not prepare a wrap-up on its own integration: %v", err)
	}
	if _, err := r.b.Move(ctx, "accept-successor-wrap-up", r.task, api.Move{State: "working", Authorization: prepared.Authorization}, &r.newProof); err != nil {
		t.Fatal(err)
	}
	done, err := r.b.Move(ctx, "complete-successor-wrap-up", r.task, api.Move{State: "done", Result: "completed", Authorization: prepared.Authorization, DeliveryCommit: commit}, &r.newProof)
	if err != nil || done.State != "done" {
		t.Fatalf("successor completion: %+v %v", done, err)
	}
}
