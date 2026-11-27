package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/rules"
	"github.com/dualface/kander/v2/skills"
)

// legacyProjectFixture builds the main worktree a project install of an earlier release left:
// the binary, the rules with their state file, the project-level skill and extension, the
// exclude line, a terminal definition, and the project configuration.
func legacyProjectFixture(t *testing.T, binary string) string {
	t.Helper()
	setupInstallHome(t)
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	root := initGitRepo(t, filepath.Join(t.TempDir(), "app"))
	install := filepath.Join(root, ".kander")
	writeIntegrateFile(t, filepath.Join(install, "bin", binaryName()), binary)
	if err := os.Chmod(filepath.Join(install, "bin", binaryName()), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range rules.Names() {
		data, err := rules.File(name)
		if err != nil {
			t.Fatal(err)
		}
		writeIntegrateFile(t, filepath.Join(install, "rules", filepath.FromSlash(name)), string(data))
	}
	writeIntegrateFile(t, filepath.Join(install, "rules", stateFileName), "{}\n")
	writeIntegrateFile(t, filepath.Join(install, "rules", "notes.md"), "mine\n")
	writeIntegrateFile(t, filepath.Join(install, "share", "terminals", "zellij.json"), "{}\n")
	cfg := config.DefaultConfig()
	cfg.KanbanAgent = "codex"
	cfg.TUI.Theme = "dark"
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	writeIntegrateFile(t, filepath.Join(install, "config.json"), string(data))
	exclude := filepath.Join(root, ".git", "info", "exclude")
	writeIntegrateFile(t, exclude, "# local\n/.kander/\n*.log\n")
	_, extension, _ := config.AgentExtension("pi")
	writeIntegrateFile(t, filepath.Join(root, ".pi", "extensions", "kander-rules.ts"), string(extension))
	skill := filepath.Join(root, ".agents", "skills", leaderName)
	legacyScope := config.InstallPaths{RulesDir: filepath.Join(install, "rules")}
	s, err := inspectLeaderSkill(skill, legacyScope)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.install(legacyScope); err != nil {
		t.Fatal(err)
	}
	return root
}

func kanderScript() string {
	return "#!/bin/sh\necho 'kander 1.9.0'\n"
}

func TestLegacyProjectInstallCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake binary is a shell script")
	}
	root := legacyProjectFixture(t, kanderScript())
	overlay := filepath.Join(root, config.OverlayFilename)
	writeIntegrateFile(t, overlay, `{"tui":{"theme":"light"}}`)
	plan := InspectLegacyProjectInstall(root)
	if !plan.Found() || plan.Config == nil {
		t.Fatalf("plan=%+v", plan)
	}
	if !slices.Contains(plan.Config.Migrate, "kanban_agent") || !slices.Equal(plan.Config.Conflicts, []string{"tui"}) || len(plan.Config.Retained) != 0 {
		t.Fatalf("config plan=%+v", plan.Config)
	}
	result, err := RepairLegacyProjectInstall(plan)
	if err != nil {
		t.Fatal(err)
	}
	install := filepath.Join(root, ".kander")
	for _, gone := range []string{
		filepath.Join(install, "bin"),
		filepath.Join(install, "rules", "KANDER-AGENTS.md"),
		filepath.Join(install, "rules", stateFileName),
		filepath.Join(install, "rules", "snippets"),
		filepath.Join(root, ".pi"),
		filepath.Join(root, ".agents"),
	} {
		if _, err := os.Lstat(gone); !os.IsNotExist(err) {
			t.Errorf("%s kept: %v", gone, err)
		}
	}
	for path, want := range map[string]string{
		filepath.Join(install, "rules", "notes.md"):                 "mine\n",
		filepath.Join(install, "share", "terminals", "zellij.json"): "{}\n",
		filepath.Join(root, ".git", "info", "exclude"):              "# local\n*.log\n",
	} {
		if got := readBytes(t, path); got != want {
			t.Errorf("%s=%q want %q", path, got, want)
		}
	}
	migrated, err := config.ReadOverlayFile(overlay)
	if err != nil {
		t.Fatal(err)
	}
	if migrated["kanban_agent"] != "codex" || migrated["tui"].(map[string]any)["theme"] != "light" {
		t.Fatalf("overlay=%v", migrated)
	}
	for _, key := range []string{"schema_version", "welcome_complete", "install_rules"} {
		if _, ok := migrated[key]; ok {
			t.Fatalf("overlay took %s", key)
		}
	}
	// The conflict keeps the file for the user to decide.
	if result.ConfigRemoved {
		t.Fatal("config with a conflict removed")
	}
	if _, err := os.Stat(filepath.Join(install, "config.json")); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyProjectConfigRemovedWhenFullyMigrated(t *testing.T) {
	root := legacyProjectFixture(t, "not a program\n")
	plan := InspectLegacyProjectInstall(root)
	result, err := RepairLegacyProjectInstall(plan)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ConfigRemoved || len(result.Migrated) == 0 {
		t.Fatalf("result=%+v", result)
	}
	// A binary that does not report itself as kander stays.
	binary := filepath.Join(root, ".kander", "bin", binaryName())
	if _, err := os.Stat(binary); err != nil {
		t.Fatalf("unconfirmed binary removed: %v", err)
	}
	if len(result.Kept) != 1 || result.Kept[0].Path != binary {
		t.Fatalf("kept=%+v", result.Kept)
	}
}

func TestLegacyProjectConfigRetainsScopeOnlySettings(t *testing.T) {
	root := legacyProjectFixture(t, "x")
	path := filepath.Join(root, ".kander", "config.json")
	var raw map[string]any
	if err := json.Unmarshal([]byte(readBytes(t, path)), &raw); err != nil {
		t.Fatal(err)
	}
	raw["install_rules"] = false
	raw["agents"] = map[string]any{"codex": map[string]any{"config_dir": "/elsewhere", "path": "codex-wrapper"}}
	data, _ := json.Marshal(raw)
	writeIntegrateFile(t, path, string(data))
	plan := InspectLegacyProjectInstall(root)
	if !slices.Equal(plan.Config.Retained, []string{"agents.codex.config_dir", "install_rules"}) {
		t.Fatalf("retained=%v", plan.Config.Retained)
	}
	if _, err := RepairLegacyProjectInstall(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config with retained settings removed: %v", err)
	}
	overlay, err := config.ReadOverlayFile(filepath.Join(root, config.OverlayFilename))
	if err != nil {
		t.Fatal(err)
	}
	agents := overlay["agents"].(map[string]any)["codex"].(map[string]any)
	if agents["path"] != "codex-wrapper" || agents["config_dir"] != nil {
		t.Fatalf("agents=%v", agents)
	}
}

func TestLegacyProjectKeepsModifiedSkillAndExtension(t *testing.T) {
	root := legacyProjectFixture(t, "x")
	extension := filepath.Join(root, ".pi", "extensions", "kander-rules.ts")
	writeIntegrateFile(t, extension, "// mine\n")
	skill := filepath.Join(root, ".agents", "skills", leaderName, "SKILL.md")
	writeIntegrateFile(t, skill, "my leader\n")
	plan := InspectLegacyProjectInstall(root)
	if _, err := RepairLegacyProjectInstall(plan); err != nil {
		t.Fatal(err)
	}
	if got := readBytes(t, extension); got != "// mine\n" {
		t.Fatalf("extension=%q", got)
	}
	if got := readBytes(t, skill); got != "my leader\n" {
		t.Fatalf("skill=%q", got)
	}
	var reasons []string
	for _, item := range plan.Items {
		if item.Action == UninstallKeep {
			reasons = append(reasons, item.Path)
		}
	}
	if !slices.Contains(reasons, extension) || !slices.ContainsFunc(reasons, func(path string) bool { return strings.HasPrefix(path, filepath.Dir(skill)) }) {
		t.Fatalf("kept=%v", reasons)
	}
	if _, err := skills.Leader.ReadFile(leaderName + "/SKILL.md"); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyProjectNeverFollowsTheKanderLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	setupInstallHome(t)
	root := initGitRepo(t, filepath.Join(t.TempDir(), "app"))
	outside := t.TempDir()
	writeIntegrateFile(t, filepath.Join(outside, "rules", stateFileName), "{}\n")
	if err := os.Symlink(outside, filepath.Join(root, ".kander")); err != nil {
		t.Fatal(err)
	}
	plan := InspectLegacyProjectInstall(root)
	if _, err := RepairLegacyProjectInstall(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, "rules", stateFileName)); err != nil {
		t.Fatalf("removed through the link: %v", err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Action != UninstallKeep {
		t.Fatalf("items=%+v", plan.Items)
	}
}

func TestLegacyProjectNothingToDo(t *testing.T) {
	setupInstallHome(t)
	root := initGitRepo(t, filepath.Join(t.TempDir(), "app"))
	if plan := InspectLegacyProjectInstall(root); plan.Found() {
		t.Fatalf("plan=%+v", plan)
	}
}
