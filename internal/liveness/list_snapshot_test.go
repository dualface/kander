package liveness

import (
	"encoding/json"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/terminal"
)

func listSnapshot(t *testing.T, args ...string) board.ListSnapshot {
	t.Helper()
	code, out, stderr := capture(t, func() int { return RunList(append([]string{"--json"}, args...)) })
	if code != 0 {
		t.Fatalf("list --json %v: code=%d stderr=%s", args, code, stderr)
	}
	var snapshot board.ListSnapshot
	if err := json.Unmarshal([]byte(out), &snapshot); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	return snapshot
}

func TestRunListAgentStatusUsesFakeBackend(t *testing.T) {
	tempBoard(t)
	fake := &fakeAgentBackend{name: "fake", identity: true, lister: true, panes: []terminal.AgentPane{
		{Pane: "p1", Agent: "claude", AgentStatus: "working", AgentSession: "s1", AgentSessionKind: "id"},
	}}
	useFakeBackends(t, fake)
	id, path := makeWorking(t, "list-agent", "list agent")
	setLocation(t, path, "claude s1", "fake:p1")
	other, _ := makeWorking(t, "list-no-window", "no window")

	plain := listSnapshot(t)
	if fake.listCalls != 0 || len(fake.factsCalls) != 0 {
		t.Fatalf("list --json probed: list=%d facts=%v", fake.listCalls, fake.factsCalls)
	}
	for _, task := range plain.Tasks {
		if task.Agent != nil {
			t.Fatalf("%s agent without --agent-status", task.TaskID)
		}
	}

	statuses := map[string]bool{Alive: true, Stopped: true, Drifted: true, Unknown: true, AliveOrphaned: true, IdentityMismatch: true, Unprobeable: true}
	runtimes := map[string]bool{"idle": true, "working": true, "blocked": true, "done": true, Unknown: true}
	observed := listSnapshot(t, "--agent-status")
	if fake.listCalls != 1 {
		t.Fatalf("list calls %d", fake.listCalls)
	}
	for _, task := range observed.Tasks {
		switch task.TaskID {
		case id:
			agent := task.Agent
			if agent == nil || agent.Status != Alive || agent.RuntimeState != "working" || !agent.Valid || agent.ObservedAt == "" {
				t.Fatalf("observed agent %+v", agent)
			}
			if !statuses[agent.Status] || !runtimes[agent.RuntimeState] {
				t.Fatalf("value outside the contract: %+v", agent)
			}
		case other:
			if task.Agent != nil {
				t.Fatalf("card without WINDOW has agent %+v", task.Agent)
			}
		}
	}
	if again := listSnapshot(t, "--agent-status"); again.ChangeToken != observed.ChangeToken {
		t.Fatal("an unchanged agent changed the token")
	}

	fake.panes[0].AgentStatus = "blocked"
	if blocked := listSnapshot(t, "--agent-status"); blocked.ChangeToken == observed.ChangeToken {
		t.Fatal("an agent change did not change the token")
	}
}
