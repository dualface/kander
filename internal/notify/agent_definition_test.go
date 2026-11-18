package notify

import (
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/liveness"
	"github.com/dualface/kander/internal/terminal"
)

func tmuxBackend(t *testing.T) terminal.Backend {
	t.Helper()
	return backendFor(t, "tmux")
}

func backendFor(t *testing.T, name string) terminal.Backend {
	t.Helper()
	backend, ok := terminal.Lookup(name)
	if !ok {
		t.Fatalf("backend %q is not registered", name)
	}
	return backend
}

func TestNotifyConfiguredProcessName(t *testing.T) {
	_, _ = setupBoard(t)
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Agents = map[string]config.AgentDefinition{"claude": {ProcessName: "node"}}
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANBAN_TMUX_CURRENT_COMMAND", "node")
	t.Setenv("KANBAN_TMUX_SESSION", "session-1")
	got := ProcessNotifyProbe(tmuxBackend(t), "tmux", "%9", liveness.TaskSession{Agent: "claude", Reference: "session-1"}, 0)
	if got.State != "ready" {
		t.Fatalf("%+v", got)
	}
}

func TestNotifyBuiltinPiProcessName(t *testing.T) {
	_, _ = setupBoard(t)
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANBAN_TMUX_SESSION", "session-1")
	session := liveness.TaskSession{Agent: "pi", Reference: "session-1"}
	t.Setenv("KANBAN_TMUX_CURRENT_COMMAND", "pi")
	if got := ProcessNotifyProbe(tmuxBackend(t), "tmux", "%9", session, 0); got.State != "ready" {
		t.Fatalf("pi foreground process rejected: %+v", got)
	}
	t.Setenv("KANBAN_TMUX_CURRENT_COMMAND", "node")
	if got := ProcessNotifyProbe(tmuxBackend(t), "tmux", "%9", session, 0); got.State == "ready" {
		t.Fatalf("non-pi foreground process accepted: %+v", got)
	}
}
