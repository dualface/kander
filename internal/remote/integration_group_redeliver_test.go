package remote_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
)

// redeliverGroup is a reviewed group of three members whose deliveries all
// name the fixture commit: the first two are held by host-a and host-b, the
// third by host-a.
type redeliverGroup struct {
	f       *integrationFixture
	group   string
	ids     []string
	owners  []remote.Credential
	options remote.IntegrationOptions
}

func newRedeliverGroup(t *testing.T, group string) *redeliverGroup {
	t.Helper()
	f := newIntegrationGitFixture(t)
	g := &redeliverGroup{f: f, group: group}
	g.ids, g.owners = integrationGroupCardsOf(t, f, group, []*remote.Session{f.a, f.b, f.a})
	if _, err := f.a.AcquireCoordinator(t.Context(), board.CoordinatorClaim{GroupID: group, Owner: "coordinator", Token: "redeliver-session", Basis: "explicitly coordinate the complete group", Members: g.ids}, "explicit group coordination"); err != nil {
		t.Fatal(err)
	}
	// host-b publishes the same commit from its own clone.
	deliveryGit(t, f.first, "push", "origin", f.commit+":refs/heads/redeliver-carrier")
	deliveryGit(t, f.second, "fetch", "-q", "origin", "refs/heads/redeliver-carrier")
	for i, id := range g.ids {
		session, dir := g.session(i), g.dir(i)
		if _, err := session.PublishCommit(t.Context(), dir, id, f.commit, g.owners[i]); err != nil {
			t.Fatal(err)
		}
		if _, err := session.Project.Move(t.Context(), "", id, api.Move{State: "review", DeliveryCommit: f.commit}, g.owners[i].Claim()); err != nil {
			t.Fatal(err)
		}
	}
	integrationJointClose(t, f, g.ids, f.commit)
	g.options = remote.IntegrationOptions{OperationID: "deliver-redeliver-group", Purpose: "deliver", TargetRef: "refs/heads/v2", Commit: f.commit, GroupID: group, Decision: "integrate the reviewed group into v2"}
	return g
}

func (g *redeliverGroup) session(i int) *remote.Session {
	return []*remote.Session{g.f.a, g.f.b, g.f.a}[i]
}
func (g *redeliverGroup) dir(i int) string { return []string{g.f.first, g.f.second, g.f.first}[i] }

// complete moves member i to done on its current receipt.
func (g *redeliverGroup) complete(t *testing.T, i int) {
	t.Helper()
	session, id := g.session(i), g.ids[i]
	g.owners[i].Worktree = g.dir(i)
	if err := session.SaveCredential(g.owners[i]); err != nil {
		t.Fatal(err)
	}
	current, err := session.Project.Show(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Project.PutDocument(t.Context(), "", id, api.Document{Path: "report.md", Text: "Completed through a group integration.", ExpectedRevision: current.Revision}, g.owners[i].Claim()); err != nil {
		t.Fatal(err)
	}
	if done, err := session.MoveTask(t.Context(), id, api.Move{State: "done", Result: "completed"}); err != nil || done.State != "done" {
		t.Fatalf("completion of %s: %+v %v", id, done, err)
	}
}

// takeOver releases member i and hands it to a new execution on host-b; the
// returned credential belongs to the next generation.
func (g *redeliverGroup) takeOver(t *testing.T, i int) remote.Credential {
	t.Helper()
	session, id, own := g.session(i), g.ids[i], *g.owners[i].Claim()
	if _, err := session.Project.ObserveAgent(t.Context(), "", id, api.AgentObservation{Status: "stopped", Reason: "explicit fixture exit"}, own); err != nil {
		t.Fatal(err)
	}
	state, err := session.Project.ExecutionState(t.Context(), id, own)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Project.Execution(t.Context(), "", id, api.ExecutionUpdate{Action: api.ExecutionRelease, ExpectedRevision: state.Task.Revision, ObservationTicket: state.ObservationTicket, Decision: "the holder releases for an explicit takeover", Presence: &api.AgentObservation{Status: "stopped"}}, own); err != nil {
		t.Fatal(err)
	}
	successor, err := g.f.b.Project.Takeover(t.Context(), "", id, api.ClaimRequest{ClientID: "host-b", ExecutionID: "redeliver-successor", Owner: "codex", Decision: "explicit takeover after the group integration", ExpectedGeneration: g.owners[i].Generation})
	if err != nil {
		t.Fatal(err)
	}
	next := remote.Credential{ServerURL: g.f.b.ServerURL, ClientID: "host-b", ProjectID: g.f.b.Info.ID, TaskID: id, ExecutionID: successor.Claim.ExecutionID, Generation: successor.Claim.Generation, Token: successor.Token, Worktree: g.f.second}
	if err := g.f.b.SaveCredential(next); err != nil {
		t.Fatal(err)
	}
	if _, err := g.f.b.Project.Execution(t.Context(), "", id, api.ExecutionUpdate{Action: api.ExecutionConfirm}, *next.Claim()); err != nil {
		t.Fatal(err)
	}
	return next
}

// redeliver republishes the same commit for the new generation through an
// ID/epoch-bound sync round, the way a member in review/ delivers again.
func (g *redeliverGroup) redeliver(t *testing.T, id string, next remote.Credential) {
	t.Helper()
	ctx, p, proof := t.Context(), g.f.b.Project, *next.Claim()
	card, err := p.Show(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	input := board.DispatchInput{ID: "redeliver-sync", TaskID: id, Kind: "sync", Message: "Publish the delivery of the new generation", Base: g.f.commit}
	prepared, err := p.DispatchAction(ctx, "", id, "prepare", api.DispatchCommand{Input: &input, CardRevision: card.Revision}, proof)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Move(ctx, "", id, api.Move{State: "working", Authorization: prepared.Authorization}, &proof); err != nil {
		t.Fatal(err)
	}
	if _, err := g.f.b.PublishCommit(ctx, g.f.second, id, g.f.commit, next); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Move(ctx, "", id, api.Move{State: "review", DeliveryCommit: g.f.commit, Authorization: prepared.Authorization}, &proof); err != nil {
		t.Fatal(err)
	}
}

// A member taken over after the first group integration, while its siblings
// are already done, integrates again for its new generation: done members stay
// in the topology without contributing, the target already contains the
// commit, and the new receipt completes the wrap-up.
func TestIntegrationGroupReintegratesATakenOverMemberAfterSiblingsAreDone(t *testing.T) {
	g := newRedeliverGroup(t, "20261125-redeliver-group")
	f, ids, ctx := g.f, g.ids, t.Context()
	first, err := f.a.Integrate(ctx, f.first, g.options)
	if err != nil || first.Phase != "integrated" || f.targetHead() != f.commit {
		t.Fatalf("first group integration: %+v %v", first, err)
	}
	g.complete(t, 0)
	g.complete(t, 1)
	next := g.takeOver(t, 2)
	if next.Generation != 2 {
		t.Fatalf("successor generation = %d", next.Generation)
	}
	g.redeliver(t, ids[2], next)

	g.options.OperationID = "redeliver-taken-over-member"
	second, err := f.a.Integrate(ctx, f.first, g.options)
	if err != nil || second.Phase != "integrated" {
		t.Fatalf("re-integration: %+v %v", second, err)
	}
	if second.Request.EffectiveContainment() != api.IntegrationContained || second.ObservedTarget != f.commit || f.targetHead() != f.commit {
		t.Fatalf("re-integration moved the target or did not record containment: %+v (target %s)", second.Request, f.targetHead())
	}
	if len(second.Request.Members) != 3 {
		t.Fatalf("members: %+v", second.Request.Members)
	}
	for _, member := range second.Request.Members {
		if contributes := member.DeliveryCommit != ""; contributes != (member.TaskID == ids[2]) {
			t.Fatalf("contributors: %+v", second.Request.Members)
		}
		if member.TaskID == ids[2] && member.Generation != 2 {
			t.Fatalf("taken-over member generation: %+v", member)
		}
	}

	proof := *next.Claim()
	card, err := f.b.Project.Show(ctx, ids[2])
	if err != nil {
		t.Fatal(err)
	}
	wrap := board.DispatchIntegration{IntegrationID: second.ID, IntegrationExecution: second.Request.ExecutionID, IntegrationGeneration: second.Generation, DispatchID: "redeliver-wrap-up", TaskID: ids[2], CWD: "project:" + f.a.Info.ID, SourceCommit: f.commit, ReviewTarget: f.commit, ReviewBase: f.base, TargetCommit: second.ObservedTarget, TargetRef: second.Request.TargetRef, Author: "coordinator", Basis: "receipt of the new generation", VerifiedAt: time.Now().UTC()}
	input := board.DispatchInput{ID: wrap.DispatchID, TaskID: ids[2], Kind: "wrap-up", Message: "Record completion and clean the execution workspace", Base: f.commit, Evidence: board.DispatchEvidence{WrapUp: &board.DispatchWrapUpBinding{Artifact: board.ArtifactReference{TaskID: ids[2], Path: "dispatches/" + wrap.DispatchID + "/integration.json"}, Git: wrap}}}
	prepared, err := f.b.Project.DispatchAction(ctx, "", ids[2], "prepare", api.DispatchCommand{Input: &input, CardRevision: card.Revision}, proof)
	if err != nil {
		t.Fatalf("wrap-up on the new receipt: %v", err)
	}
	accepted, err := f.b.Project.Move(ctx, "", ids[2], api.Move{State: "working", Authorization: prepared.Authorization}, &proof)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.b.Project.PutDocument(ctx, "", ids[2], api.Document{Path: "report.md", Text: "Completed by the new generation.", ExpectedRevision: accepted.Revision, Authorization: prepared.Authorization}, &proof); err != nil {
		t.Fatal(err)
	}
	done, err := f.b.MoveTask(ctx, ids[2], api.Move{State: "done", Result: "completed", Authorization: prepared.Authorization, DeliveryCommit: second.Request.Commit})
	if err != nil || done.State != "done" {
		t.Fatalf("completion of the taken-over member: %+v %v", done, err)
	}
	for _, id := range ids[:2] {
		value, err := f.b.Project.TaskIntegration(ctx, id)
		if err != nil || value.Request.ExecutionID != first.Request.ExecutionID {
			t.Fatalf("receipt of done member %s changed: %+v %v", id, value.Request, err)
		}
	}
}

// A done member still needs its closed review target to equal the commit
// the group delivers; the service names the member and both commits.
func TestIntegrationGroupRefusesADoneMemberWithAnotherClosedTarget(t *testing.T) {
	g := newRedeliverGroup(t, "20261125-done-target-group")
	f, ctx := g.f, t.Context()
	if _, err := f.a.Integrate(ctx, f.first, g.options); err != nil {
		t.Fatal(err)
	}
	g.complete(t, 0)
	before := f.targetHead()
	other := commitFile(t, f.first, "later.txt", "later\n", "Later work")
	g.options.OperationID, g.options.Commit = "deliver-other-commit", other
	_, err := f.a.Integrate(ctx, f.first, g.options)
	if err == nil {
		t.Fatal("a done member with another closed target was integrated")
	}
	for _, want := range []string{g.ids[0], f.commit, other} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal omits %s: %v", want, err)
		}
	}
	if after := f.targetHead(); after != before {
		t.Fatalf("the refused integration moved the target from %s to %s", before, after)
	}
}

// When every member that is not excluded is done, the client refuses before
// it asks for the target lease.
func TestIntegrationGroupWithoutContributorIsRefusedBeforeTheLease(t *testing.T) {
	g := newRedeliverGroup(t, "20261125-no-contributor-group")
	f, ctx := g.f, t.Context()
	if _, err := f.a.Integrate(ctx, f.first, g.options); err != nil {
		t.Fatal(err)
	}
	for i := range g.ids {
		g.complete(t, i)
	}
	before := f.targetHead()
	claims := 0
	f.intercept = func(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/integrations/") && strings.HasSuffix(r.URL.Path, "/claim") {
			claims++
		}
		return false
	}
	g.options.OperationID = "deliver-without-contributor"
	_, err := f.a.Integrate(ctx, f.first, g.options)
	if err == nil || err.Error() != config.Text("remote.integration_no_contributor", g.group) {
		t.Fatalf("refusal: %v", err)
	}
	if claims != 0 {
		t.Fatalf("the client asked the service %d time(s)", claims)
	}
	if after := f.targetHead(); after != before {
		t.Fatalf("the refused integration moved the target from %s to %s", before, after)
	}
}

// A working/ member of a group delivery is still refused by name and state.
func TestIntegrationGroupDeliverRefusesAWorkingMember(t *testing.T) {
	f := newIntegrationGitFixture(t)
	group := "20261125-working-member-group"
	ids, owners := integrationGroupCards(t, f, group)
	if _, err := f.a.AcquireCoordinator(t.Context(), board.CoordinatorClaim{GroupID: group, Owner: "coordinator", Token: "working-member-session", Basis: "explicitly coordinate the complete group", Members: ids}, "explicit group coordination"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.PublishCommit(t.Context(), f.first, ids[0], f.commit, owners[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.Project.Move(t.Context(), "", ids[0], api.Move{State: "review", DeliveryCommit: f.commit}, owners[0].Claim()); err != nil {
		t.Fatal(err)
	}
	before := f.targetHead()
	options := remote.IntegrationOptions{OperationID: "deliver-with-working-member", Purpose: "deliver", TargetRef: "refs/heads/v2", Commit: f.commit, GroupID: group, Decision: "integrate the group"}
	_, err := f.a.Integrate(t.Context(), f.first, options)
	if err == nil || err.Error() != config.Text("remote.integration_delivery_required", ids[1], "working", f.commit) {
		t.Fatalf("refusal: %v", err)
	}
	if after := f.targetHead(); after != before {
		t.Fatalf("the refused integration moved the target from %s to %s", before, after)
	}
}

// A group member named by task ID is not a standalone delivery.
func TestIntegrationStandaloneDeliverOfAGroupMemberIsRefused(t *testing.T) {
	g := newRedeliverGroup(t, "20261125-standalone-member-group")
	options := g.options
	options.OperationID, options.GroupID, options.TaskID = "deliver-member-alone", "", g.ids[0]
	_, err := g.f.a.Integrate(t.Context(), g.f.first, options)
	if err == nil || err.Error() != config.Text("remote.integration_members_changed") {
		t.Fatalf("refusal: %v", err)
	}
}

// A done standalone card still needs a current delivery: the client does not
// apply the group relaxation to it.
func TestIntegrationStandaloneDeliverOfADoneCardIsStillRefused(t *testing.T) {
	f := newIntegrationFixture(t)
	if _, err := f.a.Integrate(t.Context(), f.first, f.options("done-standalone-first")); err != nil {
		t.Fatal(err)
	}
	f.credential.Worktree = f.first
	if err := f.a.SaveCredential(f.credential); err != nil {
		t.Fatal(err)
	}
	current, err := f.a.Project.Show(t.Context(), f.task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.Project.PutDocument(t.Context(), "", f.task.TaskID, api.Document{Path: "report.md", Text: "Completed.", ExpectedRevision: current.Revision}, f.credential.Claim()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.MoveTask(t.Context(), f.task.TaskID, api.Move{State: "done", Result: "completed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.Integrate(t.Context(), f.first, f.options("done-standalone-again")); err == nil || err.Error() != config.Text("remote.integration_delivery_required", f.task.TaskID, "done", f.commit) {
		t.Fatalf("client refusal: %v", err)
	}
}

// Requests that bypass the client: a done member that names a delivery is not
// a contributor, and a group delivery without any contributor is refused.
func TestIntegrationServiceRefusesForgedGroupDeliveries(t *testing.T) {
	g := newRedeliverGroup(t, "20261125-forged-group")
	f, ctx := g.f, t.Context()
	if _, err := f.a.Integrate(ctx, f.first, g.options); err != nil {
		t.Fatal(err)
	}
	for i := range g.ids {
		g.complete(t, i)
	}
	coordinator, found, err := f.a.CoordinatorCredential(g.group)
	if err != nil || !found {
		t.Fatalf("coordinator credential: %v %v", found, err)
	}
	var members []api.IntegrationMember
	for _, id := range g.ids {
		card, err := f.a.Project.Show(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		members = append(members, api.IntegrationMember{TaskID: id, Revision: card.Revision, Generation: card.Claim.Generation})
	}
	request := forgedDeliver(t, f, g.group, members)
	request.ActorExecutionID = coordinator.ExecutionID
	if _, err := f.a.Project.ClaimIntegration(ctx, "", request, *coordinator.Claim()); !api.IsCode(err, api.CodeEvidenceIncomplete) || !strings.Contains(err.Error(), "integration needs at least one delivered member") {
		t.Fatalf("no contributor: %v", err)
	}
	request = forgedDeliver(t, f, g.group, members)
	request.ActorExecutionID = coordinator.ExecutionID
	request.Members[0].DeliveryCommit = f.commit
	if _, err := f.a.Project.ClaimIntegration(ctx, "", request, *coordinator.Claim()); !api.IsCode(err, api.CodeLeaseInvalid) || !strings.Contains(err.Error(), "contributor must have an active review execution") {
		t.Fatalf("done contributor: %v", err)
	}
}

// forgedDeliver builds a deliver request for the target that already holds
// the fixture commit, as a client that skips its own checks would send it.
func forgedDeliver(t *testing.T, f *integrationFixture, group string, members []api.IntegrationMember) api.IntegrationRequest {
	t.Helper()
	target := "refs/heads/v2"
	observation, err := f.a.Project.IntegrationObservation(t.Context(), api.IntegrationID(target))
	if err != nil {
		t.Fatal(err)
	}
	request := api.IntegrationRequest{Purpose: "deliver", TargetRef: target, BaseCommit: f.commit, ExpectedTarget: f.commit, Commit: f.commit, GroupID: group, ExecutionID: client.NewKey(), Decision: "forged request fixture", Members: members, ObservationTicket: observation.Ticket, AlreadyContained: true, Edges: []api.IntegrationEdge{{Ancestor: f.commit, Descendant: f.commit}}}
	if observation.Integration != nil {
		request.ExpectedRevision, request.ExpectedGeneration = observation.Integration.Revision, observation.Integration.Generation
	}
	return request
}

// An excluded member, a done member and a member in review/ with its current
// delivery: only the member in review/ contributes to the group delivery.
func TestIntegrationGroupDeliverWithExcludedAndDoneMembers(t *testing.T) {
	g := newRedeliverGroup(t, "20261125-excluded-done-group")
	f, ids, ctx := g.f, g.ids, t.Context()
	if _, err := f.a.Project.Move(ctx, "", ids[0], api.Move{State: "archived", Result: "cancelled", Reason: "the user withdrew this member", Decision: "user decision fixture"}, g.owners[0].Claim()); err != nil {
		t.Fatal(err)
	}
	first, err := f.a.Integrate(ctx, f.first, g.options)
	if err != nil {
		t.Fatal(err)
	}
	g.complete(t, 1)
	g.options.OperationID = "deliver-excluded-done-group"
	second, err := f.a.Integrate(ctx, f.first, g.options)
	if err != nil || second.Phase != "integrated" || f.targetHead() != f.commit {
		t.Fatalf("delivery with excluded and done members: %+v %v", second, err)
	}
	for _, member := range second.Request.Members {
		if contributes := member.DeliveryCommit != ""; contributes != (member.TaskID == ids[2]) {
			t.Fatalf("contributors: %+v", second.Request.Members)
		}
	}
	if value, err := f.b.Project.TaskIntegration(ctx, ids[1]); err != nil || value.Request.ExecutionID != first.Request.ExecutionID {
		t.Fatalf("receipt of the done member changed: %+v %v", value.Request, err)
	}
}
