package menu

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

// unusableCodexScope writes a scope config whose built-in codex entry this version cannot use
// (an unregistered session hook), the way a newer release could leave it, and loads it.
func unusableCodexScope(t *testing.T, mutate func(doc map[string]any)) (string, *config.Config) {
	t.Helper()
	isolateOverlay(t)
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
