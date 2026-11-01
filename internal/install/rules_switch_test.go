package install

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

// writeScopeSwitches saves a scope config with the given switches at the global config path.
func writeScopeSwitches(t *testing.T, home string, global bool, agents map[string]bool) {
	t.Helper()
	file := filepath.Join(home, ".config", "kander", "config.json")
	if _, err := config.UpdateAt(file, func(cfg *config.Config) error {
		cfg.InstallRules = global
		for agent, value := range agents {
			config.SetAgentInstallRules(cfg, agent, &value)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func readBytes(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPerformSkipsAgentsWithSwitchOff(t *testing.T) {
	home := setupInstallHome(t)
	for _, dir := range []string{".claude", ".codex", ".pi/agent"} {
		if err := os.MkdirAll(filepath.Join(home, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	codex := filepath.Join(home, ".codex", "AGENTS.md")
	override := filepath.Join(home, ".codex", "AGENTS.override.md")
	writeIntegrateFile(t, codex, "# Personal codex rules\n")
	writeIntegrateFile(t, override, "# Override\n")
	writeScopeSwitches(t, home, true, map[string]bool{"codex": false, "pi": false})
	result, err := Perform(Request{Language: "cn", Source: stubBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	if got := readBytes(t, codex); got != "# Personal codex rules\n" {
		t.Fatalf("codex rules file changed: %q", got)
	}
	if got := readBytes(t, override); got != "# Override\n" {
		t.Fatalf("override changed: %q", got)
	}
	if _, err := os.Lstat(piExtensionTarget(t, home, false)); !os.IsNotExist(err) {
		t.Fatalf("pi extension written: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".pi", "agent", "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("pi rules file written: %v", err)
	}
	if !strings.Contains(readBytes(t, filepath.Join(home, ".claude", "CLAUDE.md")), "KANDER-AGENTS.md") {
		t.Fatal("claude reference missing")
	}
	var disabled []string
	for _, item := range result.Integrations {
		if item.Status == IntegrationDisabled {
			disabled = append(disabled, item.DisabledBy...)
		}
	}
	if !slices.Equal(disabled, []string{"codex", "pi"}) {
		t.Fatalf("disabled=%v", disabled)
	}
}

func TestRulesRootStillExtractedWithAllSwitchesOff(t *testing.T) {
	home := setupInstallHome(t)
	writeScopeSwitches(t, home, false, nil)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Perform(Request{Language: "cn", Source: stubBinary(t)}); err != nil {
		t.Fatal(err)
	}
	paths, _ := config.GlobalInstallPaths()
	if _, err := os.Stat(RulesEntry(paths)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".claude", "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("claude rules file created: %v", err)
	}
	if err := os.Remove(RulesEntry(paths)); err != nil {
		t.Fatal(err)
	}
	if _, err := RepairRules(paths); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(RulesEntry(paths)); err != nil {
		t.Fatalf("repair did not restore the entry: %v", err)
	}
}

func TestEnsurePrimitivesHonorSwitchOff(t *testing.T) {
	home := setupInstallHome(t)
	writeScopeSwitches(t, home, false, nil)
	paths, _ := config.GlobalInstallPaths()
	for _, dir := range []string{".codex", ".pi/agent/extensions"} {
		if err := os.MkdirAll(filepath.Join(home, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	codex := filepath.Join(home, ".codex", "AGENTS.md")
	writeIntegrateFile(t, codex, "# Mine\n")
	outcome, err := EnsureRulesIntegration("codex", paths)
	if err != nil || outcome.Status != IntegrationDisabled || !slices.Equal(outcome.DisabledBy, []string{"codex"}) {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	if got := readBytes(t, codex); got != "# Mine\n" {
		t.Fatalf("codex changed: %q", got)
	}
	target := piExtensionTarget(t, home, false)
	writeIntegrateFile(t, target, "// old\n")
	ext, err := EnsureExtension("pi", paths)
	if err != nil || ext.Status != ExtensionDisabled {
		t.Fatalf("extension=%+v err=%v", ext, err)
	}
	if got := readBytes(t, target); got != "// old\n" {
		t.Fatalf("extension changed: %q", got)
	}
}

func TestLegacyEntrySymlinkKeptWhenSwitchOff(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := setupInstallHome(t)
	writeScopeSwitches(t, home, true, map[string]bool{"claude": false})
	paths, _ := config.GlobalInstallPaths()
	link := filepath.Join(home, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(legacyRulesEntry(paths), link); err != nil {
		t.Fatal(err)
	}
	if removed := removeLegacyEntrySymlinks(paths); len(removed) != 0 {
		t.Fatalf("removed=%v", removed)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("legacy link replaced: %v", err)
	}
}

func TestSharedProjectRulesFileNeedsEveryReader(t *testing.T) {
	setupInstallHome(t)
	project := initGitRepo(t, filepath.Join(t.TempDir(), "app"))
	paths, err := config.ProjectInstallPaths(project)
	if err != nil {
		t.Fatal(err)
	}
	off := false
	if _, err := config.UpdateAt(paths.ConfigPath, func(cfg *config.Config) error {
		config.SetAgentInstallRules(cfg, "pi", &off)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	outcome, err := EnsureRulesIntegration("codex", paths)
	if err != nil || outcome.Status != IntegrationDisabled || !slices.Equal(outcome.DisabledBy, []string{"pi"}) {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	if _, err := os.Lstat(filepath.Join(project, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("shared AGENTS.md written: %v", err)
	}
	claude, err := EnsureRulesIntegration("claude", paths)
	if err != nil || claude.Status != IntegrationCreated {
		t.Fatalf("claude=%+v err=%v", claude, err)
	}
}

func TestCommentedReferenceIsInactive(t *testing.T) {
	home := setupInstallHome(t)
	paths, _ := config.GlobalInstallPaths()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeIntegrateFile(t, RulesEntry(paths), "# entry\n")
	target := filepath.Join(home, ".claude", "CLAUDE.md")
	body := "# Mine\n\n<!--\n@~/.agents/kander/KANDER-AGENTS.md\n-->\n"
	writeIntegrateFile(t, target, body)
	outcome, err := EnsureRulesIntegration("claude", paths)
	if err != nil || outcome.Status != IntegrationPresent || !outcome.Inactive {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	if got := readBytes(t, target); got != body {
		t.Fatalf("file changed: %q", got)
	}
	writeIntegrateFile(t, target, "# Mine\n\n@~/.agents/kander/KANDER-AGENTS.md\n")
	outcome, err = EnsureRulesIntegration("claude", paths)
	if err != nil || outcome.Status != IntegrationPresent || outcome.Inactive {
		t.Fatalf("active reference outcome=%+v err=%v", outcome, err)
	}
}

func TestSymlinkedRulesFileNeedsEveryReader(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := setupInstallHome(t)
	writeScopeSwitches(t, home, true, map[string]bool{"codex": false})
	paths, _ := config.GlobalInstallPaths()
	codex := filepath.Join(home, ".codex", "AGENTS.md")
	writeIntegrateFile(t, codex, "# Mine\n")
	if err := os.MkdirAll(filepath.Join(home, ".grok"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(codex, filepath.Join(home, ".grok", "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	outcome, err := EnsureRulesIntegration("grok", paths)
	if err != nil || outcome.Status != IntegrationDisabled || !slices.Equal(outcome.DisabledBy, []string{"codex"}) {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	if got := readBytes(t, codex); got != "# Mine\n" {
		t.Fatalf("codex file written through the grok link: %q", got)
	}
	var shared []string
	for _, group := range PlanRulesInjection(paths) {
		if slices.Contains(group.Agents, "grok") {
			shared = group.Agents
		}
	}
	if !slices.Equal(shared, []string{"codex", "grok"}) {
		t.Fatalf("linked rules files not grouped: %v", shared)
	}
}
