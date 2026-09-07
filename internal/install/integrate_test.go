package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func writeIntegrateFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func globalIntegrationPaths(t *testing.T, home string) config.InstallPaths {
	t.Helper()
	paths := config.InstallPaths{Mode: config.ModeGlobal, RulesDir: filepath.Join(home, ".agents")}
	writeIntegrateFile(t, RulesEntry(paths), "# Kander entry\n")
	return paths
}

func TestEnsureRulesIntegrationCreatesClaudeImport(t *testing.T) {
	home := setupInstallHome(t)
	paths := globalIntegrationPaths(t, home)
	outcome, err := EnsureRulesIntegration("claude", paths)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".claude", "CLAUDE.md")
	if outcome.Status != IntegrationCreated || outcome.Target != target {
		t.Fatalf("outcome=%+v", outcome)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "@~/.agents/KANDER-AGENTS.md\n" {
		t.Fatalf("content=%q", got)
	}
	again, err := EnsureRulesIntegration("claude", paths)
	if err != nil || again.Status != IntegrationPresent {
		t.Fatalf("second run: %+v %v", again, err)
	}
	unchanged, _ := os.ReadFile(target)
	if string(unchanged) != string(got) {
		t.Fatalf("duplicated reference: %q", unchanged)
	}
}

func TestEnsureRulesIntegrationAppendsToExistingFile(t *testing.T) {
	home := setupInstallHome(t)
	paths := globalIntegrationPaths(t, home)
	target := filepath.Join(home, ".claude", "CLAUDE.md")
	writeIntegrateFile(t, target, "# My own rules\n\nAlways be kind.\n")
	outcome, err := EnsureRulesIntegration("claude", paths)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != IntegrationUpdated {
		t.Fatalf("outcome=%+v", outcome)
	}
	got, _ := os.ReadFile(target)
	text := string(got)
	if !strings.HasPrefix(text, "# My own rules\n\nAlways be kind.\n") {
		t.Fatalf("existing content lost: %q", text)
	}
	if strings.Count(text, "@~/.agents/KANDER-AGENTS.md") != 1 {
		t.Fatalf("reference count wrong: %q", text)
	}
	if again, err := EnsureRulesIntegration("claude", paths); err != nil || again.Status != IntegrationPresent {
		t.Fatalf("second run: %+v %v", again, err)
	}
}

func TestEnsureRulesIntegrationWritesAgentsInstruction(t *testing.T) {
	home := setupInstallHome(t)
	paths := globalIntegrationPaths(t, home)
	outcome, err := EnsureRulesIntegration("codex", paths)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".codex", "AGENTS.md")
	if outcome.Status != IntegrationCreated || outcome.Target != target {
		t.Fatalf("outcome=%+v", outcome)
	}
	got, _ := os.ReadFile(target)
	if !strings.Contains(string(got), "~/.agents/KANDER-AGENTS.md") {
		t.Fatalf("content=%q", got)
	}
	if again, err := EnsureRulesIntegration("codex", paths); err != nil || again.Status != IntegrationPresent {
		t.Fatalf("second run: %+v %v", again, err)
	}
	single, _ := os.ReadFile(target)
	if strings.Count(string(single), "Kander Rules Entry") != 1 {
		t.Fatalf("instruction duplicated: %q", single)
	}
}

func TestEnsureRulesIntegrationProjectScope(t *testing.T) {
	setupInstallHome(t)
	project := t.TempDir()
	paths := config.InstallPaths{
		Mode:        config.ModeProject,
		ProjectRoot: project,
		RulesDir:    filepath.Join(project, ".kander", "rules"),
	}
	writeIntegrateFile(t, RulesEntry(paths), "# Kander entry\n")

	claude, err := EnsureRulesIntegration("claude", paths)
	if err != nil || claude.Status != IntegrationCreated {
		t.Fatalf("claude: %+v %v", claude, err)
	}
	got, _ := os.ReadFile(filepath.Join(project, "CLAUDE.md"))
	if string(got) != "@.kander/rules/KANDER-AGENTS.md\n" {
		t.Fatalf("claude content=%q", got)
	}

	agents := filepath.Join(project, "AGENTS.md")
	writeIntegrateFile(t, agents, "# Project conventions\n")
	codex, err := EnsureRulesIntegration("codex", paths)
	if err != nil || codex.Status != IntegrationUpdated {
		t.Fatalf("codex: %+v %v", codex, err)
	}
	text, _ := os.ReadFile(agents)
	if !strings.HasPrefix(string(text), "# Project conventions\n") ||
		!strings.Contains(string(text), ".kander/rules/KANDER-AGENTS.md") {
		t.Fatalf("agents content=%q", text)
	}
	if again, err := EnsureRulesIntegration("codex", paths); err != nil || again.Status != IntegrationPresent {
		t.Fatalf("second run: %+v %v", again, err)
	}
}

func TestEnsureRulesIntegrationNegationWordsDoNotRepeatAppend(t *testing.T) {
	home := setupInstallHome(t)
	paths := globalIntegrationPaths(t, home)
	for agent, target := range map[string]string{
		"claude": filepath.Join(home, ".claude", "CLAUDE.md"),
		"codex":  filepath.Join(home, ".codex", "AGENTS.md"),
	} {
		writeIntegrateFile(t, target, "# Notes\n\nThe old API is deprecated; ignore lint warnings there.\n")
		if outcome, err := EnsureRulesIntegration(agent, paths); err != nil || outcome.Status != IntegrationUpdated {
			t.Fatalf("%s first run: %+v %v", agent, outcome, err)
		}
		for i := 0; i < 2; i++ {
			if outcome, err := EnsureRulesIntegration(agent, paths); err != nil || outcome.Status != IntegrationPresent {
				t.Fatalf("%s rerun %d: %+v %v", agent, i, outcome, err)
			}
		}
		got, _ := os.ReadFile(target)
		if strings.Count(string(got), "KANDER-AGENTS.md") != 1 {
			t.Fatalf("%s reference repeated: %q", agent, got)
		}
	}
}

func TestEnsureRulesIntegrationSurvivesMissingEntry(t *testing.T) {
	home := setupInstallHome(t)
	paths := globalIntegrationPaths(t, home)
	if outcome, err := EnsureRulesIntegration("claude", paths); err != nil || outcome.Status != IntegrationCreated {
		t.Fatalf("first run: %+v %v", outcome, err)
	}
	if err := os.Remove(RulesEntry(paths)); err != nil {
		t.Fatal(err)
	}
	if outcome, err := EnsureRulesIntegration("claude", paths); err != nil || outcome.Status != IntegrationPresent {
		t.Fatalf("rerun without entry: %+v %v", outcome, err)
	}
	got, _ := os.ReadFile(filepath.Join(home, ".claude", "CLAUDE.md"))
	if strings.Count(string(got), "KANDER-AGENTS.md") != 1 {
		t.Fatalf("reference repeated: %q", got)
	}
}

func TestEnsureRulesIntegrationProjectSymlinks(t *testing.T) {
	setupInstallHome(t)
	project := t.TempDir()
	paths := config.InstallPaths{
		Mode:        config.ModeProject,
		ProjectRoot: project,
		RulesDir:    filepath.Join(project, ".kander", "rules"),
	}
	writeIntegrateFile(t, RulesEntry(paths), "# Kander entry\n")

	outside := filepath.Join(t.TempDir(), "victim.md")
	writeIntegrateFile(t, outside, "precious\n")
	agents := filepath.Join(project, "AGENTS.md")
	if err := os.Symlink(outside, agents); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := EnsureRulesIntegration("codex", paths); err == nil {
		t.Fatal("escaping symlink must be refused")
	}
	if got, _ := os.ReadFile(outside); string(got) != "precious\n" {
		t.Fatalf("outside file mutated: %q", got)
	}

	if err := os.Remove(agents); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(project, "docs", "shared-agents.md")
	writeIntegrateFile(t, inside, "# Shared\n")
	if err := os.Symlink(inside, agents); err != nil {
		t.Fatal(err)
	}
	if outcome, err := EnsureRulesIntegration("codex", paths); err != nil || outcome.Status != IntegrationUpdated {
		t.Fatalf("inside symlink: %+v %v", outcome, err)
	}
	if outcome, err := EnsureRulesIntegration("codex", paths); err != nil || outcome.Status != IntegrationPresent {
		t.Fatalf("inside symlink rerun: %+v %v", outcome, err)
	}
	got, _ := os.ReadFile(inside)
	if strings.Count(string(got), "KANDER-AGENTS.md") != 1 || !strings.HasPrefix(string(got), "# Shared\n") {
		t.Fatalf("inside content=%q", got)
	}
}

func TestPerformGlobalIntegratesOnlyExistingAgentDirs(t *testing.T) {
	home := setupInstallHome(t)
	for _, dir := range []string{".claude", ".codex"} {
		if err := os.Mkdir(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	result, err := Perform(Request{Language: "cn", Source: stubBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Integrations) != 2 {
		t.Fatalf("integrations=%+v", result.Integrations)
	}
	for _, item := range result.Integrations {
		if item.Err != nil || item.Status != IntegrationCreated {
			t.Fatalf("integration=%+v err=%v", item.IntegrationOutcome, item.Err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".cursor")); !os.IsNotExist(err) {
		t.Fatal("created a directory for an absent agent")
	}
}
