package server_test

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

func groupRequest(t *testing.T, p *client.Project) api.GroupClaim {
	t.Helper()
	group := "20261112-coordinator-group"
	one := todoTask(t, p, "coordinator-one", group)
	two := todoTask(t, p, "coordinator-two", group)
	return api.GroupClaim{ExecutionID: "first-coordinator", Request: board.CoordinatorClaim{GroupID: group, Owner: "codex", Token: "public-session", Basis: "confirmed group fixture", Members: []string{one.TaskID, two.TaskID}}}
}
func TestCoordinatorLeaseHasOneWinnerAndItsPublicTokenGrantsNoAuthority(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	request := groupRequest(t, a)
	inputs := []api.GroupClaim{request, request}
	inputs[1].ExecutionID = "second-coordinator"
	projects := []*client.Project{a, b}
	grants := make([]api.GroupGrant, 2)
	errs := make([]error, 2)
	var workers sync.WaitGroup
	for i := range 2 {
		workers.Go(func() {
			grants[i], errs[i] = projects[i].ClaimGroup(t.Context(), "", request.Request.GroupID, inputs[i], nil)
		})
	}
	workers.Wait()
	winner := -1
	for i, err := range errs {
		if err == nil {
			if winner != -1 {
				t.Fatal("two coordinator leases were granted")
			}
			winner = i
		} else {
			expectCode(t, err, api.CodeLeaseActive)
		}
	}
	if winner < 0 {
		t.Fatalf("no winner: %v", errs)
	}
	grant := grants[winner]
	view, err := a.Group(t.Context(), request.Request.GroupID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Checkpoint == nil || view.Claim == nil || view.Claim.Generation != 1 || view.Checkpoint.Authority.Epoch != 1 {
		t.Fatalf("incomplete atomic group claim: %+v", view)
	}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), grant.Token) {
		t.Fatal("public group view leaked its private bearer token")
	}
	fake := client.Claim{Token: view.Checkpoint.Authority.Token, Generation: grant.Claim.Generation}
	_, err = projects[1-winner].RenewGroup(t.Context(), request.Request.GroupID, fake)
	expectCode(t, err, api.CodeHeldByOther)
	proof := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	f.clock.advance(16 * time.Second)
	if _, err := projects[winner].RenewGroup(t.Context(), request.Request.GroupID, proof); err != nil {
		t.Fatal(err)
	}
	after, err := a.Group(t.Context(), request.Request.GroupID)
	if err != nil || after.Checkpoint.Revision != view.Checkpoint.Revision || after.Checkpoint.ContentHash != view.Checkpoint.ContentHash {
		t.Fatal("renewal changed the group checkpoint")
	}
	for _, id := range request.Request.Members {
		task, err := a.Show(t.Context(), id)
		if err != nil || task.State != "todo" {
			t.Fatal("coordinator claim mutated an executing card")
		}
	}
}
func TestCoordinatorTakeoverFencesLateWritesAndRecoversItsReceipt(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	request := groupRequest(t, a)
	first, err := a.ClaimGroup(t.Context(), "claim-first", request.Request.GroupID, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	firstProof := client.Claim{Token: first.Token, Generation: first.Claim.Generation}
	f.clock.advance(91 * time.Second)
	secondInput := request
	secondInput.ExecutionID = "successor-coordinator"
	secondInput.ExpectedGeneration = first.Claim.Generation
	secondInput.Decision = "explicit coordinator handoff after lease loss"
	secondInput.Request.ExpectedRevision = first.Checkpoint.Revision
	secondInput.Request.ExpectedEpoch = first.Checkpoint.Authority.Epoch
	second, err := b.ClaimGroup(t.Context(), "claim-second", request.Request.GroupID, secondInput, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.Claim.Generation != 2 || second.Checkpoint.Authority.Epoch != first.Checkpoint.Authority.Epoch+1 {
		t.Fatalf("handoff failed: %+v", second)
	}
	_, err = a.RenewGroup(t.Context(), request.Request.GroupID, firstProof)
	expectCode(t, err, api.CodeLeaseInvalid)
	reconcile := board.CoordinatorReconcile{GroupID: request.Request.GroupID, CWD: "/first/clone", ExpectedRevision: first.Checkpoint.Revision, Authority: first.Checkpoint.Authority, Members: map[string]board.CoordinatorObservation{}}
	for id, member := range first.Checkpoint.Members {
		reconcile.Members[id] = board.CoordinatorObservation{Revision: member.Revision}
	}
	_, err = a.CoordinatorAction(t.Context(), "late-checkpoint", request.Request.GroupID, "reconcile", api.GroupReconcile{Request: reconcile}, firstProof)
	expectCode(t, err, api.CodeLeaseInvalid)
	dir, clock := f.dir, f.clock
	f.stop()
	f = start(t, dir, clock)
	b = f.project("host-b")
	replay, err := b.ClaimGroup(t.Context(), "claim-second", request.Request.GroupID, secondInput, nil)
	if err != nil || replay.Token != second.Token || replay.Checkpoint.ContentHash != second.Checkpoint.ContentHash {
		t.Fatalf("group handoff receipt lost: %+v %v", replay, err)
	}
	current, err := b.Group(t.Context(), request.Request.GroupID)
	if err != nil || current.Checkpoint.Revision != second.Checkpoint.Revision {
		t.Fatal("late coordinator wrote over the successor")
	}
	// The successor can reconcile through service-validated producer facts.
	reconcile.ExpectedRevision = second.Checkpoint.Revision
	reconcile.Authority = second.Checkpoint.Authority
	reconcile.CWD = "/other/clone"
	facts, err := b.CoordinatorFacts(t.Context(), request.Request.GroupID, reconcile)
	if err != nil {
		t.Fatal(err)
	}
	checks := make([]api.CoordinatorGitCheck, len(facts))
	for i, fact := range facts {
		checks[i] = api.CoordinatorGitCheck{Facts: fact}
	}
	_, err = b.CoordinatorAction(t.Context(), "current-checkpoint", request.Request.GroupID, "reconcile", api.GroupReconcile{Request: reconcile, Checks: checks}, client.Claim{Token: second.Token, Generation: second.Claim.Generation})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCoordinatorRequiresVerdictsForTheExactCurrentGitFacts(t *testing.T) {
	f := newFixture(t)
	project := f.project("host-a")
	input := groupRequest(t, project)
	grant, err := project.ClaimGroup(t.Context(), "", input.Request.GroupID, input, nil)
	if err != nil {
		t.Fatal(err)
	}
	id := input.Request.Members[0]
	executor, err := project.Claim(t.Context(), "", id, api.ClaimRequest{ClientID: "host-a", ExecutionID: "member-execution", Owner: "codex", Start: &api.StartInput{Session: "codex member-session", Window: "tmux:$1:@1:%1"}})
	if err != nil {
		t.Fatal(err)
	}
	taskProof := client.Claim{Token: executor.Token, Generation: executor.Claim.Generation}
	if _, err := project.Execution(t.Context(), "", id, api.ExecutionUpdate{Action: api.ExecutionConfirm}, taskProof); err != nil {
		t.Fatal(err)
	}
	card, err := project.Show(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(card.Text, "- TASK_BRANCH:", "- TASK_BRANCH: member-delivery", 1)
	if _, err := project.PutDocument(t.Context(), "", id, api.Document{Path: "spec.md", Text: text, ExpectedRevision: card.Revision}, &taskProof); err != nil {
		t.Fatal(err)
	}
	card, err = project.Move(t.Context(), "", id, api.Move{State: "review"}, &taskProof)
	if err != nil {
		t.Fatal(err)
	}
	request := board.CoordinatorReconcile{CWD: "/independent/clone", GroupID: input.Request.GroupID, ExpectedRevision: grant.Checkpoint.Revision, Authority: grant.Checkpoint.Authority, Members: map[string]board.CoordinatorObservation{}}
	for task, member := range grant.Checkpoint.Members {
		request.Members[task] = board.CoordinatorObservation{Revision: member.Revision}
	}
	commit := strings.Repeat("b", 40)
	request.Members[id] = board.CoordinatorObservation{Revision: card.Revision, DeliveryCommit: commit}
	proof := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	blocked, err := project.CoordinatorAction(t.Context(), "without-git-facts", input.Request.GroupID, "reconcile", api.GroupReconcile{Request: request}, proof)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Members[id].Blocked == nil || blocked.Members[id].DeliveryCommit != "" {
		t.Fatal("unverified Git delivery advanced its checkpoint")
	}
	request.ExpectedRevision = blocked.Revision
	facts, err := project.CoordinatorFacts(t.Context(), input.Request.GroupID, request)
	if err != nil || len(facts) != 1 || facts[0].Delivery == nil {
		t.Fatalf("missing producer facts: %+v %v", facts, err)
	}
	wrong := facts[0]
	wrong.Delivery = &board.CoordinatorDelivery{TaskID: id, Branch: facts[0].Delivery.Branch, Commit: strings.Repeat("c", 40)}
	rejected, err := project.CoordinatorAction(t.Context(), "changed-git-facts", input.Request.GroupID, "reconcile", api.GroupReconcile{Request: request, Checks: []api.CoordinatorGitCheck{{Facts: wrong}}}, proof)
	if err != nil {
		t.Fatal(err)
	}
	if rejected.Members[id].Blocked == nil || rejected.Members[id].DeliveryCommit != "" {
		t.Fatal("a verdict for different Git facts was accepted")
	}
	request.ExpectedRevision = rejected.Revision
	checked, err := project.CoordinatorAction(t.Context(), "verified-git-facts", input.Request.GroupID, "reconcile", api.GroupReconcile{Request: request, Checks: []api.CoordinatorGitCheck{{Facts: facts[0]}}}, proof)
	if err != nil || checked.Members[id].Blocked != nil || checked.Members[id].DeliveryCommit != commit {
		t.Fatalf("exact verified delivery refused: %+v %v", checked.Members[id], err)
	}
}
