package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOptionsTargetsByInstallMode(t *testing.T) {
	got := OptionsTargets(ModeGlobal)
	if len(got) != 2 || got[0] != TargetScope || got[1] != TargetOverlay {
		t.Fatalf("global targets: %#v", got)
	}
	got = OptionsTargets(ModeProject)
	if len(got) != 1 || got[0] != TargetOverlay {
		t.Fatalf("project targets: %#v", got)
	}
}

func TestOverlayHasTreatsFalseEmptyAndEqualAsOverride(t *testing.T) {
	overlay := map[string]any{
		"tui": map[string]any{
			"single": false,
			"theme":  "",
		},
		"rules":  map[string]any{"code": false},
		"agents": map[string]any{"helper": map[string]any{"args": []any{}}},
	}
	if !OverlayHas(overlay, "tui", "single") {
		t.Fatal("false must be an explicit override")
	}
	if !OverlayHas(overlay, "tui", "theme") {
		t.Fatal("empty string must be an explicit override")
	}
	if !OverlayHas(overlay, "rules", "code") {
		t.Fatal("false rule must be an explicit override")
	}
	if !OverlayHas(overlay, "agents", "helper", "args") {
		t.Fatal("empty array must be an explicit override")
	}
	if OverlayHas(overlay, "language") {
		t.Fatal("missing key is inherit")
	}
	if OverlayHas(overlay, "tui", "columns") {
		t.Fatal("missing nested key is inherit")
	}
}

func TestOverlaySetAndDeletePruneEmptyParents(t *testing.T) {
	overlay := map[string]any{"language": "ja"}
	OverlaySet(overlay, "dark", "tui", "theme")
	OverlaySet(overlay, 3, "tui", "columns")
	if !OverlayHas(overlay, "tui", "theme") || !OverlayHas(overlay, "tui", "columns") {
		t.Fatalf("set: %#v", overlay)
	}
	OverlayDelete(overlay, "tui", "theme")
	if OverlayHas(overlay, "tui", "theme") || !OverlayHas(overlay, "tui", "columns") {
		t.Fatalf("partial delete: %#v", overlay)
	}
	if OverlayHas(overlay, "language") == false {
		t.Fatal("unrelated key was removed")
	}
	OverlayDelete(overlay, "tui", "columns")
	if OverlayHas(overlay, "tui") {
		t.Fatalf("empty tui parent should be pruned: %#v", overlay)
	}
	if !OverlayHas(overlay, "language") {
		t.Fatal("language should remain")
	}
}

func TestResolveOverlayLocationGitWorktreeAndNonGit(t *testing.T) {
	setupHome(t)
	root := t.TempDir()
	main := initGitRepo(t, filepath.Join(root, "repo"))
	subdir := filepath.Join(main, "internal", "pkg")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(root, "task-wt")
	runGit(t, main, "worktree", "add", "-q", worktree)
	want := filepath.Join(main, OverlayFilename)

	for _, dir := range []string{main, subdir, worktree} {
		loc, err := ResolveOverlayLocation(dir)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		assertSameRealPath(t, loc.ProjectRoot, main)
		assertSameRealPath(t, loc.Path, want)
		if loc.Exists {
			t.Fatalf("%s: missing overlay should not exist yet", dir)
		}
	}

	writeJSONFile(t, want, map[string]any{"kanban_agent": "claude"})
	loc, err := ResolveOverlayLocation(worktree)
	if err != nil {
		t.Fatal(err)
	}
	if !loc.Exists {
		t.Fatal("existing overlay should be reported")
	}

	plain := filepath.Join(root, "plain", "nested")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	loc, err = ResolveOverlayLocation(plain)
	if err != nil {
		t.Fatal(err)
	}
	assertSameRealPath(t, loc.ProjectRoot, plain)
	assertSameRealPath(t, loc.Path, filepath.Join(plain, OverlayFilename))
	if loc.Exists {
		t.Fatal("non-git create target must not claim the file exists")
	}

	existing := filepath.Join(root, "plain", OverlayFilename)
	writeJSONFile(t, existing, map[string]any{"language": "ja"})
	loc, err = ResolveOverlayLocation(plain)
	if err != nil {
		t.Fatal(err)
	}
	assertSameRealPath(t, loc.Path, existing)
	if !loc.Exists {
		t.Fatal("walk-up should reuse the existing overlay")
	}
}

func TestSaveOverlayCreatesSparseFileAndDeletesWhenEmpty(t *testing.T) {
	setupHome(t)
	root := t.TempDir()
	main := initGitRepo(t, filepath.Join(root, "repo"))
	t.Chdir(main)
	scope := filepath.Join(root, "config.json")
	writeScopeFile(t, scope)
	path := filepath.Join(main, OverlayFilename)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("overlay should not exist before first override: %v", err)
	}
	saved, err := SaveOverlayIfUnchanged(path, map[string]any{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if saved != path {
		t.Fatalf("empty save path=%s", saved)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("empty overlay must not create a file")
	}

	overlay := map[string]any{"language": "ja", "tui": map[string]any{"theme": "dark"}}
	if _, err := SaveOverlayIfUnchanged(path, overlay, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatal("overlay save left a lock sidecar in the project root")
	}
	raw, err := ReadOverlayFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if raw["language"] != "ja" {
		t.Fatalf("saved language=%v", raw["language"])
	}
	if _, ok := raw["kanban_agent"]; ok {
		t.Fatal("save copied an inherited key")
	}
	if _, ok := raw["schema_version"]; ok || raw["welcome_complete"] != nil {
		t.Fatal("save wrote forbidden keys")
	}
	scopeCfg, err := LoadScope(true)
	if err != nil {
		t.Fatal(err)
	}
	if scopeCfg.Language == "ja" || scopeCfg.TUI.Theme == "dark" {
		t.Fatalf("overlay leaked into scope: %+v", scopeCfg)
	}
	merged, err := Load(true)
	if err != nil {
		t.Fatal(err)
	}
	if merged.Language != "ja" || merged.TUI.Theme != "dark" {
		t.Fatalf("merged %#v", merged)
	}

	if _, err := SaveOverlayIfUnchanged(path, map[string]any{}, overlay); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("restoring every key should delete the overlay file")
	}
}

func TestSaveOverlayRejectsConflictAndInvalidMerge(t *testing.T) {
	setupHome(t)
	root := t.TempDir()
	main := initGitRepo(t, filepath.Join(root, "repo"))
	t.Chdir(main)
	writeScopeFile(t, filepath.Join(root, "config.json"))
	path := filepath.Join(main, OverlayFilename)

	_, err := SaveOverlayIfUnchanged(path, map[string]any{"welcome_complete": true}, map[string]any{})
	if err == nil {
		t.Fatal("forbidden key must fail before write")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("failed save created a file")
	}

	first := map[string]any{"language": "ja"}
	if _, err := SaveOverlayIfUnchanged(path, first, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	_, err = SaveOverlayIfUnchanged(path, map[string]any{"language": "en"}, map[string]any{})
	if err == nil {
		t.Fatal("stale baseline should conflict")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk["language"] != "ja" {
		t.Fatalf("conflict overwrote disk: %s", data)
	}
}

func TestSaveOverlayIgnoresKANDERConfigPath(t *testing.T) {
	setupHome(t)
	root := t.TempDir()
	main := initGitRepo(t, filepath.Join(root, "repo"))
	t.Chdir(main)
	scope := filepath.Join(root, "alt-config.json")
	writeScopeFile(t, scope)
	overlay := filepath.Join(main, OverlayFilename)
	if _, err := SaveOverlayIfUnchanged(overlay, map[string]any{"launcher": "foreground"}, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(scope); err != nil {
		t.Fatal(err)
	}
	scopeCfg, err := LoadScope(true)
	if err != nil {
		t.Fatal(err)
	}
	if scopeCfg.Launcher == "foreground" {
		t.Fatal("overlay save rewrote KANDER_CONFIG")
	}
	raw, err := ReadOverlayFile(overlay)
	if err != nil {
		t.Fatal(err)
	}
	if raw["launcher"] != "foreground" {
		t.Fatalf("overlay launcher=%v", raw["launcher"])
	}
}

func TestMergeScopeAndOverlayKeepsExplicitEqualOverride(t *testing.T) {
	scope := DefaultConfig()
	scope.WelcomeComplete = true
	scope.Language = "en"
	overlay := map[string]any{"language": "en"}
	merged, err := MergeScopeAndOverlay(scope, overlay)
	if err != nil {
		t.Fatal(err)
	}
	if merged.Language != "en" {
		t.Fatal(merged.Language)
	}
	if !OverlayHas(overlay, "language") {
		t.Fatal("explicit equal value must stay an override")
	}
}

func TestSaveOverlayRejectsPartialTUIOnLegacyScope(t *testing.T) {
	setupHome(t)
	root := t.TempDir()
	main := initGitRepo(t, filepath.Join(root, "repo"))
	t.Chdir(main)
	scope := filepath.Join(root, "config.json")
	cfg := DefaultConfig()
	cfg.WelcomeComplete = true
	raw, err := DocumentFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	delete(raw, "tui")
	writeJSONFile(t, scope, raw)
	t.Setenv(EnvConfig, scope)

	path := filepath.Join(main, OverlayFilename)
	_, err = SaveOverlayIfUnchanged(path, map[string]any{"tui": map[string]any{"theme": "dark"}}, map[string]any{})
	if err == nil {
		t.Fatal("partial tui overlay must fail against a scope that omits tui")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("failed save created a file")
	}
	if _, err := LoadScope(true); err != nil {
		t.Fatalf("legacy scope without tui should still load: %v", err)
	}
}

func TestMergeOverlayOnRawMatchesLoadForMissingRules(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WelcomeComplete = true
	raw, err := DocumentFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	delete(raw, "rules")
	overlay := map[string]any{"rules": map[string]any{"code": false}}
	merged, err := MergeOverlayOnRaw(raw, overlay)
	if err != nil {
		t.Fatal(err)
	}
	if merged.Rules["code"] {
		t.Fatal("explicit code=false should win")
	}
	if merged.Rules[RuleGit] {
		t.Fatal("raw merge must not keep filled default rules")
	}
	filled, err := MergeScopeAndOverlay(cfg, overlay)
	if err != nil {
		t.Fatal(err)
	}
	if !filled.Rules[RuleGit] {
		t.Fatal("filled-config merge is the rejected preview path")
	}
}
