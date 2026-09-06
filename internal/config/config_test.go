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
	return root
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

func TestSourceTreeEntryKeepsGlobalHomePaths(t *testing.T) {
	root := setupHome(t)
	home := filepath.Join(root, "home")
	entry := filepath.Join(root, "src", "cmd", "kander", "kander")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("bin\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	paths, err := InstallPathsFromEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	if paths.Mode != ModeGlobal {
		t.Fatalf("mode=%s", paths.Mode)
	}
	if paths.ProjectRoot != "" || paths.InstallRoot != "" {
		t.Fatalf("project paths set: %+v", paths)
	}
	if paths.ConfigPath != filepath.Join(home, ".config", "kander", "config.json") {
		t.Fatalf("config=%s", paths.ConfigPath)
	}
	if paths.RulesDir != filepath.Join(home, ".agents") {
		t.Fatalf("rules=%s", paths.RulesDir)
	}
	if paths.BinDir != filepath.Join(home, ".local", "bin") {
		t.Fatalf("bin=%s", paths.BinDir)
	}
	if paths.ShareDir != filepath.Join(home, ".local", "share", "kander") {
		t.Fatalf("share=%s", paths.ShareDir)
	}
}

func TestPosixUppercaseKanderDirIsNotProject(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows treats .KANDER as the project directory")
	}
	root := setupHome(t)
	entry := filepath.Join(root, "cased", ".KANDER", "bin", "kander")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("# not a project layout on POSIX\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := InstallPathsFromEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	if paths.Mode != ModeGlobal {
		t.Fatalf("mode=%s", paths.Mode)
	}
}

func TestSourceLayoutWithCmdAndRulesIsNotProject(t *testing.T) {
	root := setupHome(t)
	home := filepath.Join(root, "home")
	source := filepath.Join(root, "kander-src")
	if err := os.MkdirAll(filepath.Join(source, "cmd", "kander"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(source, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(source, "cmd", "kander", "kander")
	if err := os.WriteFile(entry, []byte("# source tree entry\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	paths, err := InstallPathsFromEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	if paths.Mode != ModeGlobal {
		t.Fatalf("mode=%s", paths.Mode)
	}
	if paths.ConfigPath != filepath.Join(home, ".config", "kander", "config.json") {
		t.Fatalf("config=%s", paths.ConfigPath)
	}
}

func TestProjectEntryResolvesMainWorktreeLayout(t *testing.T) {
	root := setupHome(t)
	project := initGitRepo(t, filepath.Join(root, "app"))
	entry := filepath.Join(project, ".kander", "bin", "kander")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	paths, err := InstallPathsFromEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	if paths.Mode != ModeProject {
		t.Fatalf("mode=%s", paths.Mode)
	}
	assertSameRealPath(t, project, paths.ProjectRoot)
	if paths.InstallRoot != filepath.Join(paths.ProjectRoot, ".kander") {
		t.Fatalf("install=%s", paths.InstallRoot)
	}
	if paths.ConfigPath != filepath.Join(paths.InstallRoot, "config.json") {
		t.Fatal(paths.ConfigPath)
	}
	if paths.RulesDir != filepath.Join(paths.InstallRoot, "rules") {
		t.Fatal(paths.RulesDir)
	}
	if paths.BinDir != filepath.Join(paths.InstallRoot, "bin") {
		t.Fatal(paths.BinDir)
	}
	if paths.ShareDir != filepath.Join(paths.InstallRoot, "share") {
		t.Fatal(paths.ShareDir)
	}
}

func TestProjectInstallPathsFromLinkedWorktreeUsesMain(t *testing.T) {
	root := setupHome(t)
	main := initGitRepo(t, filepath.Join(root, "app"))
	linked := filepath.Join(root, "app-linked")
	runGit(t, main, "-C", main, "worktree", "add", "-q", linked, "HEAD")
	paths, err := ProjectInstallPaths(linked)
	if err != nil {
		t.Fatal(err)
	}
	if paths.Mode != ModeProject {
		t.Fatalf("mode=%s", paths.Mode)
	}
	assertSameRealPath(t, main, paths.ProjectRoot)
	if sameRealPath(t, linked, paths.ProjectRoot) {
		t.Fatal("linked worktree should not be the project root")
	}
	if paths.ConfigPath != filepath.Join(paths.ProjectRoot, ".kander", "config.json") {
		t.Fatal(paths.ConfigPath)
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
			"single": true, "theme": "dark",
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
	if cfg.TUI.Columns != 4 || cfg.TUI.Theme != "dark" || !cfg.TUI.Single {
		t.Fatalf("tui=%+v", cfg.TUI)
	}

	for name, tui := range map[string]any{
		"columns range": map[string]any{"columns": 8, "min_column_width": 40, "refresh": 30, "single": false, "theme": "auto"},
		"refresh type":  map[string]any{"columns": 3, "min_column_width": 40, "refresh": "fast", "single": false, "theme": "auto"},
		"unknown theme": map[string]any{"columns": 3, "min_column_width": 40, "refresh": 30, "single": false, "theme": "blue"},
		"unknown field": map[string]any{"columns": 3, "min_column_width": 40, "refresh": 30, "single": false, "theme": "auto", "extra": true},
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

func TestGitDirEnvDoesNotDivertProjectPaths(t *testing.T) {
	root := setupHome(t)
	project := initGitRepo(t, filepath.Join(root, "app"))
	other := initGitRepo(t, filepath.Join(root, "other"))
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	entry := filepath.Join(project, ".kander", "bin", "kander")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	paths, err := ProjectInstallPaths(project)
	if err != nil {
		t.Fatal(err)
	}
	runtimePaths, err := InstallPathsFromEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	returned, err := EnsureProjectGitExclude(project)
	if err != nil {
		t.Fatal(err)
	}
	if paths.Mode != ModeProject {
		t.Fatal(paths.Mode)
	}
	assertSameRealPath(t, project, paths.ProjectRoot)
	assertSameRealPath(t, project, runtimePaths.ProjectRoot)
	exclude := filepath.Join(project, ".git", "info", "exclude")
	assertSameRealPath(t, exclude, returned)
	text, err := os.ReadFile(exclude)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(strings.Split(string(text), "\n"), "/.kander/") {
		t.Fatalf("exclude missing pattern: %q", text)
	}
	otherText, err := os.ReadFile(filepath.Join(other, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	if contains(strings.Split(string(otherText), "\n"), "/.kander/") {
		t.Fatalf("other exclude mutated: %q", otherText)
	}
}

func TestInstallPathsRejectsProjectEntryWithoutGit(t *testing.T) {
	root := setupHome(t)
	entry := filepath.Join(root, "not-git", ".kander", "bin", "kander")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallPathsFromEntry(entry); !IsError(err) {
		t.Fatalf("expected config error, got %v", err)
	}
}

func TestProjectInstallPathsRejectsNonGit(t *testing.T) {
	root := setupHome(t)
	directory := filepath.Join(root, "not-git")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ProjectInstallPaths(directory); !IsError(err) {
		t.Fatalf("expected config error, got %v", err)
	}
}

func TestProjectInstallPathsRejectsMissingDirectory(t *testing.T) {
	root := setupHome(t)
	if _, err := ProjectInstallPaths(filepath.Join(root, "missing")); !IsError(err) {
		t.Fatalf("expected config error, got %v", err)
	}
}

func TestGitExcludeIsIdempotentAndPreservesMode(t *testing.T) {
	root := setupHome(t)
	project := initGitRepo(t, filepath.Join(root, "app"))
	exclude := filepath.Join(project, ".git", "info", "exclude")
	if err := os.WriteFile(exclude, []byte("# local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(exclude, 0o644); err != nil {
		t.Fatal(err)
	}
	var returned string
	for i := 0; i < 2; i++ {
		path, err := EnsureProjectGitExclude(project)
		if err != nil {
			t.Fatal(err)
		}
		returned = path
	}
	assertSameRealPath(t, exclude, returned)
	text, err := os.ReadFile(exclude)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(text), "\n"), "\n")
	count := 0
	for _, line := range lines {
		if line == "/.kander/" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("pattern count=%d lines=%v", count, lines)
	}
	if lines[0] != "# local" {
		t.Fatalf("first line=%q", lines[0])
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(exclude)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o644 {
			t.Fatalf("mode=%o", st.Mode().Perm())
		}
	}
}

func TestGitExcludeRejectsInfoSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink rejection")
	}
	root := setupHome(t)
	project := initGitRepo(t, filepath.Join(root, "app"))
	info := filepath.Join(project, ".git", "info")
	original := filepath.Join(project, ".git", "info-original")
	if err := os.Rename(info, original); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	outsideExclude := filepath.Join(outside, "exclude")
	if err := os.WriteFile(outsideExclude, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, info); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureProjectGitExclude(project); !IsError(err) {
		t.Fatalf("expected config error, got %v", err)
	}
	got, err := os.ReadFile(outsideExclude)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep\n" {
		t.Fatalf("got %q", got)
	}
}

func TestInstallPathsRejectsKanderSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink rejection")
	}
	root := setupHome(t)
	project := initGitRepo(t, filepath.Join(root, "app"))
	payload := filepath.Join(root, "payload")
	if err := os.MkdirAll(filepath.Join(payload, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, "bin", "kander"), []byte("entry\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	kanderDir := filepath.Join(project, ".kander")
	if err := os.Symlink(payload, kanderDir); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(kanderDir, "bin", "kander")
	if _, err := InstallPathsFromEntry(entry); !IsError(err) {
		t.Fatalf("expected config error, got %v", err)
	}
}

func TestProjectInstallPathsRejectsSymlinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink rejection")
	}
	root := setupHome(t)
	project := initGitRepo(t, filepath.Join(root, "app"))
	link := filepath.Join(root, "app-link")
	if err := os.Symlink(project, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ProjectInstallPaths(link); !IsError(err) {
		t.Fatalf("expected config error, got %v", err)
	}
}

func TestCursorIsAnExecutionAndReviewAgent(t *testing.T) {
	setupHome(t)
	if !contains(ExecutionAgents, "cursor") || !contains(ReviewAgents, "cursor") {
		t.Fatal("cursor missing")
	}
	if AgentExecutableName("cursor") != "cursor-agent" {
		t.Fatal(AgentExecutableName("cursor"))
	}
	if AgentExecutableName("codex") != "codex" {
		t.Fatal(AgentExecutableName("codex"))
	}
}

func TestCursorModelDefaultsUseFullIDs(t *testing.T) {
	setupHome(t)
	models := DefaultModels()
	cursor := models.Kanban["cursor"]
	if cursor["large_model"] != "cursor-grok-4.6-xhigh" || cursor["small_model"] != "cursor-grok-4.6-high" {
		t.Fatalf("%v", cursor)
	}
	if models.Review["cursor"]["model"] != "cursor-grok-4.6-xhigh" {
		t.Fatalf("%v", models.Review["cursor"])
	}
	if _, ok := cursor["large_effort"]; ok {
		t.Fatal("large_effort should be absent")
	}
	if _, ok := models.Review["cursor"]["effort"]; ok {
		t.Fatal("effort should be absent")
	}
}

func TestCursorModelIDsAcceptEmptyStrings(t *testing.T) {
	setupHome(t)
	validated, err := Validate(minimalPayload(map[string]any{
		"models": map[string]any{
			"kanban": map[string]any{"cursor": map[string]any{"large_model": "", "small_model": ""}},
			"review": map[string]any{"cursor": map[string]any{"model": ""}},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if validated.Models.Kanban["cursor"]["large_model"] != "" {
		t.Fatal(validated.Models.Kanban["cursor"])
	}
	if validated.Models.Kanban["cursor"]["small_model"] != "" {
		t.Fatal(validated.Models.Kanban["cursor"])
	}
	if validated.Models.Review["cursor"]["model"] != "" {
		t.Fatal(validated.Models.Review["cursor"])
	}
}

func TestKanbanAgentsDefaultToTheSingleKanbanAgent(t *testing.T) {
	setupHome(t)
	payload := minimalPayload(nil)
	payload["kanban_agent"] = "codex"
	validated, err := Validate(payload)
	if err != nil {
		t.Fatal(err)
	}
	if validated.KanbanAgents["large"] != "codex" || validated.KanbanAgents["small"] != "codex" {
		t.Fatalf("%v", validated.KanbanAgents)
	}
	large, err := KanbanAgentFor(validated, "large")
	if err != nil || large != "codex" {
		t.Fatal(large, err)
	}
	small, err := KanbanAgentFor(validated, "small")
	if err != nil || small != "codex" {
		t.Fatal(small, err)
	}
	if got := ExecutionAgentsInUse(validated); len(got) != 1 || got[0] != "codex" {
		t.Fatalf("%v", got)
	}
	defaults := DefaultConfig()
	if defaults.KanbanAgents["large"] != "codex" || defaults.KanbanAgents["small"] != "codex" {
		t.Fatalf("%v", defaults.KanbanAgents)
	}
}

func TestKanbanAgentsSelectTheAgentByScale(t *testing.T) {
	setupHome(t)
	payload := minimalPayload(nil)
	payload["kanban_agent"] = "codex"
	payload["kanban_agents"] = map[string]any{"small": "cursor"}
	validated, err := Validate(payload)
	if err != nil {
		t.Fatal(err)
	}
	if validated.KanbanAgents["large"] != "codex" || validated.KanbanAgents["small"] != "cursor" {
		t.Fatalf("%v", validated.KanbanAgents)
	}
	small, _ := KanbanAgentFor(validated, "small")
	large, _ := KanbanAgentFor(validated, "large")
	if small != "cursor" || large != "codex" {
		t.Fatal(small, large)
	}
	got := ExecutionAgentsInUse(validated)
	if len(got) != 2 || got[0] != "codex" || got[1] != "cursor" {
		t.Fatalf("%v", got)
	}
	if _, err := KanbanAgentFor(validated, "medium"); !IsError(err) {
		t.Fatalf("expected error, got %v", err)
	}
}

func TestKanbanAgentsRejectUnknownScalesAndAgents(t *testing.T) {
	setupHome(t)
	bads := []any{
		map[string]any{"medium": "codex"},
		map[string]any{"large": "vim"},
		[]any{"codex"},
		map[string]any{"small": nil},
	}
	for _, bad := range bads {
		payload := minimalPayload(nil)
		payload["kanban_agents"] = bad
		if _, err := Validate(payload); !IsError(err) {
			t.Fatalf("expected error for %v, got %v", bad, err)
		}
	}
}

func TestCursorRejectsUnknownModelFields(t *testing.T) {
	setupHome(t)
	_, err := Validate(minimalPayload(map[string]any{
		"models": map[string]any{"kanban": map[string]any{"cursor": map[string]any{"large_effort": "high"}}},
	}))
	if err == nil || !strings.Contains(err.Error(), "models.kanban.cursor 含未知字段") {
		t.Fatalf("got %v", err)
	}
	_, err = Validate(minimalPayload(map[string]any{
		"models": map[string]any{"review": map[string]any{"cursor": map[string]any{"effort": "xhigh"}}},
	}))
	if err == nil || !strings.Contains(err.Error(), "models.review.cursor 含未知字段") {
		t.Fatalf("got %v", err)
	}
}

func TestReviewStagesDefaultsAndValidation(t *testing.T) {
	setupHome(t)
	stages := DefaultReviewStages()
	for _, role := range ReviewRoles {
		if stages[role] != "auto" {
			t.Fatalf("%s=%s", role, stages[role])
		}
	}
	payload := map[string]any{
		"schema_version":   1,
		"welcome_complete": true,
		"kanban_agent":     "codex",
		"launcher":         "tmux",
		"reviewers":        map[string]any{"PM": "codex", "CSA": "codex", "Hacker": "codex", "QA": "codex"},
	}
	validated, err := Validate(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range ReviewRoles {
		if validated.ReviewStages[role] != "auto" {
			t.Fatalf("%s=%s", role, validated.ReviewStages[role])
		}
	}
	payload["review_stages"] = map[string]any{"PM": "required", "CSA": "skip", "Hacker": "skip", "QA": "auto"}
	validated, err = Validate(payload)
	if err != nil {
		t.Fatal(err)
	}
	if validated.ReviewStages["CSA"] != "skip" {
		t.Fatal(validated.ReviewStages)
	}
	payload["review_stages"] = map[string]any{"PM": "always"}
	if _, err := Validate(payload); !IsError(err) {
		t.Fatalf("got %v", err)
	}
	for _, invalid := range []any{nil, "auto", []any{}} {
		payload["review_stages"] = invalid
		if _, err := Validate(payload); !IsError(err) {
			t.Fatalf("invalid %v: %v", invalid, err)
		}
	}
	delete(payload, "review_stages")
	payload["language"] = "fr"
	if _, err := Validate(payload); !IsError(err) {
		t.Fatalf("got %v", err)
	}
}

func TestConfigRejectsInvalidModelsSection(t *testing.T) {
	setupHome(t)
	base := map[string]any{
		"schema_version":   1,
		"welcome_complete": true,
		"kanban_agent":     "codex",
		"launcher":         "tmux",
		"reviewers":        map[string]any{"PM": "codex", "CSA": "codex", "Hacker": "codex", "QA": "codex"},
	}
	cases := []struct {
		models   any
		fragment string
	}{
		{map[string]any{"review": map[string]any{"codex": map[string]any{"model": "x", "effort": ""}}}, "models.review.codex.effort"},
		{map[string]any{"review": map[string]any{"codex": map[string]any{"model": "a\nb"}}}, "models.review.codex.model"},
		{map[string]any{"review": map[string]any{"codex": map[string]any{"effort": "hi\rgh"}}}, "models.review.codex.effort"},
		{map[string]any{"review": map[string]any{"codex": map[string]any{"model": "a\x00b"}}}, "models.review.codex.model"},
		{map[string]any{"kanban": map[string]any{"codex": map[string]any{"large_effort": "hi\x00gh"}}}, "models.kanban.codex.large_effort"},
		{nil, "models 必须是 JSON object"},
		{map[string]any{"review": nil}, "models.review 必须是 JSON object"},
		{map[string]any{"review": map[string]any{"other": map[string]any{}}}, "models.review 含未知 agent"},
		{map[string]any{"extra": map[string]any{}}, "models 含未知键"},
	}
	for _, tc := range cases {
		payload := map[string]any{}
		for k, v := range base {
			payload[k] = v
		}
		payload["models"] = tc.models
		_, err := Validate(payload)
		if err == nil || !strings.Contains(err.Error(), tc.fragment) {
			t.Fatalf("models=%v err=%v want %s", tc.models, err, tc.fragment)
		}
	}
}

func TestUpdateAndSaveIfUnchangedPreventLostUpdate(t *testing.T) {
	root := setupHome(t)
	path := filepath.Join(root, "config.json")
	t.Setenv(EnvConfig, path)
	initial := DefaultConfig()
	initial.WelcomeComplete = true
	if _, err := Save(initial); err != nil {
		t.Fatal(err)
	}
	baseline, err := Load(false)
	if err != nil {
		t.Fatal(err)
	}
	edited := Clone(baseline)
	edited.Rules[RuleReview] = false
	if _, err := Update(func(cfg *Config) error {
		cfg.TUI.Theme = "light"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveIfUnchanged(edited, baseline); err == nil || !strings.Contains(err.Error(), "配置已被") {
		t.Fatalf("expected edit conflict, got %v", err)
	}
	loaded, err := Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TUI.Theme != "light" || !loaded.Rules[RuleReview] {
		t.Fatalf("concurrent update was overwritten: %+v", loaded)
	}
}

func TestDefaultLauncherMatchesPlatform(t *testing.T) {
	setupHome(t)
	got := DefaultConfig().Launcher
	if runtime.GOOS == "windows" {
		if got != "console" {
			t.Fatalf("got %s", got)
		}
		// herdr has a native Windows build, so auto/herdr are no longer rejected by platform.
		for _, launcher := range []string{"auto", "herdr", "console"} {
			if err := CheckLauncherPlatform(launcher); err != nil {
				t.Fatalf("%s should be accepted: %v", launcher, err)
			}
		}
		return
	}
	if got != "auto" {
		t.Fatalf("got %s", got)
	}
	if err := CheckLauncherPlatform("console"); !IsError(err) {
		t.Fatalf("console should be rejected: %v", err)
	}
	if err := CheckLauncherPlatform("auto"); err != nil {
		t.Fatal(err)
	}
}

func TestLanguagePriorityCLIOverConfigOverEnv(t *testing.T) {
	root := setupHome(t)
	path := filepath.Join(root, "cfg.json")
	t.Setenv(EnvConfig, path)
	payload := map[string]any{
		"schema_version":   1,
		"welcome_complete": true,
		"kanban_agent":     "codex",
		"launcher":         "tmux",
		"language":         "en",
		"reviewers":        map[string]any{"PM": "codex", "CSA": "codex", "Hacker": "codex", "QA": "codex"},
	}
	data, _ := json.Marshal(payload)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvLang, "cn")
	resetLanguageState()
	BindEffectiveLanguage()
	if LanguageIsChinese() {
		t.Fatal("config en should override env cn")
	}
	ApplyLanguageArgument([]string{"--lang", "cn"})
	BindEffectiveLanguage()
	if !LanguageIsChinese() {
		t.Fatal("cli cn should override config en")
	}
	if os.Getenv(EnvLangCLI) != "1" {
		t.Fatalf("KANDER_LANG_CLI=%q", os.Getenv(EnvLangCLI))
	}
}

func TestConfiguredLanguageRequiresWelcomeAndKey(t *testing.T) {
	root := setupHome(t)
	path := filepath.Join(root, "cfg.json")
	t.Setenv(EnvConfig, path)
	if ConfiguredLanguage() != "" {
		t.Fatal("missing config should be empty")
	}
	payload := map[string]any{
		"schema_version":   1,
		"welcome_complete": true,
		"kanban_agent":     "codex",
		"launcher":         "tmux",
		"language":         "en",
		"reviewers":        map[string]any{"PM": "codex", "CSA": "codex", "Hacker": "codex", "QA": "codex"},
	}
	data, _ := json.Marshal(payload)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if ConfiguredLanguage() != "en" {
		t.Fatalf("got %q", ConfiguredLanguage())
	}
	delete(payload, "language")
	data, _ = json.Marshal(payload)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if ConfiguredLanguage() != "" {
		t.Fatalf("legacy got %q", ConfiguredLanguage())
	}
}

func TestLoadMissingOKAndInvalidJSON(t *testing.T) {
	root := setupHome(t)
	path := filepath.Join(root, "cfg.json")
	t.Setenv(EnvConfig, path)
	cfg, err := Load(true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WelcomeComplete {
		t.Fatal("default should be incomplete")
	}
	if exists, err := Exists(); err != nil || exists {
		t.Fatalf("exists=%v err=%v", exists, err)
	}
	if _, err := Load(false); !IsError(err) {
		t.Fatalf("expected missing error, got %v", err)
	}
	if err := os.WriteFile(path, []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if exists, err := Exists(); err != nil || !exists {
		t.Fatalf("exists=%v err=%v", exists, err)
	}
	if _, err := Load(true); !IsError(err) {
		t.Fatalf("expected read error, got %v", err)
	}
}

func TestEffectiveIgnoresIncompleteWelcome(t *testing.T) {
	setupHome(t)
	payload := map[string]any{
		"schema_version":   1,
		"welcome_complete": false,
		"kanban_agent":     "cursor",
		"launcher":         "tmux",
		"reviewers":        map[string]any{"PM": "cursor", "CSA": "cursor", "Hacker": "cursor", "QA": "cursor"},
	}
	cfg, err := Validate(payload)
	if err != nil {
		t.Fatal(err)
	}
	effective, err := Effective(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if effective.KanbanAgent != "codex" {
		t.Fatalf("%+v", effective)
	}
}

func TestFormatConfigLinesAndReviewHelpers(t *testing.T) {
	setupHome(t)
	cfg := DefaultConfig()
	cfg.WelcomeComplete = false
	lines, err := FormatConfigLines(cfg)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "初始化: 未完成") || !strings.Contains(joined, "看板 Agent: codex") {
		t.Fatalf("%s", joined)
	}
	complete := DefaultConfig()
	complete.WelcomeComplete = true
	complete.ReviewStages["CSA"] = "skip"
	complete.ReviewStages["PM"] = "required"
	stageLines, err := ReviewStageLines(complete)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(stageLines, " ") != "required skip auto auto" {
		t.Fatalf("%v", stageLines)
	}
	modelLines, err := ReviewModelLines(complete, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if modelLines[0] != "gpt-5.6-sol" || modelLines[1] != "high" {
		t.Fatalf("%v", modelLines)
	}
}

func TestLanguageNullIsRejectedWhileMissingDefaults(t *testing.T) {
	setupHome(t)
	payload := map[string]any{
		"schema_version":   1,
		"welcome_complete": true,
		"kanban_agent":     "codex",
		"launcher":         "tmux",
		"reviewers":        map[string]any{"PM": "codex", "CSA": "codex", "Hacker": "codex", "QA": "codex"},
	}
	missing, err := Validate(payload)
	if err != nil {
		t.Fatal(err)
	}
	if missing.Language != "cn" {
		t.Fatalf("missing language=%s", missing.Language)
	}
	payload["language"] = nil
	if _, err := Validate(payload); !IsError(err) {
		t.Fatalf("expected error for language null, got %v", err)
	}
	raw := []byte(`{"schema_version":1,"welcome_complete":true,"kanban_agent":"codex","launcher":"tmux","language":null,"reviewers":{"PM":"codex","CSA":"codex","Hacker":"codex","QA":"codex"}}`)
	if _, err := ValidateJSON(raw); !IsError(err) {
		t.Fatalf("expected JSON null language error, got %v", err)
	}
}

func TestValidateJSONRejectsTrailingTokens(t *testing.T) {
	setupHome(t)
	raw := []byte(`{"schema_version":1,"welcome_complete":true,"kanban_agent":"codex","launcher":"tmux","reviewers":{"PM":"codex","CSA":"codex","Hacker":"codex","QA":"codex"}}{"extra":true}`)
	if _, err := ValidateJSON(raw); !IsError(err) {
		t.Fatalf("expected trailing JSON error, got %v", err)
	}
}

func TestJSONRoundTripUsesNumberSchema(t *testing.T) {
	setupHome(t)
	raw := []byte(`{"schema_version":1,"welcome_complete":true,"kanban_agent":"codex","launcher":"tmux","reviewers":{"PM":"codex","CSA":"codex","Hacker":"codex","QA":"codex"}}`)
	cfg, err := ValidateJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SchemaVersion != 1 {
		t.Fatal(cfg.SchemaVersion)
	}
}
