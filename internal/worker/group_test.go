package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/remote"
)

func TestWorkerRenewsGroupLeaseAndReportsCoordinatorExit(t *testing.T) {
	h := newHarness(t)
	project := h.project("host-a")
	group := "20261112-worker-group"
	task, err := project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "group-worker", Title: "Group worker fixture", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: "+group, 1)
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\nCARD_REVIEW: PASS\n", 1)
	if task, err = project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
		t.Fatal(err)
	}
	if task, err = project.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
		t.Fatal(err)
	}
	request := api.GroupClaim{ExecutionID: "coordinator-execution", Request: board.CoordinatorClaim{GroupID: group, Owner: "coordinator", Token: "public-label", Basis: "group supervision fixture", Members: []string{task.TaskID}}}
	grant, err := project.ClaimGroup(t.Context(), "", group, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := remote.Credential{ResourceKind: "group", ServerURL: h.http.URL, ClientID: "host-a", ProjectID: project.ID(), TaskID: group, ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, Token: grant.Token, RenewInterval: grant.RenewInterval, TTL: grant.TTL, RenewedAt: grant.Claim.RenewedAt, AgentPID: 4242}
	if err := remote.WriteCredential(h.home, c); err != nil {
		t.Fatal(err)
	}
	status := "alive"
	h.loop.o.Observe = func(context.Context, remote.Credential) api.AgentObservation {
		return api.AgentObservation{Status: status, Reason: "confirmed coordinator process"}
	}
	for range 10 {
		h.advance(16 * time.Second)
		if live, err := h.loop.pass(t.Context()); err != nil || live != 1 {
			t.Fatalf("group renewal: %d %v", live, err)
		}
	}
	view, err := project.Group(t.Context(), group)
	if err != nil || view.Claim == nil || view.Claim.Status != api.ClaimActive || view.Checkpoint.Revision != grant.Checkpoint.Revision {
		t.Fatalf("group supervision modified its checkpoint: %+v %v", view, err)
	}
	after, err := project.Show(t.Context(), task.TaskID)
	if err != nil || after.Revision != task.Revision {
		t.Fatal("group renewal modified a member card")
	}
	status = "stopped"
	h.advance(16 * time.Second)
	if live, err := h.loop.pass(t.Context()); err != nil || live != 0 {
		t.Fatalf("exited coordinator kept its lease: %d %v", live, err)
	}
	view, err = project.Group(t.Context(), group)
	if err != nil || view.Claim.Status != api.ClaimLost || view.Claim.Observation.Status != "stopped" || len(h.stopped) != 1 {
		t.Fatalf("group exit was not reported and handled: %+v %v", view, err)
	}
	successor := h.project("host-b")
	request.ExecutionID = "successor-coordinator"
	request.ExpectedGeneration = grant.Claim.Generation
	request.Decision = "explicit takeover after coordinator exit"
	request.Request.ExpectedRevision = view.Checkpoint.Revision
	request.Request.ExpectedEpoch = view.Checkpoint.Authority.Epoch
	next, err := successor.ClaimGroup(t.Context(), "", group, request, nil)
	if err != nil || next.Claim.Generation != grant.Claim.Generation+1 {
		t.Fatalf("failed explicit group takeover: %+v %v", next, err)
	}
}
