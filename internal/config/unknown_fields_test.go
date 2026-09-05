package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSaveDropsUnknownTopLevelFields(t *testing.T) {
	root := setupHome(t)
	path := filepath.Join(root, "config.json")
	t.Setenv(EnvConfig, path)
	cfg := DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Language = "en"
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	payload["retired_feature"] = map[string]any{"enabled": true}
	data, err = json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.WelcomeComplete || loaded.Language != "en" {
		t.Fatalf("existing settings changed: %+v", loaded)
	}
	if _, err := Save(loaded); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if _, exists := saved["retired_feature"]; exists {
		t.Fatal("unknown field was written back")
	}
	if saved["welcome_complete"] != true || saved["language"] != "en" {
		t.Fatalf("saved settings changed: %v", saved)
	}
}
