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

func integrationTask(t *testing.T, f *fixture, p *client.Project, host, slug string) (api.IntegrationMember, api.ClaimGrant) {
	t.Helper()
	task := todoTask(t, p, slug)
	text := strings.Replace(task.Text, "- TASK_BRANCH:", "- TASK_BRANCH: task/"+slug, 1)
	task, err := p.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := claim(t, p, task.TaskID, host)
	if err != nil {
		t.Fatal(err)
	}
	own := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	base, commit := strings.Repeat("a", 40), strings.Repeat("b", 40)
	if _, err := p.PublishDelivery(t.Context(), "", api.DeliveryEvidence{Purpose: "delivery", TaskID: task.TaskID, ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, CardRevision: grant.Task.Revision, Commit: commit, Ref: api.DeliveryRef(p.ID(), task.TaskID, grant.Claim.Generation, commit), Remote: "git@example.invalid:demo.git", ObservedAt: f.clock.now()}, own); err != nil {
		t.Fatal(err)
	}
	task, err = p.Move(t.Context(), "", task.TaskID, api.Move{State: "review", DeliveryCommit: commit}, &own)
	if err != nil {
		t.Fatal(err)
	}
	plan := board.ReviewPlan{Schema: 1, PlanID: "integration-plan-" + slug, Sealed: true, Author: "coordinator", Basis: "integration fixture", CWD: "project:" + p.ID(), ReportLanguage: "en", TaskIDs: []string{task.TaskID}, Batches: []board.ReviewPlanBatch{{BatchID: "integration-batch-" + slug, TaskIDs: []string{task.TaskID}, Base: base, TargetCommit: commit, Requirements: map[string]string{"PMQA": "N/A: lease fixture", "Security": "N/A: repository policy"}}}}
	if _, err := p.ReviewPlan(t.Context(), "", plan, &own); err != nil {
		t.Fatal(err)
	}
	raw, err := p.ReviewBatchView(t.Context(), plan.Batches[0].BatchID)
	if err != nil {
		t.Fatal(err)
	}
	var view board.ReviewBatchView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	request := board.ReviewCloseRequest{BatchID: view.Batch.BatchID, ExpectedRevision: view.Batch.Revision, ViewHash: board.ReviewViewDigest(view), Author: "coordinator", Roles: map[string]board.ReviewRoleConclusion{}}
	edges, _, err := board.ReviewClosureEdges(view, request)
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"request": request, "evidence": board.ReviewGitEvidence{CWD: plan.CWD, Head: commit, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges}}
	if _, err := p.ReviewClose(t.Context(), "", body, &own); err != nil {
		t.Fatal(err)
	}
	current, err := p.Show(t.Context(), task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	return api.IntegrationMember{TaskID: task.TaskID, Revision: current.Revision, Generation: grant.Claim.Generation, DeliveryCommit: commit}, grant
}
func integrationInput(t *testing.T, p *client.Project, member api.IntegrationMember, actor, execution string) api.IntegrationRequest {
	t.Helper()
	target := "refs/heads/v2"
	observation, err := p.IntegrationObservation(t.Context(), api.IntegrationID(target))
	if err != nil {
		t.Fatal(err)
	}
	base, commit := strings.Repeat("a", 40), member.DeliveryCommit
	input := api.IntegrationRequest{Purpose: "deliver", TargetRef: target, BaseCommit: base, ExpectedTarget: base, Commit: commit, ExecutionID: execution, ActorExecutionID: actor, Decision: "explicitly integrate the reviewed fixture into v2", Members: []api.IntegrationMember{member}, ObservationTicket: observation.Ticket, Edges: []api.IntegrationEdge{{Ancestor: base, Descendant: commit}, {Ancestor: commit, Descendant: commit}}}
	if observation.Integration != nil {
		input.ExpectedRevision, input.ExpectedGeneration = observation.Integration.Revision, observation.Integration.Generation
	}
	return input
}

func TestIntegrationLeaseHasOneWinnerAndFencesLateGeneration(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	one, first := integrationTask(t, f, a, "host-a", "integrate-one")
	two, second := integrationTask(t, f, b, "host-b", "integrate-two")
	requests := []api.IntegrationRequest{integrationInput(t, a, one, first.Claim.ExecutionID, "integration-one"), integrationInput(t, b, two, second.Claim.ExecutionID, "integration-two")}
	projects := []*client.Project{a, b}
	owners := []client.Claim{{Token: first.Token, Generation: first.Claim.Generation}, {Token: second.Token, Generation: second.Claim.Generation}}
	grants := make([]api.IntegrationGrant, 2)
	failures := make([]error, 2)
	var racers sync.WaitGroup
	for i := range 2 {
		racers.Go(func() { grants[i], failures[i] = projects[i].ClaimIntegration(t.Context(), "", requests[i], owners[i]) })
	}
	racers.Wait()
	winner := -1
	for i, err := range failures {
		if err == nil {
			if winner != -1 {
				t.Fatal("two target leases were granted")
			}
			winner = i
		} else {
			expectCode(t, err, api.CodeLeaseActive)
		}
	}
	if winner < 0 {
		t.Fatalf("no target lease: %v", failures)
	}
	granted := grants[winner]
	target := granted.Integration.ID
	old := client.Claim{Token: granted.Token, Generation: granted.Integration.Generation}
	public, err := a.Integration(t.Context(), target)
	raw, _ := json.Marshal(public)
	if err != nil || strings.Contains(string(raw), granted.Token) {
		t.Fatalf("public lease leaked token: %v", err)
	}
	if _, err := projects[1-winner].RenewIntegration(t.Context(), target, old); !api.IsCode(err, api.CodeHeldByOther) {
		t.Fatalf("foreign client renewed: %v", err)
	}
	f.clock.advance(45 * time.Second)
	for i, member := range []api.IntegrationMember{one, two} {
		if _, err := projects[i].Renew(t.Context(), member.TaskID, owners[i]); err != nil {
			t.Fatal(err)
		}
	}
	f.clock.advance(46 * time.Second)
	lost, err := a.Integration(t.Context(), target)
	if err != nil || lost.Status != api.ClaimLost {
		t.Fatalf("expired target: %+v %v", lost, err)
	}
	nextIndex := 1 - winner
	next := integrationInput(t, projects[nextIndex], []api.IntegrationMember{one, two}[nextIndex], []api.ClaimGrant{first, second}[nextIndex].Claim.ExecutionID, "integration-successor")
	successor, err := projects[nextIndex].ClaimIntegration(t.Context(), "explicit-target-handoff", next, owners[nextIndex])
	if err != nil || successor.Integration.Generation != 2 {
		t.Fatalf("target handoff: %+v %v", successor, err)
	}
	if _, err := projects[winner].RenewIntegration(t.Context(), target, old); !api.IsCode(err, api.CodeLeaseInvalid) {
		t.Fatalf("old target lease renewed: %v", err)
	}
	if _, err := projects[winner].IntegrationAction(t.Context(), "late-old-push", target, api.IntegrationCommand{Action: "pushing", ExpectedRevision: granted.Integration.Revision}, old); !api.IsCode(err, api.CodeLeaseInvalid) {
		t.Fatalf("late old intent: %v", err)
	}
	for _, member := range []api.IntegrationMember{one, two} {
		current, err := a.Show(t.Context(), member.TaskID)
		if err != nil || current.Revision != member.Revision {
			t.Fatal("target ownership changed card content")
		}
	}
	replay, err := projects[nextIndex].ClaimIntegration(t.Context(), "explicit-target-handoff", next, owners[nextIndex])
	if err != nil || replay.Token != successor.Token || replay.Integration.Revision != successor.Integration.Revision {
		t.Fatalf("target receipt replay: %+v %v", replay, err)
	}
}

func TestIntegrationIntentAndConfirmationSurviveRestartWithoutCardWrites(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	member, taskGrant := integrationTask(t, f, p, "host-a", "integrate-restart")
	input := integrationInput(t, p, member, taskGrant.Claim.ExecutionID, "target-executor")
	grant, err := p.ClaimIntegration(t.Context(), "target-claim", input, client.Claim{Token: taskGrant.Token, Generation: taskGrant.Claim.Generation})
	if err != nil {
		t.Fatal(err)
	}
	proof := client.Claim{Token: grant.Token, Generation: grant.Integration.Generation}
	observation, err := p.IntegrationObservation(t.Context(), grant.Integration.ID)
	if err != nil {
		t.Fatal(err)
	}
	pushing, err := p.IntegrationAction(t.Context(), "reserve-push", grant.Integration.ID, api.IntegrationCommand{Action: "pushing", ExpectedRevision: grant.Integration.Revision, ObservedTarget: input.ExpectedTarget, ObservationTicket: observation.Ticket}, proof)
	if err != nil || pushing.Phase != "pushing" {
		t.Fatalf("push intent: %+v %v", pushing, err)
	}
	dir, clock := f.dir, f.clock
	f.stop()
	f = start(t, dir, clock)
	p = f.project("host-a")
	recovered, err := p.Integration(t.Context(), grant.Integration.ID)
	if err != nil || recovered.Phase != "pushing" || recovered.Revision != pushing.Revision {
		t.Fatalf("lost uncertain intent: %+v %v", recovered, err)
	}
	observation, err = p.IntegrationObservation(t.Context(), grant.Integration.ID)
	if err != nil {
		t.Fatal(err)
	}
	confirmation := api.IntegrationCommand{Action: "confirm", ExpectedRevision: pushing.Revision, ObservedTarget: input.Commit, ObservedAt: time.Now().UTC(), ObservationTicket: observation.Ticket, Edges: []api.IntegrationEdge{{Ancestor: input.Commit, Descendant: input.Commit}}}
	complete, err := p.IntegrationAction(t.Context(), "confirm-pushed-sha", grant.Integration.ID, confirmation, proof)
	if err != nil || complete.Phase != "integrated" || complete.Status != api.ClaimReleased {
		t.Fatalf("confirmation: %+v %v", complete, err)
	}
	again, err := p.IntegrationAction(t.Context(), "confirm-pushed-sha", grant.Integration.ID, confirmation, proof)
	if err != nil || again.Revision != complete.Revision {
		t.Fatalf("confirmation receipt: %+v %v", again, err)
	}
	current, err := p.Show(t.Context(), member.TaskID)
	if err != nil || current.Revision != member.Revision {
		t.Fatal("target facts changed card revision")
	}
	if _, err := p.RenewIntegration(t.Context(), grant.Integration.ID, proof); !api.IsCode(err, api.CodeLeaseInvalid) {
		t.Fatalf("completed target renewed: %v", err)
	}
}

func TestIntegrationRejectsStaleFactsAndAllowsAuditedReleaseAfterProgress(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	member, taskGrant := integrationTask(t, f, p, "host-a", "integrate-facts")
	owner := client.Claim{Token: taskGrant.Token, Generation: taskGrant.Claim.Generation}
	input := integrationInput(t, p, member, taskGrant.Claim.ExecutionID, "stale-observation")
	f.clock.advance(31 * time.Second)
	if _, err := p.ClaimIntegration(t.Context(), "stale-target-observation", input, owner); !api.IsCode(err, api.CodeEvidenceIncomplete) {
		t.Fatalf("stale target facts accepted: %v", err)
	}
	input = integrationInput(t, p, member, taskGrant.Claim.ExecutionID, "missing-edges")
	input.Edges = nil
	if _, err := p.ClaimIntegration(t.Context(), "missing-target-edges", input, owner); !api.IsCode(err, api.CodeEvidenceIncomplete) {
		t.Fatalf("missing ancestry accepted: %v", err)
	}
	input = integrationInput(t, p, member, taskGrant.Claim.ExecutionID, "current-material")
	grant, err := p.ClaimIntegration(t.Context(), "current-target", input, owner)
	if err != nil {
		t.Fatal(err)
	}
	proof := client.Claim{Token: grant.Token, Generation: grant.Integration.Generation}
	updated, err := p.PutDocument(t.Context(), "later-member-progress", member.TaskID, api.Document{Path: "progress.md", Text: "progress after target reservation", ExpectedRevision: member.Revision}, &owner)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := p.IntegrationObservation(t.Context(), grant.Integration.ID)
	if err != nil {
		t.Fatal(err)
	}
	command := api.IntegrationCommand{Action: "pushing", ExpectedRevision: grant.Integration.Revision, ObservedTarget: input.ExpectedTarget, ObservationTicket: observation.Ticket}
	if _, err := p.IntegrationAction(t.Context(), "stale-push-intent", grant.Integration.ID, command, proof); !api.IsCode(err, api.CodeRevisionConflict) {
		t.Fatalf("changed member authorized push: %v", err)
	}
	command.Action, command.Decision = "release", "explicitly abandon stale material after inspecting the unchanged remote"
	released, err := p.IntegrationAction(t.Context(), "release-stale-reservation", grant.Integration.ID, command, proof)
	if err != nil || released.Status != api.ClaimReleased || released.Phase != "released" {
		t.Fatalf("stale reservation could not release: %+v %v", released, err)
	}
	current, err := p.Show(t.Context(), member.TaskID)
	if err != nil || current.Revision != updated.Revision {
		t.Fatal("target cleanup changed member progress")
	}
}

func TestIntegrationRequiresCompleteGroupAndPrivateCoordinatorAuthority(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	request := groupRequest(t, p)
	coordinator, err := p.ClaimGroup(t.Context(), "integration-coordinator", request.Request.GroupID, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	base := strings.Repeat("a", 40)
	target := "refs/heads/group/" + request.Request.GroupID
	observation, err := p.IntegrationObservation(t.Context(), api.IntegrationID(target))
	if err != nil {
		t.Fatal(err)
	}
	input := api.IntegrationRequest{Purpose: "initialize", TargetRef: target, BaseCommit: base, Commit: base, GroupID: request.Request.GroupID, ExecutionID: "group-initialization", ActorExecutionID: request.ExecutionID, Decision: "initialize the confirmed group anchor", ObservationTicket: observation.Ticket, Edges: []api.IntegrationEdge{{Ancestor: base, Descendant: base}}}
	for _, id := range request.Request.Members {
		task, err := p.Show(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		input.Members = append(input.Members, api.IntegrationMember{TaskID: id, Revision: task.Revision})
	}
	proof := client.Claim{Token: coordinator.Token, Generation: coordinator.Claim.Generation}
	incomplete := input
	incomplete.Members = input.Members[:1]
	if _, err := p.ClaimIntegration(t.Context(), "incomplete-group", incomplete, proof); err == nil {
		t.Fatal("omitted group member was accepted")
	}
	publicLabel := client.Claim{Token: coordinator.Checkpoint.Authority.Token, Generation: coordinator.Claim.Generation}
	if _, err := p.ClaimIntegration(t.Context(), "public-coordinator-label", input, publicLabel); !api.IsCode(err, api.CodeHeldByOther) {
		t.Fatalf("public label granted integration rights: %v", err)
	}
	grant, err := p.ClaimIntegration(t.Context(), "complete-group-anchor", input, proof)
	if err != nil || grant.Integration.Request.GroupID != request.Request.GroupID {
		t.Fatalf("group initialization claim: %+v %v", grant, err)
	}
	for _, member := range input.Members {
		task, err := p.Show(t.Context(), member.TaskID)
		if err != nil || task.State != "todo" || task.Revision != member.Revision {
			t.Fatal("group target claim wrote a member")
		}
	}
}

func TestIntegrationCanConfirmAlreadyContainedSourceAndRetainHistoricalResult(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	member, taskGrant := integrationTask(t, f, p, "host-a", "integrate-contained")
	input := integrationInput(t, p, member, taskGrant.Claim.ExecutionID, "contained-source")
	input.ExpectedTarget, input.BaseCommit = strings.Repeat("c", 40), strings.Repeat("c", 40)
	input.AlreadyContained = true
	input.Edges = []api.IntegrationEdge{{Ancestor: input.Commit, Descendant: input.ExpectedTarget}, {Ancestor: member.DeliveryCommit, Descendant: input.Commit}}
	grant, err := p.ClaimIntegration(t.Context(), "contained-target-claim", input, client.Claim{Token: taskGrant.Token, Generation: taskGrant.Claim.Generation})
	if err != nil {
		t.Fatal(err)
	}
	proof := client.Claim{Token: grant.Token, Generation: grant.Integration.Generation}
	observation, err := p.IntegrationObservation(t.Context(), grant.Integration.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.IntegrationAction(t.Context(), "contained-cannot-push", grant.Integration.ID, api.IntegrationCommand{Action: "pushing", ExpectedRevision: grant.Integration.Revision, ObservedTarget: input.ExpectedTarget, ObservationTicket: observation.Ticket}, proof); !api.IsCode(err, api.CodeEvidenceIncomplete) {
		t.Fatalf("read-only reconciliation authorized push: %v", err)
	}
	confirmed, err := p.IntegrationAction(t.Context(), "confirm-contained-source", grant.Integration.ID, api.IntegrationCommand{Action: "confirm", ExpectedRevision: grant.Integration.Revision, ObservedTarget: input.ExpectedTarget, ObservedAt: time.Now().UTC(), ObservationTicket: observation.Ticket, Edges: []api.IntegrationEdge{{Ancestor: input.Commit, Descendant: input.ExpectedTarget}}}, proof)
	if err != nil || confirmed.Phase != "integrated" {
		t.Fatalf("contained confirmation: %+v %v", confirmed, err)
	}
	next := integrationInput(t, p, member, taskGrant.Claim.ExecutionID, "later-contained-operation")
	next.ExpectedTarget, next.BaseCommit, next.AlreadyContained = input.ExpectedTarget, input.BaseCommit, true
	next.Edges = input.Edges
	if _, err := p.ClaimIntegration(t.Context(), "later-target-operation", next, client.Claim{Token: taskGrant.Token, Generation: taskGrant.Claim.Generation}); err != nil {
		t.Fatal(err)
	}
	old, err := p.IntegrationExecution(t.Context(), grant.Integration.ID, input.ExecutionID)
	if err != nil || old.Phase != "integrated" || old.Generation != confirmed.Generation || old.ObservedTarget != input.ExpectedTarget {
		t.Fatalf("lost completed integration history: %+v %v", old, err)
	}
}
