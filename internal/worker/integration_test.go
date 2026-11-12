package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/remote"
)

func (h *harness) integration() (*client.Project, remote.Credential) {
	t := h.t
	cl, err := client.New(h.http.URL, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	info, err := cl.Register(t.Context(), "integration-worker", "git@example.invalid:integration.git")
	if err != nil {
		t.Fatal(err)
	}
	p := cl.Project(info.ID)
	task, err := p.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "integration-worker", Title: "Supervise an integration command", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	group := "20261112-worker-integration-group"
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: "+group, 1)
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\nCARD_REVIEW: PASS\n", 1)
	task, err = p.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil)
	if err != nil {
		t.Fatal(err)
	}
	task, err = p.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := p.ClaimGroup(t.Context(), "", group, api.GroupClaim{ExecutionID: "integration-coordinator", Request: board.CoordinatorClaim{GroupID: group, Owner: "coordinator", Token: "public-integration", Basis: "fixture", Members: []string{task.TaskID}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	target := "refs/heads/group/" + group
	observation, err := p.IntegrationObservation(t.Context(), api.IntegrationID(target))
	if err != nil {
		t.Fatal(err)
	}
	commit := strings.Repeat("a", 40)
	input := api.IntegrationRequest{Purpose: "initialize", TargetRef: target, BaseCommit: commit, Commit: commit, ExecutionID: "integration-command", ActorExecutionID: coordinator.Claim.ExecutionID, Decision: "explicitly initialize group target", ObservationTicket: observation.Ticket, Members: []api.IntegrationMember{{TaskID: task.TaskID, Revision: task.Revision}}, GroupID: group, Edges: []api.IntegrationEdge{{Ancestor: commit, Descendant: commit}}}
	grant, err := p.ClaimIntegration(t.Context(), "", input, client.Claim{Token: coordinator.Token, Generation: coordinator.Claim.Generation})
	if err != nil {
		t.Fatal(err)
	}
	c := remote.Credential{ResourceKind: "integration", AuthorityExecutionID: coordinator.Claim.ExecutionID, ServerURL: h.http.URL, ClientID: "host-a", ProjectID: p.ID(), TaskID: grant.Integration.ID, ExecutionID: input.ExecutionID, Generation: grant.Integration.Generation, Token: grant.Token, RenewInterval: grant.RenewInterval, TTL: grant.TTL, RenewedAt: grant.Integration.RenewedAt, AgentPID: 4242}
	if err := remote.WriteCredential(h.home, c); err != nil {
		t.Fatal(err)
	}
	return p, c
}
func TestWorkerRenewsIntegrationWithoutTaskObservationOrDelivery(t *testing.T) {
	h := newHarness(t)
	p, c := h.integration()
	h.loop.o.Observe = func(context.Context, remote.Credential) api.AgentObservation {
		return api.AgentObservation{Status: "alive"}
	}
	for range 3 {
		h.advance(16 * time.Second)
		if live, err := h.loop.pass(t.Context()); err != nil || live != 1 {
			t.Fatalf("integration renewal: %d %v", live, err)
		}
	}
	value, err := p.Integration(t.Context(), c.TaskID)
	if err != nil || value.Status != api.ClaimActive || value.Revision != 1 || !value.RenewedAt.Equal(h.now()) {
		t.Fatalf("integration renew: %+v %v", value, err)
	}
	if len(h.lines) != 0 || len(h.stopped) != 0 {
		t.Fatal("integration worker delivered a task message or stopped a live command")
	}
}
func TestWorkerStopsIntegrationCommandAfterLossAndPreservesIdentity(t *testing.T) {
	h := newHarness(t)
	_, c := h.integration()
	h.http.Close()
	h.advance(91 * time.Second)
	if live, _ := h.loop.pass(t.Context()); live != 0 {
		t.Fatal("partitioned integration remained live")
	}
	got := h.credential()
	if !got.Lost || got.AuthorityExecutionID != c.AuthorityExecutionID || len(h.stopped) != 1 || h.stopped[0] != 4242 {
		t.Fatalf("lost integration: %+v %v", got, h.stopped)
	}
	if live, _ := h.loop.pass(t.Context()); live != 0 || len(h.stopped) != 1 {
		t.Fatal("uncertain stop was repeated")
	}
}
func TestWorkerRemovesReleasedIntegrationWithoutStoppingCommand(t *testing.T) {
	h := newHarness(t)
	p, c := h.integration()
	observation, err := p.IntegrationObservation(t.Context(), c.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.IntegrationAction(t.Context(), "release-command", c.TaskID, api.IntegrationCommand{Action: "release", ExpectedRevision: 1, ObservationTicket: observation.Ticket, Decision: "explicitly abandon before push"}, *c.Claim()); err != nil {
		t.Fatal(err)
	}
	h.advance(16 * time.Second)
	if live, _ := h.loop.pass(t.Context()); live != 0 {
		t.Fatal("released integration remained live")
	}
	all, err := remote.Credentials(h.home)
	if err != nil || len(all) != 0 || len(h.stopped) != 0 {
		t.Fatalf("normal integration end: %+v %v", all, err)
	}
}
