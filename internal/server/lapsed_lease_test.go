package server_test

import (
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
)

// lapsedTask claims a todo task for host-a and lets its lease lapse.
func lapsedTask(t *testing.T, f *fixture, p *client.Project, slug string) (api.ClaimGrant, client.Claim) {
	t.Helper()
	task := todoTask(t, p, slug)
	grant, err := claim(t, p, task.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	f.clock.advance(91 * time.Second)
	return grant, client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
}

// A lapsed lease keeps its holder: the views show it lapsed, the holder writes
// and renews without a takeover, and the renewal keeps the generation.
func TestLapsedClaimKeepsItsHolder(t *testing.T) {
	f := newFixture(t)
	a := f.project("host-a")
	grant, own := lapsedTask(t, f, a, "lapsed-holder")
	id := grant.Task.TaskID
	shown, err := a.Show(t.Context(), id)
	if err != nil || shown.Claim == nil || shown.Claim.Status != api.ClaimLapsed {
		t.Fatalf("show of a lapsed lease: %+v %v", shown.Claim, err)
	}
	list, err := a.List(t.Context(), "working")
	if err != nil || len(list.Tasks) != 1 || list.Tasks[0].Claim == nil || list.Tasks[0].Claim.Status != api.ClaimLapsed {
		t.Fatalf("list of a lapsed lease: %+v %v", list.Tasks, err)
	}
	if _, err := a.PutDocument(t.Context(), "", id, api.Document{Path: "report.md", Text: "written while lapsed", ExpectedRevision: shown.Revision}, &own); err != nil {
		t.Fatalf("lapsed holder write: %v", err)
	}
	renewed, err := a.Renew(t.Context(), id, own)
	if err != nil || renewed.Claim.Generation != grant.Claim.Generation || renewed.Claim.Status != api.ClaimActive {
		t.Fatalf("lapsed holder renewal: %+v %v", renewed.Claim, err)
	}
}

// A user-authorized termination of a lapsed task passes with the holder's
// credential and is refused for another execution's token, as for an expired
// active record before.
func TestLapsedClaimTerminationKeepsItsHolder(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	grant, own := lapsedTask(t, f, a, "lapsed-termination")
	id := grant.Task.TaskID
	foreign, err := claim(t, b, todoTask(t, b, "lapsed-termination-other").TaskID, "host-b")
	if err != nil {
		t.Fatal(err)
	}
	stop := api.Move{State: "archived", Result: "cancelled", Reason: "stopped", Decision: "user:stop"}
	_, err = b.Move(t.Context(), "", id, stop, &client.Claim{Token: foreign.Token, Generation: grant.Claim.Generation})
	expectCode(t, err, api.CodeHeldByOther)
	if _, err := a.Move(t.Context(), "", id, stop, &own); err != nil {
		t.Fatalf("lapsed holder termination: %v", err)
	}
}

// The holder of a lapsed lease releases it for an explicit successor.
func TestLapsedHolderReleases(t *testing.T) {
	f := newFixture(t)
	a := f.project("host-a")
	grant, own := lapsedTask(t, f, a, "lapsed-release")
	id := grant.Task.TaskID
	state, err := a.ExecutionState(t.Context(), id, own)
	if err != nil {
		t.Fatal(err)
	}
	release := api.ExecutionUpdate{Action: api.ExecutionRelease, ExpectedRevision: state.Task.Revision, ObservationTicket: state.ObservationTicket, Decision: "user:release", Presence: &api.AgentObservation{Status: "stopped"}}
	if _, err := a.Execution(t.Context(), "", id, release, own); err != nil {
		t.Fatalf("lapsed holder release: %v", err)
	}
	if shown, err := a.Show(t.Context(), id); err != nil || shown.Claim == nil || shown.Claim.Status != api.ClaimLost {
		t.Fatalf("released lease: %+v %v", shown.Claim, err)
	}
}

// A takeover replaces the holder for good: after the successor completed the
// task, the old execution still learns that it was taken over.
func TestTakenOverAnswerOutlivesTheSuccessor(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	grant, own := lapsedTask(t, f, a, "taken-over")
	id := grant.Task.TaskID
	next, err := b.Takeover(t.Context(), "", id, api.ClaimRequest{BranchMode: "main-develop", ClientID: "host-b", ExecutionID: "successor", Owner: "codex", Decision: "user:takeover", ExpectedGeneration: grant.Claim.Generation})
	if err != nil || next.Claim.Generation != grant.Claim.Generation+1 {
		t.Fatalf("takeover of a lapsed lease: %+v %v", next.Claim, err)
	}
	successor := client.Claim{Token: next.Token, Generation: next.Claim.Generation}
	if _, err := b.Move(t.Context(), "", id, api.Move{State: "archived", Result: "cancelled", Reason: "done elsewhere", Decision: "user:stop"}, &successor); err != nil {
		t.Fatal(err)
	}
	_, err = a.Renew(t.Context(), id, own)
	expectCode(t, err, api.CodeTakenOver)
}

// The server's downtime is not the holders' fault: a lease that expired while
// the server was stopped is extended at startup, so a takeover right after the
// restart is refused and the holder renews at its generation.
func TestStartupExtendsTaskAndGroupLeases(t *testing.T) {
	f := newFixture(t)
	a := f.project("host-a")
	task := todoTask(t, a, "startup-task")
	grant, err := claim(t, a, task.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	request := groupRequest(t, a)
	group := request.Request.GroupID
	coordinator, err := a.ClaimGroup(t.Context(), "", group, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir, clock := f.dir, f.clock
	f.stop()
	clock.advance(10 * time.Minute)
	f = start(t, dir, clock)
	a, b := f.project("host-a"), f.project("host-b")

	_, err = b.Takeover(t.Context(), "", task.TaskID, api.ClaimRequest{BranchMode: "main-develop", ClientID: "host-b", ExecutionID: "early", Owner: "codex", Decision: "user:takeover", ExpectedGeneration: grant.Claim.Generation})
	expectCode(t, err, api.CodeLeaseActive)
	take := request
	take.ExecutionID, take.ExpectedGeneration, take.Decision = "early-coordinator", coordinator.Claim.Generation, "user:takeover"
	_, err = b.ClaimGroup(t.Context(), "", group, take, nil)
	expectCode(t, err, api.CodeLeaseActive)

	renewed, err := a.Renew(t.Context(), task.TaskID, client.Claim{Token: grant.Token, Generation: grant.Claim.Generation})
	if err != nil || renewed.Claim.Generation != grant.Claim.Generation {
		t.Fatalf("task renewal after restart: %+v %v", renewed.Claim, err)
	}
	group2, err := a.RenewGroup(t.Context(), group, client.Claim{Token: coordinator.Token, Generation: coordinator.Claim.Generation})
	if err != nil || group2.Claim.Generation != coordinator.Claim.Generation {
		t.Fatalf("group renewal after restart: %+v %v", group2.Claim, err)
	}
}
