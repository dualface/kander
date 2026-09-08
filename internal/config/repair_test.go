package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRepairPreservesValidSettingsAndBacksUpOriginal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvConfig, path)
	original := []byte(`{
		"schema_version": 999, "welcome_complete": true, "kanban_agent": "claude",
		"launcher": "foreground", "language": "en", "retired_feature": true,
		"reviewers": {"PM":"claude","QA":"bad"},
		"tui": {"columns":999,"theme":"dark","refresh":15},
		"models": {"kanban":{"claude":{"large_model":"my-model","small_effort":42}},
		"review":{"codex":{"model":"custom-review","effort":"high"}}}
	}`)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, result, err := Repair(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Created || result.BackupPath == "" {
		t.Fatalf("result=%+v", result)
	}
	if cfg.Language != "en" || cfg.KanbanAgent != "claude" || cfg.KanbanAgents["small"] != "claude" || cfg.Reviewers["PM"] != "claude" || cfg.Reviewers["QA"] != "codex" {
		t.Fatalf("valid selections lost: %+v", cfg)
	}
	if cfg.TUI.Theme != "dark" || cfg.TUI.Refresh != 15 || cfg.TUI.Columns != DefaultTUIColumns || cfg.Models.Kanban["claude"]["large_model"] != "my-model" || cfg.Models.Review["codex"]["model"] != "custom-review" {
		t.Fatalf("valid nested settings lost: %+v", cfg)
	}
	backup, err := os.ReadFile(result.BackupPath)
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatalf("original backup mismatch: %v", err)
	}
	if _, err := Load(false); err != nil {
		t.Fatal(err)
	}
	_, second, err := Repair(nil)
	if err != nil || second.Changed || second.BackupPath != "" {
		t.Fatalf("second repair must not rewrite: %+v, %v", second, err)
	}
}

func TestRepairDoesNotOverwriteUnreadableConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvConfig, path)
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Repair(nil); err == nil {
		t.Fatal("non-file path must fail without replacement")
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		t.Fatalf("original directory changed: %v", err)
	}
}

func TestRepairHonorsCLILanguage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvConfig, path)
	t.Setenv(EnvLang, "cn")
	t.Setenv(EnvLangCLI, "1")
	ApplyLanguageArgument([]string{"kander", "--lang", "cn"})
	cfg, result, err := Repair(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created || cfg.Language != "cn" {
		t.Fatalf("language=%q created=%v", cfg.Language, result.Created)
	}
	ApplyLanguageArgument(nil)
	t.Setenv(EnvLangCLI, "")
	t.Setenv(EnvLang, "")
	path2 := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvConfig, path2)
	cfg, result, err = Repair(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created || cfg.Language != "en" {
		t.Fatalf("default language=%q created=%v", cfg.Language, result.Created)
	}
}

func TestSetLanguageIfPresent(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.json")
	if err := SetLanguageIfPresent(missing, "cn"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("created missing config: %v", err)
	}
	path := filepath.Join(dir, "config.json")
	t.Setenv(EnvConfig, path)
	cfg := DefaultConfig()
	cfg.Language = "en"
	cfg.WelcomeComplete = true
	cfg.KanbanAgent = "claude"
	if _, err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	if err := SetLanguageIfPresent(path, "cn"); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Language != "cn" || loaded.KanbanAgent != "claude" || !loaded.WelcomeComplete {
		t.Fatalf("%+v", loaded)
	}
	if err := SetLanguageIfPresent(path, "nope"); err == nil {
		t.Fatal("invalid language")
	}
}

func TestRepairFillsMissingReviewStageScaleFromTheOther(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvConfig, path)
	original := []byte(`{
		"schema_version": 1, "welcome_complete": true, "kanban_agent": "codex",
		"launcher": "foreground",
		"reviewers": {"PM":"codex","CSA":"codex","Hacker":"codex","QA":"codex"},
		"review_stages": {"large": {"PM":"required","CSA":"skip","Hacker":"skip","QA":"auto"}}
	}`)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, result, err := Repair(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("missing small scale must be rewritten")
	}
	if cfg.ReviewStages["large"]["PM"] != "required" || cfg.ReviewStages["small"]["PM"] != "required" {
		t.Fatalf("missing scale not copied: %+v", cfg.ReviewStages)
	}
	if cfg.ReviewStages["small"]["CSA"] != "skip" {
		t.Fatalf("copied scale incomplete: %+v", cfg.ReviewStages["small"])
	}

	emptyPath := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvConfig, emptyPath)
	if err := os.WriteFile(emptyPath, []byte(`{
		"schema_version": 1, "welcome_complete": true, "kanban_agent": "codex",
		"launcher": "foreground",
		"reviewers": {"PM":"codex","CSA":"codex","Hacker":"codex","QA":"codex"}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err = Repair(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, scale := range TaskScales {
		for _, role := range ReviewRoles {
			if cfg.ReviewStages[scale][role] != "auto" {
				t.Fatalf("both missing must default %s.%s=%s", scale, role, cfg.ReviewStages[scale][role])
			}
		}
	}
}

func TestRepairWritesWhenOnlyOneReviewStageScaleIsMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvConfig, path)
	complete := DefaultConfig()
	complete.WelcomeComplete = true
	complete.ReviewStages["large"]["PM"] = "required"
	complete.ReviewStages["small"]["PM"] = "required"
	if _, err := Save(complete); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := decodeJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	obj, ok := asObject(raw)
	if !ok {
		t.Fatal(raw)
	}
	stages, ok := asObject(obj["review_stages"])
	if !ok {
		t.Fatal(obj["review_stages"])
	}
	delete(stages, "small")
	trimmed, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(trimmed, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	repaired, result, err := Repair(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("a normalized config missing only small must be rewritten")
	}
	if repaired.ReviewStages["small"]["PM"] != "required" {
		t.Fatalf("in-memory small=%v", repaired.ReviewStages["small"])
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := ValidateJSON(onDisk)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ReviewStages["small"]["PM"] != "required" {
		t.Fatalf("disk small=%v", loaded.ReviewStages["small"])
	}
}
