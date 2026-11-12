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
	if third.ChangeToken == second.ChangeToken || third.Tasks[1].Agent == nil || third.Tasks[1].Agent.Status != "unknown" || third.Tasks[1].Agent.Valid {
		t.Fatal("lost remote execution did not change the snapshot")
	}
}

func TestMachineListUsesRemoteWorkerFactsWithoutInventingProcessExit(t *testing.T) {
	c, err := client.New("http://example.invalid", "host-a")
	if err != nil {
		t.Fatal(err)
	}
	session := &Session{Client: c, Info: api.Project{ID: "project"}}
	now := time.Now().UTC()
	list := api.TaskList{ObservedAt: now, Tasks: []api.TaskSummary{{TaskID: "20261112-remote-observed-task", State: "working", Window: "tmux:$9:@9:%9", Session: "codex foreign", Claim: &api.ClaimView{ClientID: "host-b", Generation: 1, ExecutionID: "remote-execution", Status: api.ClaimActive, RenewedAt: now, ExpiresAt: now.Add(90 * time.Second), Observation: &api.AgentObservation{Status: "alive", RuntimeState: "working", ObservedAt: now}}}}}
	observe := func(context.Context, []board.AgentStatusRequest) []board.ListAgent {
		t.Fatal("foreign pane was probed locally")
		return nil
	}
	first, err := session.machineList(t.Context(), list, observe)
	if err != nil {
		t.Fatal(err)
	}
	if agent := first.Tasks[0].Agent; agent == nil || agent.Status != "alive" || !agent.Valid || first.Tasks[0].Window != "" {
		t.Fatalf("worker observation lost or foreign address exposed: %+v", first.Tasks[0])
	}
	list.ObservedAt = now.Add(15 * time.Second)
	list.Tasks[0].Claim.RenewedAt = list.ObservedAt
	list.Tasks[0].Claim.ExpiresAt = list.ObservedAt.Add(90 * time.Second)
	list.Tasks[0].Claim.Observation.ObservedAt = list.ObservedAt
	second, err := session.machineList(t.Context(), list, observe)
	if err != nil {
		t.Fatal(err)
	}
	if first.ChangeToken != second.ChangeToken {
		t.Fatal("observation heartbeat changed content token")
	}
	list.Tasks[0].Claim.Status = api.ClaimLost
	lost, err := session.machineList(t.Context(), list, observe)
	if err != nil {
		t.Fatal(err)
	}
	if agent := lost.Tasks[0].Agent; agent.Status != "unknown" || agent.Valid {
		t.Fatal("lease loss invented a confirmed process exit")
	}
	list.Tasks[0].Claim.Observation.Status = "stopped"
	stopped, err := session.machineList(t.Context(), list, observe)
	if err != nil {
		t.Fatal(err)
	}
	if agent := stopped.Tasks[0].Agent; agent.Status != "stopped" || !agent.Valid {
		t.Fatal("confirmed agent exit was discarded")
	}
}
