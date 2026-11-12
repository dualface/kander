package remote

import (
	"context"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

func TestMachineListObservesOnlyThisHostAndIgnoresRenewalTimestamps(t *testing.T) {
	c, err := client.New("http://example.invalid", "host-a")
	if err != nil {
		t.Fatal(err)
	}
	s := &Session{Client: c, Info: api.Project{ID: "project-1", Name: "test"}}
	now := time.Now()
	list := api.TaskList{Tasks: []api.TaskSummary{
		{TaskID: "20261112-local-task", State: "working", Window: "tmux:$1:@1:%1", Session: "codex local", Claim: &api.ClaimView{ClientID: "host-a", Generation: 1, Status: api.ClaimActive, RenewedAt: now, ExpiresAt: now.Add(time.Minute)}},
		{TaskID: "20261112-foreign-task", State: "working", Window: "tmux:$1:@1:%1", Session: "codex foreign", Claim: &api.ClaimView{ClientID: "host-b", Generation: 1, Status: api.ClaimActive}},
	}}
	observe := func(_ context.Context, requests []board.AgentStatusRequest) []board.ListAgent {
		if len(requests) != 1 || requests[0].TaskID != list.Tasks[0].TaskID {
			t.Fatalf("foreign pane reached this host's observer: %+v", requests)
		}
		return []board.ListAgent{{Status: "alive", RuntimeState: "working", Valid: true, ObservedAt: time.Now().Format(time.RFC3339Nano)}}
	}
	first, err := s.machineList(t.Context(), list, observe)
	if err != nil {
		t.Fatal(err)
	}
	if first.Tasks[1].Window != "" || first.Tasks[1].Session != "" || first.Tasks[1].Agent != nil {
		t.Fatal("foreign execution was exposed as a local terminal")
	}
	list.Tasks[0].Claim.RenewedAt = now.Add(15 * time.Second)
	list.Tasks[0].Claim.ExpiresAt = now.Add(2 * time.Minute)
	second, err := s.machineList(t.Context(), list, observe)
	if err != nil {
		t.Fatal(err)
	}
	if first.ChangeToken != second.ChangeToken {
		t.Fatal("heartbeat or observation timestamp changed the content token")
	}
	list.Tasks[1].Claim.Status = api.ClaimLost
	third, err := s.machineList(t.Context(), list, observe)
	if err != nil {
		t.Fatal(err)
	}
	if third.ChangeToken == second.ChangeToken || third.Tasks[1].Agent == nil || third.Tasks[1].Agent.Status != "stopped" {
		t.Fatal("lost remote execution did not change the snapshot")
	}
}
