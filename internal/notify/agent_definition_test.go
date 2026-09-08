package notify

import (
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/liveness"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	got := TmuxNotifyProbe("tmux", "%9", liveness.TaskSession{Agent: "claude", Reference: "session-1"}, 0)
	if got.State != "ready" {
		t.Fatalf("%+v", got)
	}
}

func TestNotifyNoneUsesFreshRecovery(t *testing.T) {
	root, bin := setupBoard(t)
	executable := filepath.Join(bin, "claude")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Launcher = "herdr"
	cfg.Agents = map[string]config.AgentDefinition{"claude": {Path: executable, Args: &config.AgentArgs{Start: []string{"fresh-start"}}, Session: &config.AgentSessionDefinition{Mode: "none"}}}
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_WORKSPACE_ID", "w1")
	t.Setenv("KANBAN_HERDR_AGENT", "claude")
	t.Setenv("KANBAN_HERDR_SESSION", "session-1")
	id, path := makeReview(t, root, "no-session")
	setWindow(t, path, "herdr:w1:t9:w1:p9")
	_, _, err := capture(t, func() error { return commandNotify(root, id, "recover", "", "", true, 61) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "herdr.log.prompt")); !os.IsNotExist(err) {
		t.Fatal("none sent direct prompt")
	}
	sent, err := os.ReadFile(filepath.Join(root, "herdr.log.run"))
	if err != nil || !strings.Contains(string(sent), "fresh-start") || strings.Contains(string(sent), "--resume") {
		t.Fatalf("%s %v", sent, err)
	}
}
