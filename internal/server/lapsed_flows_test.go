package server_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
)

// A review writer whose task lease lapsed still stands: its run finalizes, and
// a new run prepared while the lease is lapsed takes its execution as input.
func TestReviewRuntimeAcceptsLapsedExecution(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	grant, run, proof := prepareRuntime(t, f, p)
	f.clock.advance(91 * time.Second)
	if _, err := p.ReviewRuntime(t.Context(), "lapsed-finalize", run.RunID, "finalize", runtimeFinalization(run), &proof); err != nil {
		t.Fatalf("lapsed writer finalization: %v", err)
	}
	secret := strings.Repeat("c", 64)
	next := proof
	next.ReviewWriter = secret
	request := api.ReviewPrepare{Input: board.ReviewInput{RunID: "lapsed-run", BatchID: "lapsed-batch", TaskIDs: []string{grant.Task.TaskID}, Role: "PMQA", Reviewer: "codex", Model: "test", Effort: "high", CWD: "/independent/clone", Base: strings.Repeat("a", 40), Commit: strings.Repeat("b", 40), ReportLanguage: "en"}, Requirements: map[string]string{"PMQA": "required", "Security": "N/A: repository policy"}, Originals: map[string][]byte{"task-context.md": []byte("frozen task"), "review-context.md": []byte("context")}, Version: "test", WriterHash: board.ReviewDigest([]byte(secret))}
	raw, err := p.ReviewRuntime(t.Context(), "lapsed-prepare", request.Input.RunID, "prepare", request, &next)
	if err != nil {
		t.Fatalf("lapsed input preparation: %v", err)
	}
	var prepared board.ReviewPreparation
	if err := json.Unmarshal(raw, &prepared); err != nil || !prepared.Fresh {
		t.Fatalf("lapsed input preparation: %+v %v", prepared, err)
	}
}

// A run prepared after a takeover takes the successor's generation as input:
// the successor's credential finalizes it.
func TestReviewRuntimeInputTakesTheSuccessorGeneration(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	task := todoTask(t, a, "runtime-successor")
	grant, err := claim(t, a, task.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	f.clock.advance(91 * time.Second)
	next, err := b.Takeover(t.Context(), "", task.TaskID, api.ClaimRequest{ClientID: "host-b", ExecutionID: "successor", Owner: "codex", ExpectedGeneration: grant.Claim.Generation, Decision: "explicit test takeover"})
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.Repeat("d", 64)
	proof := client.Claim{Token: next.Token, Generation: next.Claim.Generation, ReviewWriter: secret}
	request := api.ReviewPrepare{Input: board.ReviewInput{RunID: "successor-run", BatchID: "successor-batch", TaskIDs: []string{task.TaskID}, Role: "PMQA", Reviewer: "codex", Model: "test", Effort: "high", CWD: "/independent/clone", Base: strings.Repeat("a", 40), Commit: strings.Repeat("b", 40), ReportLanguage: "en"}, Requirements: map[string]string{"PMQA": "required", "Security": "N/A: repository policy"}, Originals: map[string][]byte{"task-context.md": []byte("frozen task"), "review-context.md": []byte("context")}, Version: "test", WriterHash: board.ReviewDigest([]byte(secret))}
	raw, err := b.ReviewRuntime(t.Context(), "successor-prepare", request.Input.RunID, "prepare", request, &proof)
	if err != nil {
		t.Fatal(err)
	}
	var prepared board.ReviewPreparation
	if err := json.Unmarshal(raw, &prepared); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ReviewRuntime(t.Context(), "successor-finalize", prepared.Run.RunID, "finalize", runtimeFinalization(prepared.Run), &proof); err != nil {
		t.Fatalf("successor finalization: %v", err)
	}
}

// A request that targets a generation that was taken over is refused.
func TestRequestForTakenOverGenerationIsRefused(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	grant, _ := lapsedTask(t, f, a, "request-taken-over")
	id := grant.Task.TaskID
	if _, err := b.Takeover(t.Context(), "", id, api.ClaimRequest{ClientID: "host-b", ExecutionID: "successor", Owner: "codex", Decision: "user:takeover", ExpectedGeneration: grant.Claim.Generation}); err != nil {
		t.Fatal(err)
	}
	_, err := b.CreateRequest(t.Context(), "", id, api.NewCollabRequest{Kind: api.RequestFix, Message: "fix", TargetGeneration: grant.Claim.Generation})
	expectCode(t, err, api.CodeLeaseInvalid)
}

// A lapsed integration lease keeps its owner: with its actor lapsed as well,
// the owner renews before the actor does, and records a push intent and its
// confirmation without renewing first.
func TestLapsedIntegrationKeepsItsOwner(t *testing.T) {
	r := newIntegrationRig(t, "lapsed-integration")
	ctx := t.Context()
	input := r.input("lapsed-execution")
	var grant api.IntegrationGrant
	r.claim("lapsed-claim", input, &grant)
	proof := client.Claim{Token: grant.Token, Generation: grant.Integration.Generation}
	id := grant.Integration.ID
	r.f.clock.advance(91 * time.Second)
	if view, err := r.a.Integration(ctx, id); err != nil || view.Status != api.ClaimLapsed {
		t.Fatalf("view of a lapsed integration: %+v %v", view.Status, err)
	}
	if actor, err := r.a.Show(ctx, r.member.TaskID); err != nil || actor.Claim == nil || actor.Claim.Status != api.ClaimLapsed {
		t.Fatalf("the actor did not lapse: %+v %v", actor.Claim, err)
	}
	if _, err := r.a.RenewIntegration(ctx, id, proof); err != nil {
		t.Fatalf("integration renewal before its actor: %v", err)
	}
	r.f.clock.advance(91 * time.Second)
	pushing := r.command(proof, grant.Integration.Revision, "pushing")
	pushing.ObservedTarget = input.ExpectedTarget
	if _, err := r.a.IntegrationAction(ctx, "", id, pushing, proof); err != nil {
		t.Fatalf("lapsed push intent: %v", err)
	}
	r.f.clock.advance(91 * time.Second)
	confirm := r.command(proof, grant.Integration.Revision+1, "confirm")
	confirm.ObservedTarget, confirm.ObservedAt = input.Commit, time.Now().UTC()
	confirm.Edges = []api.IntegrationEdge{{Ancestor: input.Commit, Descendant: input.Commit}}
	if _, err := r.a.IntegrationAction(ctx, "", id, confirm, proof); err != nil {
		t.Fatalf("lapsed confirmation: %v", err)
	}
}

// An integration lease that expired while the server was stopped is extended
// at startup: a rival claim right after the restart is refused and the owner
// renews.
func TestStartupExtendsIntegrationLease(t *testing.T) {
	r := newIntegrationRig(t, "startup-integration")
	ctx := t.Context()
	other, otherGrant := integrationTask(t, r.f, r.b, "host-b", "startup-integration-other")
	var grant api.IntegrationGrant
	r.claim("startup-claim", r.input("startup-execution"), &grant)
	dir, clock := r.f.dir, r.f.clock
	r.f.stop()
	clock.advance(10 * time.Minute)
	f := start(t, dir, clock)
	a, b := f.project("host-a"), f.project("host-b")
	rival := integrationInput(t, b, other, otherGrant.Claim.ExecutionID, "rival-execution")
	_, err := b.ClaimIntegration(ctx, "", rival, client.Claim{Token: otherGrant.Token, Generation: otherGrant.Claim.Generation})
	expectCode(t, err, api.CodeLeaseActive)
	if _, err := a.RenewIntegration(ctx, grant.Integration.ID, client.Claim{Token: grant.Token, Generation: grant.Integration.Generation}); err != nil {
		t.Fatalf("integration renewal after restart: %v", err)
	}
}

// A contributor whose lease lapsed still stands: the integration of its
// delivery is claimed. Once it was taken over, the delivery of the generation
// that stands is missing, and the integration is refused.
func TestIntegrationContributorStanding(t *testing.T) {
	r := newIntegrationRig(t, "contributor-standing")
	ctx := t.Context()
	r.f.clock.advance(91 * time.Second)
	var grant api.IntegrationGrant
	r.claim("lapsed-contributor", r.input("lapsed-contributor"), &grant)

	s := newIntegrationRig(t, "contributor-taken-over")
	s.f.clock.advance(91 * time.Second)
	next, err := s.b.Takeover(ctx, "", s.member.TaskID, api.ClaimRequest{ClientID: "host-b", ExecutionID: "contributor-successor", Owner: "codex", Decision: "user:takeover", ExpectedGeneration: s.task.Claim.Generation})
	if err != nil {
		t.Fatal(err)
	}
	member := s.member
	member.Revision, member.Generation = next.Task.Revision, next.Claim.Generation
	input := integrationInput(t, s.b, member, next.Claim.ExecutionID, "taken-over-contributor")
	_, err = s.b.ClaimIntegration(ctx, "", input, client.Claim{Token: next.Token, Generation: next.Claim.Generation})
	expectCode(t, err, api.CodeEvidenceIncomplete)
}
