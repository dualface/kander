package install

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
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

func TestPerformSkipsExtensionWithSwitchOff(t *testing.T) {
	home := setupInstallHome(t)
	for _, dir := range []string{".claude", ".codex", ".pi/agent"} {
		if err := os.MkdirAll(filepath.Join(home, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeScopeSwitches(t, home, true, map[string]bool{"pi": false})
	result, err := Perform(Request{Language: "cn", Source: stubBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(piExtensionTarget(t, home, false)); !os.IsNotExist(err) {
		t.Fatalf("pi extension written: %v", err)
	}
	if len(result.Extensions) != 1 || result.Extensions[0].Status != ExtensionDisabled {
		t.Fatalf("extensions=%+v", result.Extensions)
	}
	for _, file := range []string{filepath.Join(home, ".pi", "agent", "AGENTS.md"), filepath.Join(home, ".claude", "CLAUDE.md"), filepath.Join(home, ".codex", "AGENTS.md")} {
		if _, err := os.Lstat(file); !os.IsNotExist(err) {
			t.Fatalf("global rules file written: %s: %v", file, err)
		}
	}
}

func TestRulesRootStillExtractedWithAllSwitchesOff(t *testing.T) {
	home := setupInstallHome(t)
	writeScopeSwitches(t, home, false, nil)
	if _, err := Perform(Request{Language: "cn", Source: stubBinary(t)}); err != nil {
		t.Fatal(err)
	}
	paths, _ := config.GlobalInstallPaths()
	if _, err := os.Stat(RulesEntry(paths)); err != nil {
		t.Fatal(err)
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
	project, home := projectFixture(t)
	writeScopeSwitches(t, home, false, nil)
	paths := project.Paths
	switches, err := config.ScopeInstallRules(config.InstallPaths{ConfigPath: filepath.Join(home, ".config", "kander", "config.json")})
	if err != nil {
		t.Fatal(err)
	}
	agents := filepath.Join(project.Root, "AGENTS.md")
	writeIntegrateFile(t, agents, "# Mine\n")
	outcome, err := EnsureProjectReference("codex", project, switches)
	if err != nil || outcome.Status != IntegrationDisabled || !slices.Contains(outcome.DisabledBy, "codex") {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	if got := readBytes(t, agents); got != "# Mine\n" {
		t.Fatalf("AGENTS.md changed: %q", got)
	}
	if err := os.MkdirAll(filepath.Join(home, ".pi", "agent", "extensions"), 0o755); err != nil {
		t.Fatal(err)
	}
	paths.ConfigPath = filepath.Join(home, ".config", "kander", "config.json")
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

// The cleanup removes what Kander wrote whatever the switches say: they decide what Kander
// writes, not what it takes back.
func TestGlobalCleanupIgnoresSwitches(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := setupInstallHome(t)
	writeScopeSwitches(t, home, false, nil)
	paths, _ := config.GlobalInstallPaths()
	link := filepath.Join(home, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(legacyRulesEntry(paths), link); err != nil {
		t.Fatal(err)
	}
	codex := filepath.Join(home, ".codex", "AGENTS.md")
	writeIntegrateFile(t, codex, "# Mine\n\n@~/.agents/kander/KANDER-AGENTS.md\n")
	if result := CleanGlobalInjection(paths); result.Err != nil || len(result.Removed) != 2 {
		t.Fatalf("cleanup=%+v", result)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("legacy link kept: %v", err)
	}
	if got := readBytes(t, codex); got != "# Mine\n" {
		t.Fatalf("codex=%q", got)
	}
}

func TestCommentedReferenceIsInactive(t *testing.T) {
	project, _ := projectFixture(t)
	target := filepath.Join(project.Root, "CLAUDE.md")
	body := "# Mine\n\n<!--\n@~/.agents/kander/KANDER-AGENTS.md\n-->\n"
	writeIntegrateFile(t, target, body)
	outcome, err := EnsureProjectReference("claude", project, config.InstallRulesOf(nil))
	if err != nil || outcome.Status != IntegrationPresent || !outcome.Inactive {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	if got := readBytes(t, target); got != body {
		t.Fatalf("file changed: %q", got)
	}
	writeIntegrateFile(t, target, "# Mine\n\n@~/.agents/kander/KANDER-AGENTS.md\n")
	outcome, err = EnsureProjectReference("claude", project, config.InstallRulesOf(nil))
	if err != nil || outcome.Status != IntegrationPresent || outcome.Inactive {
		t.Fatalf("active reference outcome=%+v err=%v", outcome, err)
	}
}

func TestSymlinkedRulesFileNeedsEveryReader(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	project, _ := projectFixture(t)
	cfg := config.DefaultConfig()
	off := false
	config.SetAgentInstallRules(cfg, "codex", &off)
	agents := filepath.Join(project.Root, "AGENTS.md")
	writeIntegrateFile(t, agents, "# Mine\n")
	if err := os.Symlink("AGENTS.md", filepath.Join(project.Root, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	outcome, err := EnsureProjectReference("claude", project, config.InstallRulesOf(cfg))
	if err != nil || outcome.Status != IntegrationDisabled || !slices.Equal(outcome.DisabledBy, []string{"codex"}) {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	if got := readBytes(t, agents); got != "# Mine\n" {
		t.Fatalf("AGENTS.md written through the CLAUDE.md link: %q", got)
	}
}
