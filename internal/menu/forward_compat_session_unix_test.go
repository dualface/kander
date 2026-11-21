//go:build unix

package menu

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/config"
)

// Before the welcome completes, opening Options picks installed agents for the roles that name
// one that is missing, but a role that names an agent only a newer version knows stays as it is.
func TestOptionsBeforeWelcomeKeepsUnknownAgentReferences(t *testing.T) {
	h := newHarness(t)
	t.Setenv("PATH", h.fakeBin)
	for _, name := range []string{"claude", "codex"} {
		if err := os.WriteFile(filepath.Join(h.fakeBin, name), []byte("#!/bin/sh\necho fixture\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(config.EnvConfig, h.configPath)
	doc := defaultPayload(map[string]any{"welcome_complete": false, "kanban_agent": "newagent", "launcher": "foreground", "language": "en"})
	doc["kanban_agents"] = map[string]any{"large": "newagent", "small": "newagent"}
	doc["reviewers"] = map[string]any{
		"large": map[string]any{"PMQA": "newagent", "Security": "newagent"},
		"small": map[string]any{"PMQA": "newagent", "Security": "newagent"},
	}
	h.writeConfig(doc)
	existing, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewSession(existing, true)
	if err != nil {
		t.Fatal(err)
	}
	cfg := session.Config
	if cfg.KanbanAgent != "newagent" || cfg.KanbanAgents["large"] != "newagent" || cfg.KanbanAgents["small"] != "newagent" ||
		cfg.Reviewers["large"]["PMQA"] != "newagent" || cfg.Reviewers["small"]["Security"] != "newagent" {
		t.Fatalf("the preparation replaced a legal unknown agent reference: %q %v %v", cfg.KanbanAgent, cfg.KanbanAgents, cfg.Reviewers)
	}
}

// A task scale that has to fall back before the welcome completes takes an agent that is
// installed, never the unknown agent kept for kanban_agent: the legacy mirror is not a usable
// choice, and the saved file must start small tasks.
func TestOptionsBeforeWelcomeFallsBackToAnInstalledAgentAndKeepsTheUnknownMirror(t *testing.T) {
	h := newHarness(t)
	t.Setenv("PATH", h.fakeBin)
	for _, name := range []string{"codex", "grok"} {
		if err := os.WriteFile(filepath.Join(h.fakeBin, name), []byte("#!/bin/sh\necho fixture\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(config.EnvConfig, h.configPath)
	doc := defaultPayload(map[string]any{"welcome_complete": false, "kanban_agent": "futureagent", "launcher": "foreground", "language": "en"})
	// large is installed; small names a built-in agent this machine lacks.
	doc["kanban_agents"] = map[string]any{"large": "codex", "small": "claude"}
	h.writeConfig(doc)
	existing, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewSession(existing, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Save(); err != nil {
		t.Fatal(err)
	}
	saved, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if saved.KanbanAgent != "futureagent" || saved.KanbanAgents["large"] != "codex" {
		t.Fatalf("the unknown mirror or the installed large agent changed: %q %v", saved.KanbanAgent, saved.KanbanAgents)
	}
	if small := saved.KanbanAgents["small"]; !config.HasAgent(saved, small) || small == "claude" {
		t.Fatalf("small must fall back to an installed agent, got %q", small)
	}
}
