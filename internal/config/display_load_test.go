package config

import (
	"path/filepath"
	"testing"
)

// An unusable built-in entry hides the models.* entries of its agent from the loader. The
// reference configuration brings the agent back, and with it those entries as settings; a value
// this version cannot read there leaves the reference without a result, and the command shows
// the configuration the other commands use instead of failing.
func TestLoadWithDisplayShowsTheLoadedConfigurationWhenTheReferenceDoesNotLoad(t *testing.T) {
	setupHome(t)
	doc := scopeDocument(t)
	OverlaySet(doc, map[string]any{
		"args":    map[string]any{"start": []any{}, "resume": []any{}},
		"session": map[string]any{"mode": "hook:future-hook"},
	}, "agents", "codex")
	OverlaySet(doc, map[string]any{"id": "future-model"}, "models", "kanban", "codex", "large_model")
	writeScope(t, doc)
	t.Chdir(t.TempDir())
	layered, err := LoadWithDisplay(false)
	if err != nil {
		t.Fatal(err)
	}
	if layered.Display != layered.Loaded {
		t.Fatalf("display is not the loaded configuration: %s", marshalConfig(t, layered.Display))
	}
	if _, bad := layered.Loaded.UnusableAgents["codex"]; !bad {
		t.Fatal("codex is usable")
	}
}

// The files are read once and the saved language comes from that read: the layers that save a
// language bind it, the ones that save none leave the binding empty.
func TestLoadWithDisplayBindsTheSavedLanguage(t *testing.T) {
	setupHome(t)
	doc := scopeDocument(t)
	doc["language"] = "ja"
	writeScope(t, doc)
	project := t.TempDir()
	t.Chdir(project)
	writeJSONFile(t, filepath.Join(project, OverlayFilename), map[string]any{"language": "cn"})
	layered, err := LoadWithDisplay(false)
	if err != nil {
		t.Fatal(err)
	}
	if layered.Language != "cn" || BoundConfigLanguage() != "cn" {
		t.Fatalf("language=%q bound=%q, want the overlay's cn", layered.Language, BoundConfigLanguage())
	}
}
