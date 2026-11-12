package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGitBranchModeDefaultsToMainDevelop(t *testing.T) {
	cfg, err := Validate(minimalPayload(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Git.BranchMode != GitBranchModeMainDevelop || MainlineBranch(cfg) != "develop" {
		t.Fatalf("missing section: %q", cfg.Git.BranchMode)
	}
	cfg, err = Validate(minimalPayload(map[string]any{"git": map[string]any{}}))
	if err != nil || cfg.Git.BranchMode != GitBranchModeMainDevelop {
		t.Fatalf("missing key: %q err=%v", cfg.Git.BranchMode, err)
	}
}

func TestGitBranchModeValidation(t *testing.T) {
	cfg, err := Validate(minimalPayload(map[string]any{"git": map[string]any{"branch_mode": "main-only"}}))
	if err != nil || cfg.Git.BranchMode != GitBranchModeMainOnly || MainlineBranch(cfg) != "main" {
		t.Fatalf("main-only: %q err=%v", cfg.Git.BranchMode, err)
	}
	for _, value := range []any{"main", "develop", "", true, 1} {
		_, err := Validate(minimalPayload(map[string]any{"git": map[string]any{"branch_mode": value}}))
		if err == nil {
			t.Fatalf("accepted %v", value)
		}
	}
	if _, err := Validate(minimalPayload(map[string]any{"git": "main-only"})); err == nil {
		t.Fatal("accepted a non-object git section")
	}
}

func TestRepairWritesGitBranchMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvConfig, path)
	if err := os.WriteFile(path, []byte(`{"language":"en"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Repair(nil); err != nil {
		t.Fatal(err)
	}
	saved, _, err := LoadScopeRaw(false)
	if err != nil || saved.Git.BranchMode != GitBranchModeMainDevelop {
		t.Fatalf("saved value = %q err=%v", saved.Git.BranchMode, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := decodeJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	git, _ := asObject(raw.(map[string]any)["git"])
	if git["branch_mode"] != GitBranchModeMainDevelop {
		t.Fatalf("repair did not write git.branch_mode: %s", data)
	}
}

func TestOverlayOverridesGitBranchMode(t *testing.T) {
	scope := DefaultConfig()
	scope.WelcomeComplete = true
	merged, err := ApplyOverlay(scope, map[string]any{"git": map[string]any{"branch_mode": "main-only"}})
	if err != nil {
		t.Fatal(err)
	}
	if merged.Git.BranchMode != GitBranchModeMainOnly || scope.Git.BranchMode != GitBranchModeMainDevelop {
		t.Fatalf("merged=%q scope=%q", merged.Git.BranchMode, scope.Git.BranchMode)
	}
}

func TestLoadForReadsOverlayOfGivenDirectory(t *testing.T) {
	t.Setenv(EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	scope := DefaultConfig()
	scope.WelcomeComplete = true
	if _, err := Save(scope); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, OverlayFilename), []byte(`{"git":{"branch_mode":"main-only"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFor(project)
	if err != nil {
		t.Fatal(err)
	}
	if MainlineBranch(cfg) != "main" {
		t.Fatalf("overlay of %s ignored: %q", project, cfg.Git.BranchMode)
	}
}
