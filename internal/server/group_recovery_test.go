package server_test

import (
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

type lostRecoveryFixture struct {
	fixture            *fixture
	owner, coordinator *client.Project
	task               api.Task
	taskGrant          api.ClaimGrant
	group              api.GroupGrant
	proof              client.Claim
	input              api.GroupRecover
}

func newLostRecoveryFixture(t *testing.T, sharedHost ...bool) *lostRecoveryFixture {
	t.Helper()
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	ownerID := "host-a"
	if len(sharedHost) != 0 && sharedHost[0] {
		a, ownerID = b, "host-b"
	}
	request := groupRequest(t, b)
	coordinator, err := b.ClaimGroup(t.Context(), "recovery-coordinator", request.Request.GroupID, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	proof := client.Claim{Token: coordinator.Token, Generation: coordinator.Claim.Generation}
	grant, err := claim(t, a, request.Request.Members[0], ownerID)
	if err != nil {
		t.Fatal(err)
	}
	task, err := a.Show(t.Context(), grant.Task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	reconcile := board.CoordinatorReconcile{GroupID: request.Request.GroupID, ExpectedRevision: coordinator.Checkpoint.Revision, Authority: coordinator.Checkpoint.Authority, Members: map[string]board.CoordinatorObservation{}}
	for _, id := range request.Request.Members {
		value, err := b.Show(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		reconcile.Members[id] = board.CoordinatorObservation{Revision: value.Revision}
	}
	reconcile.Members[task.TaskID] = board.CoordinatorObservation{Revision: task.Revision, DispatchID: "missing-round", Epoch: 1}
	checkpoint, err := b.CoordinatorAction(t.Context(), "block-recovery-member", request.Request.GroupID, "reconcile", api.GroupReconcile{Request: reconcile}, proof)
	if err != nil || checkpoint.Members[task.TaskID].Blocked == nil {
		t.Fatalf("blocked recovery fixture: %+v %v", checkpoint, err)
	}
	input := api.GroupRecover{Request: board.CoordinatorRecover{GroupID: request.Request.GroupID, Member: task.TaskID, ExpectedRevision: checkpoint.Revision, ExpectedEpoch: checkpoint.Authority.Epoch, Decision: "user: explicitly recover the lost member without launching an agent", Reason: "restore a blocked execution cycle"}, ObservedRevision: task.Revision, Observation: api.AgentObservation{Status: "unknown", Reason: "predecessor host was not probed"}, Lost: &api.RecoveryLease{ExecutionID: "recovery-audit-execution", ExpectedGeneration: grant.Claim.Generation}}
	coordinator.Checkpoint = checkpoint
	return &lostRecoveryFixture{fixture: f, owner: a, coordinator: b, task: task, taskGrant: grant, group: coordinator, proof: proof, input: input}
}
func (f *lostRecoveryFixture) expireMember(t *testing.T) {
	t.Helper()
	f.fixture.clock.advance(45 * time.Second)
	if _, err := f.coordinator.RenewGroup(t.Context(), f.input.Request.GroupID, f.proof); err != nil {
		t.Fatal(err)
	}
	f.fixture.clock.advance(46 * time.Second)
}
func TestLostMemberRecoveryFencesOldGenerationWithoutStartingOrAssigning(t *testing.T) {
	f := newLostRecoveryFixture(t)
	request, err := f.coordinator.CreateRequest(t.Context(), "queued-before-recovery", f.task.TaskID, api.NewCollabRequest{Kind: api.RequestFix, Message: "old generation work", TargetGeneration: f.taskGrant.Claim.Generation})
	if err != nil {
		t.Fatal(err)
	}
	f.expireMember(t)
	checkpoint, err := f.coordinator.CoordinatorAction(t.Context(), "recover-lost-member", f.input.Request.GroupID, "recover", f.input, f.proof)
	if err != nil || len(checkpoint.Recoveries) != 1 {
		t.Fatalf("explicit lost recovery: %+v %v", checkpoint, err)
	}
	after, err := f.owner.Show(t.Context(), f.task.TaskID)
	if err != nil || after.State != "working" || after.Claim == nil || after.Claim.Generation != f.taskGrant.Claim.Generation+1 || after.Claim.Status != api.ClaimReleased {
		t.Fatalf("recovery audit generation: %+v %v", after, err)
	}
	for _, field := range []string{board.FieldOwner, board.FieldSession, board.FieldWindow} {
		if board.MetadataFrom(after.Text, field) != board.MetadataFrom(f.task.Text, field) {
			t.Fatal("recovery fabricated a new executor or overwrote the old scene")
		}
	}
	if after.Claim.Observation != nil {
		t.Fatal("recovery pretended the predecessor was observed stopped")
	}
	old := client.Claim{Token: f.taskGrant.Token, Generation: f.taskGrant.Claim.Generation}
	if _, err := f.owner.Renew(t.Context(), f.task.TaskID, old); !api.IsCode(err, api.CodeLeaseInvalid) {
		t.Fatalf("old execution renewed: %v", err)
	}
	if _, err := f.owner.PutDocument(t.Context(), "late-recovery-write", f.task.TaskID, api.Document{Path: "progress.md", Text: "late old output", ExpectedRevision: after.Revision}, &old); !api.IsCode(err, api.CodeLeaseInvalid) {
		t.Fatalf("old execution wrote: %v", err)
	}
	if _, err := f.owner.Execution(t.Context(), "late-recovery-rollback", f.task.TaskID, api.ExecutionUpdate{Action: api.ExecutionRollback}, old); !api.IsCode(err, api.CodeLeaseInvalid) {
		t.Fatalf("old execution rolled back: %v", err)
	}
	requests, err := f.coordinator.Requests(t.Context(), f.task.TaskID)
	if err != nil || len(requests) != 1 || requests[0].ID != request.ID || requests[0].Status != api.RequestSuperseded {
		t.Fatalf("old queue stayed actionable: %+v %v", requests, err)
	}
	replay, err := f.coordinator.CoordinatorAction(t.Context(), "recover-lost-member", f.input.Request.GroupID, "recover", f.input, f.proof)
	if err != nil || replay.Revision != checkpoint.Revision {
		t.Fatalf("recovery receipt: %+v %v", replay, err)
	}
	fresh, err := f.coordinator.Takeover(t.Context(), "fresh-agent-after-recovery", f.task.TaskID, api.ClaimRequest{ClientID: "host-b", ExecutionID: "fresh-agent-execution", Owner: "codex", ExpectedGeneration: after.Claim.Generation, Decision: "user: start a fresh executor after audited recovery"})
	if err != nil || fresh.Claim.Generation != after.Claim.Generation+1 {
		t.Fatalf("fresh explicit takeover: %+v %v", fresh, err)
	}
}
func TestLostMemberRecoveryRefusesLiveClaimsAndChangedFacts(t *testing.T) {
	f := newLostRecoveryFixture(t)
	if _, err := f.coordinator.CoordinatorAction(t.Context(), "live-is-not-lost", f.input.Request.GroupID, "recover", f.input, f.proof); !api.IsCode(err, api.CodeLeaseActive) {
		t.Fatalf("live foreign claim reset: %v", err)
	}
	fake := f.input
	fake.Lost = nil
	fake.Observation.Status = "stopped"
	if _, err := f.coordinator.CoordinatorAction(t.Context(), "fabricated-foreign-stop", f.input.Request.GroupID, "recover", fake, f.proof); !api.IsCode(err, api.CodeHeldByOther) {
		t.Fatalf("group authority replaced the task holder: %v", err)
	}
	f.expireMember(t)
	wrong := f.input
	lease := *wrong.Lost
	lease.ExpectedGeneration++
	wrong.Lost = &lease
	if _, err := f.coordinator.CoordinatorAction(t.Context(), "wrong-lost-generation", wrong.Request.GroupID, "recover", wrong, f.proof); !api.IsCode(err, api.CodeLeaseInvalid) {
		t.Fatalf("wrong generation reset: %v", err)
	}
	wrong = f.input
	wrong.ObservedRevision++
	if _, err := f.coordinator.CoordinatorAction(t.Context(), "wrong-lost-version", wrong.Request.GroupID, "recover", wrong, f.proof); !api.IsCode(err, api.CodeRevisionConflict) {
		t.Fatalf("wrong task version reset: %v", err)
	}
	wrong = f.input
	wrong.Observation.Status = "stopped"
	if _, err := f.coordinator.CoordinatorAction(t.Context(), "invented-predecessor-exit", wrong.Request.GroupID, "recover", wrong, f.proof); !api.IsCode(err, api.CodeInvalidRequest) {
		t.Fatalf("unobserved predecessor was called stopped: %v", err)
	}
	current, err := f.owner.Show(t.Context(), f.task.TaskID)
	if err != nil || current.Revision != f.task.Revision {
		t.Fatal("rejected recovery changed the member")
	}
}

func TestLostMemberRecoveryReceiptSurvivesRestartAndAuthorityExpiry(t *testing.T) {
	f := newLostRecoveryFixture(t)
	f.expireMember(t)
	checkpoint, err := f.coordinator.CoordinatorAction(t.Context(), "recovery-before-crash", f.input.Request.GroupID, "recover", f.input, f.proof)
	if err != nil {
		t.Fatal(err)
	}
	f.fixture.clock.advance(91 * time.Second)
	dir, clock := f.fixture.dir, f.fixture.clock
	f.fixture.stop()
	restarted := start(t, dir, clock)
	replay, err := restarted.project("host-b").CoordinatorAction(t.Context(), "recovery-before-crash", f.input.Request.GroupID, "recover", f.input, f.proof)
	if err != nil || replay.ContentHash != checkpoint.ContentHash {
		t.Fatalf("immutable receipt after restart: %+v %v", replay, err)
	}
	task, err := restarted.project("host-a").Show(t.Context(), f.task.TaskID)
	if err != nil || task.Claim.Generation != 2 || task.Revision != f.task.Revision+1 {
		t.Fatalf("replay changed recovery: %+v %v", task, err)
	}
}
func TestOwnedMemberRecoveryRequiresFreshServiceTicket(t *testing.T) {
	f := newLostRecoveryFixture(t, true)
	input := f.input
	input.Lost = nil
	input.Observation.Status = "stopped"
	f.proof.Members = map[string]api.ClaimProof{f.task.TaskID: {Token: f.taskGrant.Token, Generation: f.taskGrant.Claim.Generation}}
	if _, err := f.coordinator.CoordinatorAction(t.Context(), "missing-recovery-ticket", input.Request.GroupID, "recover", input, f.proof); !api.IsCode(err, api.CodeEvidenceIncomplete) {
		t.Fatalf("missing ticket accepted: %v", err)
	}
	state, err := f.owner.ExecutionState(t.Context(), f.task.TaskID, client.Claim{Token: f.taskGrant.Token, Generation: f.taskGrant.Claim.Generation})
	if err != nil {
		t.Fatal(err)
	}
	input.ObservationTicket = state.ObservationTicket
	f.fixture.clock.advance(31 * time.Second)
	if _, err := f.coordinator.CoordinatorAction(t.Context(), "stale-recovery-ticket", input.Request.GroupID, "recover", input, f.proof); !api.IsCode(err, api.CodeEvidenceIncomplete) {
		t.Fatalf("stale ticket accepted: %v", err)
	}
	state, err = f.owner.ExecutionState(t.Context(), f.task.TaskID, client.Claim{Token: f.taskGrant.Token, Generation: f.taskGrant.Claim.Generation})
	if err != nil {
		t.Fatal(err)
	}
	input.ObservationTicket = state.ObservationTicket
	value, err := f.coordinator.CoordinatorAction(t.Context(), "owned-stopped-recovery", input.Request.GroupID, "recover", input, f.proof)
	if err != nil || len(value.Recoveries) != 1 {
		t.Fatalf("fresh owned recovery: %+v %v", value, err)
	}
	after, err := f.owner.Show(t.Context(), f.task.TaskID)
	if err != nil || after.Claim.Generation != f.taskGrant.Claim.Generation || after.Claim.Status != api.ClaimActive {
		t.Fatalf("owned recovery changed ownership: %+v %v", after, err)
	}
}
