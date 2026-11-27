package config

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func setupHome(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(EnvLang, "cn")
	t.Setenv(EnvLangCLI, "")
	_ = os.Unsetenv(EnvConfig)
	_ = os.Unsetenv("GIT_DIR")
	_ = os.Unsetenv("GIT_WORK_TREE")
	_ = os.Unsetenv("GIT_COMMON_DIR")
	resetLanguageState()
	isolateInheritedOverlay(t)
	return root
}

// isolateInheritedOverlay moves the test into a temporary working directory
// unless that directory is itself the overlay's project root (a fixture the
// test created). During go test the package directory resolves its overlay to
// the main worktree's .kander-config.json, which Load merges but
// SaveIfUnchanged does not, so a developer's own overlay would surface as a
// concurrent edit. It runs after the Git variables are cleared so resolution
// sees the same repository the test later uses. t.Chdir restores the package
// directory after the test.
func isolateInheritedOverlay(t *testing.T) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	location, err := ResolveOverlayLocation(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if location.ProjectRoot != cwd {
		t.Chdir(t.TempDir())
	}
}

func resetLanguageState() {
	langMu.Lock()
	cliLanguageOverride = ""
	configLanguage = ""
	langMu.Unlock()
	_ = os.Unsetenv(EnvLangCLI)
}

func initGitRepo(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, path, "init", "-q", path)
	runGit(t, path, "-C", path, "config", "user.email", "kander@example.com")
	runGit(t, path, "-C", path, "config", "user.name", "Kander Test")
	runGit(t, path, "-C", path, "commit", "--allow-empty", "-q", "-m", "init")
	return path
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func sameRealPath(t *testing.T, left, right string) bool {
	t.Helper()
	a, err := filepath.EvalSymlinks(left)
	if err != nil {
		a = left
	}
	b, err := filepath.EvalSymlinks(right)
	if err != nil {
		b = right
	}
	a, _ = filepath.Abs(a)
	b, _ = filepath.Abs(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func assertSameRealPath(t *testing.T, left, right string) {
	t.Helper()
	if !sameRealPath(t, left, right) {
		t.Fatalf("%s != %s", left, right)
	}
}

func minimalPayload(overrides map[string]any) map[string]any {
	payload := map[string]any{
		"schema_version":   1,
		"welcome_complete": true,
		"kanban_agent":     "cursor",
		"launcher":         "tmux",
		"reviewers":        map[string]any{},
		"models":           map[string]any{},
	}
	reviewers := map[string]any{}
	for _, role := range ReviewRoles {
		reviewers[role] = "cursor"
	}
	payload["reviewers"] = reviewers
	for k, v := range overrides {
		payload[k] = v
	}
	return payload
}

func TestInstallPathsAreGlobalWhateverTheEntry(t *testing.T) {
	root := setupHome(t)
	home := filepath.Join(root, "home")
	paths, err := CurrentInstallPaths()
	if err != nil {
		t.Fatal(err)
	}
	want := InstallPaths{
		ConfigPath: filepath.Join(home, ".config", "kander", "config.json"),
		RulesDir:   filepath.Join(home, ".agents", "kander"),
		BinDir:     filepath.Join(home, ".local", "bin"),
		ShareDir:   filepath.Join(home, ".local", "share", "kander"),
	}
	if paths != want {
		t.Fatalf("paths=%+v want %+v", paths, want)
	}
}

func TestLegacyProjectEntryIsOnlyNamed(t *testing.T) {
	root := setupHome(t)
	project := initGitRepo(t, filepath.Join(root, "app"))
	entry := filepath.Join(project, ".kander", "bin", "kander")
	got, ok := LegacyProjectInstallRootOf(entry)
	if !ok || got != filepath.Join(project, ".kander") {
		t.Fatalf("legacy root=%q ok=%v", got, ok)
	}
	for _, entry := range []string{
		filepath.Join(root, "src", "cmd", "kander", "kander"),
		filepath.Join(root, "app", ".kander", "kander"),
		filepath.Join(root, "app", "kander", "bin", "kander"),
	} {
		if got, ok := LegacyProjectInstallRootOf(entry); ok {
			t.Fatalf("%s named a legacy root %q", entry, got)
		}
	}
}

func TestPosixUppercaseKanderDirIsNotLegacyProject(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows treats .KANDER as the project directory")
	}
	root := setupHome(t)
	if got, ok := LegacyProjectInstallRootOf(filepath.Join(root, "cased", ".KANDER", "bin", "kander")); ok {
		t.Fatalf("legacy root=%q", got)
	}
}

func TestMainWorktreeIgnoresGitDirEnv(t *testing.T) {
	root := setupHome(t)
	main := initGitRepo(t, filepath.Join(root, "app"))
	other := initGitRepo(t, filepath.Join(root, "other"))
	linked := filepath.Join(root, "app-linked")
	runGit(t, main, "-C", main, "worktree", "add", "-q", linked, "HEAD")
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	got, err := MainWorktree(linked)
	if err != nil {
		t.Fatal(err)
	}
	assertSameRealPath(t, main, got)
	exclude, err := GitInfoExcludePath(main)
	if err != nil {
		t.Fatal(err)
	}
	assertSameRealPath(t, filepath.Join(main, ".git", "info", "exclude"), exclude)
	plain := filepath.Join(root, "plain")
	if err := os.Mkdir(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", "")
	if got, err := MainWorktree(plain); err != nil || got != "" {
		t.Fatalf("non-Git main=%q err=%v", got, err)
	}
}

func TestConfigPathKeepsEnvOverride(t *testing.T) {
	root := setupHome(t)
	override := filepath.Join(root, "override-config.json")
	t.Setenv(EnvConfig, override)
	got, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != override {
		t.Fatalf("got %s", got)
	}
}

func TestConfigPathWithoutOverrideFollowsGlobalScope(t *testing.T) {
	root := setupHome(t)
	home := filepath.Join(root, "home")
	got, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".config", "kander", "config.json")
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestValidateTUI(t *testing.T) {
	if got := DefaultTUI().Columns; got != 5 {
		t.Fatalf("default columns=%d", got)
	}
	valid := minimalPayload(map[string]any{
		"tui": map[string]any{
			"columns": 4, "min_column_width": 36, "refresh": 10,
			"compact": true, "single": true, "theme": "dark",
		},
	})
	payload, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := ValidateJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TUI.Columns != 4 || cfg.TUI.Theme != "dark" || !cfg.TUI.Single || !cfg.TUI.Compact {
		t.Fatalf("tui=%+v", cfg.TUI)
	}
	legacy := minimalPayload(map[string]any{"tui": map[string]any{
		"columns": 4, "min_column_width": 36, "refresh": 10,
		"single": false, "theme": "dark",
	}})
	legacyPayload, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	legacyConfig, err := ValidateJSON(legacyPayload)
	if err != nil || legacyConfig.TUI.Compact {
		t.Fatalf("legacy compact=%v err=%v", legacyConfig.TUI.Compact, err)
	}

	for name, tui := range map[string]any{
		"compact type":  map[string]any{"compact": "yes", "columns": 3, "min_column_width": 40, "refresh": 30, "single": false, "theme": "auto"},
		"columns range": map[string]any{"columns": 8, "min_column_width": 40, "refresh": 30, "single": false, "theme": "auto"},
		"refresh type":  map[string]any{"columns": 3, "min_column_width": 40, "refresh": "fast", "single": false, "theme": "auto"},
		"unknown theme": map[string]any{"columns": 3, "min_column_width": 40, "refresh": 30, "single": false, "theme": "blue"},
	} {
		t.Run(name, func(t *testing.T) {
			raw := minimalPayload(map[string]any{"tui": tui})
			data, err := json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ValidateJSON(data); !IsError(err) {
				t.Fatalf("expected config error, got %v", err)
			}
		})
	}
}
