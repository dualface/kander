package menu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/config"
)

// unusableCodexScope writes a scope config whose built-in codex entry this version cannot use
// (an unregistered session hook), the way a newer release could leave it, and loads it.
func unusableCodexScope(t *testing.T, mutate func(doc map[string]any)) (string, *config.Config) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(config.EnvConfig, path)
	base := config.DefaultConfig()
	base.WelcomeComplete = true
	base.Language = "en"
	data, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	doc["agents"] = map[string]any{"codex": map[string]any{
		"args":    map[string]any{"start": []any{}, "resume": []any{}},
		"session": map[string]any{"mode": "hook:future-hook"},
	}}
	if mutate != nil {
		mutate(doc)
	}
	writeDoc(t, path, doc)
	cfg, err := config.Load(false)
	if err != nil {
		t.Fatalf("a configuration with an unusable built-in agent must load: %v", err)
	}
	return path, cfg
}

// After the Global tab was saved in a session, the persisted scope document is the base of the
// Project tab: restoring an Options page must still see the built-in agent as unusable and keep
// the overlay's models for it.
func TestRestoreAfterAScopeSaveKeepsModelsOfAnUnusableBuiltin(t *testing.T) {
	_, cfg := unusableCodexScope(t, nil)
	session, overlayPath := attachProject(t, cfg, map[string]any{
		"models": map[string]any{"kanban": map[string]any{"codex": map[string]any{"large_model": "overlay-model"}}},
		"tui":    map[string]any{"refresh": 45},
	})
	if err := session.SetTarget(config.TargetScope); err != nil {
		t.Fatal(err)
	}
	session.SetTUIField("theme", "dark")
	if _, err := session.Save(); err != nil {
		t.Fatal(err)
	}
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	if err := session.RestoreSection("execution"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Save(); err != nil {
		t.Fatal(err)
	}
	saved := readDoc(t, overlayPath)
	if got, _ := config.OverlayGet(saved, "models", "kanban", "codex", "large_model"); got != "overlay-model" {
		t.Fatalf("restoring the page after a scope save deleted the models of an unusable built-in agent: %v", saved)
	}
}

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
