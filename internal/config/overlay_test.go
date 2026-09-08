package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeJSONFile(t *testing.T, path string, payload any) []byte {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return data
}

func writeScopeFile(t *testing.T, path string) *Config {
	t.Helper()
	t.Setenv(EnvConfig, path)
	cfg := DefaultConfig()
	cfg.WelcomeComplete = true
	if _, err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestDeepMergeObjectsScalarsAndArrays(t *testing.T) {
	base := map[string]any{
		"kanban_agent": "codex",
		"review_stages": map[string]any{
			"large": map[string]any{"PM": "auto", "QA": "auto"},
			"small": map[string]any{"PM": "auto", "QA": "skip"},
		},
		"agents": map[string]any{
			"helper": map[string]any{"args": []any{"old", "keep"}},
		},
	}
	overlay := map[string]any{
		"kanban_agent": "claude",
		"review_stages": map[string]any{
			"large": map[string]any{"PM": "required"},
		},
		"agents": map[string]any{
			"helper": map[string]any{"args": []any{"new"}},
		},
	}
	merged := deepMerge(base, overlay)
	if merged["kanban_agent"] != "claude" {
		t.Fatalf("scalar replace: %v", merged["kanban_agent"])
	}
	stages := merged["review_stages"].(map[string]any)
	large := stages["large"].(map[string]any)
	small := stages["small"].(map[string]any)
	if large["PM"] != "required" || large["QA"] != "auto" {
		t.Fatalf("nested object merge: %#v", large)
	}
	if small["PM"] != "auto" || small["QA"] != "skip" {
		t.Fatalf("untouched nested object: %#v", small)
	}
	args := merged["agents"].(map[string]any)["helper"].(map[string]any)["args"].([]any)
	if len(args) != 1 || args[0] != "new" {
		t.Fatalf("array replace: %#v", args)
	}
	if base["kanban_agent"] != "codex" {
		t.Fatal("deepMerge must not mutate the base map")
	}
	full := deepMerge(minimalPayload(nil), map[string]any{
		"kanban_agent": "claude",
		"review_stages": map[string]any{
			"large": map[string]any{"PM": "required"},
		},
	})
	if _, err := Validate(full); err != nil {
		t.Fatalf("merged payload must validate: %v", err)
	}
}

func TestOverlayPathGitAndNonGitLookup(t *testing.T) {
	setupHome(t)
	root := t.TempDir()
	main := initGitRepo(t, filepath.Join(root, "repo"))
	overlay := filepath.Join(main, OverlayFilename)
	writeJSONFile(t, overlay, map[string]any{"kanban_agent": "claude"})
	scope := filepath.Join(root, "config.json")
	writeScopeFile(t, scope)

	subdir := filepath.Join(main, "internal", "pkg")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(root, "task-wt")
	runGit(t, main, "worktree", "add", "-q", worktree)

	for _, dir := range []string{main, subdir, worktree} {
		t.Chdir(dir)
		got, err := OverlayPath("")
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		assertSameRealPath(t, got, overlay)
		cfg, err := Load(true)
		if err != nil {
			t.Fatalf("%s load: %v", dir, err)
		}
		if cfg.KanbanAgent != "claude" {
			t.Fatalf("%s merged agent=%s", dir, cfg.KanbanAgent)
		}
		scopeCfg, err := LoadScope(true)
		if err != nil {
			t.Fatal(err)
		}
		if scopeCfg.KanbanAgent != "codex" {
			t.Fatalf("scope agent leaked overlay: %s", scopeCfg.KanbanAgent)
		}
	}

	if err := os.Remove(overlay); err != nil {
		t.Fatal(err)
	}
	t.Chdir(main)
	cfg, err := Load(true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KanbanAgent != "codex" {
		t.Fatalf("after delete want scope agent, got %s", cfg.KanbanAgent)
	}

	nongit := filepath.Join(root, "plain", "nested")
	if err := os.MkdirAll(nongit, 0o755); err != nil {
		t.Fatal(err)
	}
	plainOverlay := filepath.Join(root, "plain", OverlayFilename)
	writeJSONFile(t, plainOverlay, map[string]any{"kanban_agent": "grok"})
	t.Chdir(nongit)
	got, err := OverlayPath("")
	if err != nil {
		t.Fatal(err)
	}
	assertSameRealPath(t, got, plainOverlay)
	cfg, err = Load(true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KanbanAgent != "grok" {
		t.Fatalf("non-git walk agent=%s", cfg.KanbanAgent)
	}
}

func TestLoadMergesReviewStagesAfterNormalizingFlatScope(t *testing.T) {
	setupHome(t)
	root := t.TempDir()
	main := initGitRepo(t, filepath.Join(root, "repo"))
	t.Chdir(main)
	scope := filepath.Join(root, "config.json")
	t.Setenv(EnvConfig, scope)
	payload := minimalPayload(map[string]any{
		"review_stages": map[string]any{"PM": "auto", "QA": "skip", "CSA": "auto", "Hacker": "auto"},
	})
	writeJSONFile(t, scope, payload)
	writeJSONFile(t, filepath.Join(main, OverlayFilename), map[string]any{
		"review_stages": map[string]any{"large": map[string]any{"PM": "required"}},
	})
	cfg, err := Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReviewStages["large"]["PM"] != "required" {
		t.Fatalf("overlay large.PM=%s", cfg.ReviewStages["large"]["PM"])
	}
	if cfg.ReviewStages["large"]["QA"] != "skip" || cfg.ReviewStages["small"]["QA"] != "skip" {
		t.Fatalf("flat scope QA should apply to both scales: %+v", cfg.ReviewStages)
	}
	if cfg.ReviewStages["small"]["PM"] != "auto" {
		t.Fatalf("small.PM should stay the normalized scope value: %s", cfg.ReviewStages["small"]["PM"])
	}
}

func TestOverlayRejectsForbiddenUnknownInvalidAndUnsafeFiles(t *testing.T) {
	setupHome(t)
	root := t.TempDir()
	main := initGitRepo(t, filepath.Join(root, "repo"))
	t.Chdir(main)
	writeScopeFile(t, filepath.Join(root, "config.json"))
	overlay := filepath.Join(main, OverlayFilename)

	writeJSONFile(t, overlay, map[string]any{"schema_version": 1, "kanban_agent": "claude"})
	_, err := Load(true)
	if err == nil || !strings.Contains(err.Error(), overlay) || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("forbidden key: %v", err)
	}

	writeJSONFile(t, overlay, map[string]any{"welcome_complete": true})
	_, err = Load(true)
	if err == nil || !strings.Contains(err.Error(), overlay) || !strings.Contains(err.Error(), "welcome_complete") {
		t.Fatalf("forbidden welcome: %v", err)
	}

	writeJSONFile(t, overlay, map[string]any{"retired_feature": true})
	_, err = Load(true)
	if err == nil || !strings.Contains(err.Error(), overlay) || !strings.Contains(err.Error(), "retired_feature") {
		t.Fatalf("unknown key: %v", err)
	}

	if err := os.WriteFile(overlay, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Load(true)
	if err == nil || !strings.Contains(err.Error(), overlay) {
		t.Fatalf("invalid json: %v", err)
	}

	if err := os.Remove(overlay); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(overlay, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = OverlayPath("")
	if err == nil || !strings.Contains(err.Error(), overlay) {
		t.Fatalf("directory overlay: %v", err)
	}
	if err := os.Remove(overlay); err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS == "windows" {
		return
	}
	target := filepath.Join(main, "target.json")
	writeJSONFile(t, target, map[string]any{"kanban_agent": "claude"})
	if err := os.Symlink(target, overlay); err != nil {
		t.Fatal(err)
	}
	_, err = OverlayPath("")
	if err == nil || !strings.Contains(err.Error(), overlay) {
		t.Fatalf("symlink overlay: %v", err)
	}
}

func TestWritesDoNotCopyOverlayValuesIntoScope(t *testing.T) {
	setupHome(t)
	root := t.TempDir()
	main := initGitRepo(t, filepath.Join(root, "repo"))
	t.Chdir(main)
	scope := filepath.Join(root, "config.json")
	writeScopeFile(t, scope)
	overlay := filepath.Join(main, OverlayFilename)
	originalOverlay := writeJSONFile(t, overlay, map[string]any{"kanban_agent": "claude"})

	assertIsolated := func(t *testing.T) {
		t.Helper()
		data, err := os.ReadFile(overlay)
		if err != nil || !bytes.Equal(data, originalOverlay) {
			t.Fatalf("overlay bytes changed: %s", data)
		}
		scopeCfg, err := LoadScope(true)
		if err != nil {
			t.Fatal(err)
		}
		if scopeCfg.KanbanAgent == "claude" {
			t.Fatal("overlay-only kanban_agent written back into the scope file")
		}
		merged, err := Load(true)
		if err != nil {
			t.Fatal(err)
		}
		if merged.KanbanAgent != "claude" {
			t.Fatalf("runtime merge lost overlay: %s", merged.KanbanAgent)
		}
	}

	scopeCfg, err := LoadScope(true)
	if err != nil {
		t.Fatal(err)
	}
	scopeCfg.Language = "ja"
	if _, err := Save(scopeCfg); err != nil {
		t.Fatal(err)
	}
	assertIsolated(t)

	if _, err := Update(func(cfg *Config) error {
		cfg.Launcher = "foreground"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	assertIsolated(t)

	baseline, err := LoadScope(true)
	if err != nil {
		t.Fatal(err)
	}
	edited := Clone(baseline)
	edited.TUI.Theme = "dark"
	if _, err := SaveIfUnchanged(edited, baseline); err != nil {
		t.Fatal(err)
	}
	assertIsolated(t)
}

func TestRepairLeavesOverlayBytesAndValuesOutOfScope(t *testing.T) {
	setupHome(t)
	root := t.TempDir()
	main := initGitRepo(t, filepath.Join(root, "repo"))
	t.Chdir(main)
	scope := filepath.Join(root, "config.json")
	t.Setenv(EnvConfig, scope)
	original := []byte(`{
		"schema_version": 999, "welcome_complete": true, "kanban_agent": "codex",
		"launcher": "foreground", "language": "en",
		"reviewers": {"PM":"codex","QA":"bad","CSA":"codex","Hacker":"codex"}
	}`)
	if err := os.WriteFile(scope, original, 0o644); err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(main, OverlayFilename)
	originalOverlay := writeJSONFile(t, overlay, map[string]any{"kanban_agent": "claude", "language": "ja"})

	cfg, result, err := Repair(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("expected a repaired scope file")
	}
	if cfg.KanbanAgent != "codex" || cfg.Language != "en" {
		t.Fatalf("repair used overlay values: agent=%s language=%s", cfg.KanbanAgent, cfg.Language)
	}
	data, err := os.ReadFile(overlay)
	if err != nil || !bytes.Equal(data, originalOverlay) {
		t.Fatalf("repair changed overlay bytes: %s", data)
	}
	scopeCfg, err := LoadScope(false)
	if err != nil {
		t.Fatal(err)
	}
	if scopeCfg.KanbanAgent == "claude" || scopeCfg.Language == "ja" {
		t.Fatalf("repaired scope contains overlay-only values: %+v", scopeCfg)
	}
}

func TestFormatConfigLinesIncludesOverlayPath(t *testing.T) {
	setupHome(t)
	root := t.TempDir()
	main := initGitRepo(t, filepath.Join(root, "repo"))
	t.Chdir(main)
	writeScopeFile(t, filepath.Join(root, "config.json"))
	overlay := filepath.Join(main, OverlayFilename)
	writeJSONFile(t, overlay, map[string]any{"kanban_agent": "claude"})

	cfg, err := Load(true)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := FormatConfigLines(cfg)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, Text("config.overlay_file")+": ") || !strings.Contains(joined, overlay) {
		t.Fatalf("missing overlay path:\n%s", joined)
	}
}
