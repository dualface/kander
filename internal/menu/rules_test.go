package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func writeRulesFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRulesIntegrationUsesInstallScope(t *testing.T) {
	for _, agent := range []string{"codex", "claude", "grok", "cursor"} {
		t.Run(agent, func(t *testing.T) {
			home, project := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			globalEntry := filepath.Join(home, ".agents", "KANDER-AGENTS.md")
			projectEntry := filepath.Join(project, ".kander", "rules", "KANDER-AGENTS.md")
			globalRules, projectRules := "# Global Kander rules\n", "# Project Kander rules\n"
			writeRulesFile(t, globalEntry, globalRules)
			writeRulesFile(t, projectEntry, projectRules)
			name := "AGENTS.md"
			if agent == "claude" {
				name = "CLAUDE.md"
				globalRules, projectRules = "@"+globalEntry+"\n", "@"+projectEntry+"\n"
			}
			globalTarget := filepath.Join(home, "."+agent, name)
			projectTarget := filepath.Join(project, name)
			globalPaths := config.InstallPaths{Mode: config.ModeGlobal, RulesDir: filepath.Dir(globalEntry)}
			projectPaths := config.InstallPaths{Mode: config.ModeProject, ProjectRoot: project, RulesDir: filepath.Dir(projectEntry)}
			check := func(paths config.InstallPaths, want bool, target string) {
				t.Helper()
				ok, detail := rulesIntegration(agent, paths)
				if ok != want || !strings.Contains(detail, target) {
					t.Errorf("mode=%s integrated=%v want=%v target=%s detail=%s", paths.Mode, ok, want, target, detail)
				}
			}

			// A global file cannot satisfy project integration, even if it names the project entry.
			writeRulesFile(t, globalTarget, projectRules)
			check(projectPaths, false, projectTarget)
			writeRulesFile(t, projectTarget, projectRules)
			check(projectPaths, true, projectTarget)

			// Both installations can remain integrated with their own entries at the same time.
			writeRulesFile(t, globalTarget, globalRules)
			check(globalPaths, true, globalTarget)
			check(projectPaths, true, projectTarget)
			writeRulesFile(t, projectTarget, globalRules)
			check(projectPaths, false, projectTarget)
		})
	}
}

func TestRulesIntegrationResolvesClaudeImportsFromRulesFile(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home with spaces")
	project := filepath.Join(t.TempDir(), "main worktree")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, tc := range []struct {
		name, target, reference string
		paths                   config.InstallPaths
	}{
		{
			name: "global", target: filepath.Join(home, ".claude", "CLAUDE.md"),
			reference: "@../.agents/KANDER-AGENTS.md\n",
			paths:     config.InstallPaths{Mode: config.ModeGlobal, RulesDir: filepath.Join(home, ".agents")},
		},
		{
			name: "project", target: filepath.Join(project, "CLAUDE.md"),
			reference: "@.kander/rules/KANDER-AGENTS.md\n",
			paths:     config.InstallPaths{Mode: config.ModeProject, ProjectRoot: project, RulesDir: filepath.Join(project, ".kander", "rules")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeRulesFile(t, filepath.Join(tc.paths.RulesDir, "KANDER-AGENTS.md"), "# Kander entry\n")
			writeRulesFile(t, tc.target, tc.reference)
			if ok, detail := rulesIntegration("claude", tc.paths); !ok || detail != tc.target {
				t.Fatalf("relative import rejected: %v %s", ok, detail)
			}
			for _, inactive := range []string{
				"<!--\n" + tc.reference + "-->\n",
				"```text\n" + tc.reference + "```\n",
				"不要使用以下规则\n" + tc.reference,
			} {
				writeRulesFile(t, tc.target, inactive)
				if ok, detail := rulesIntegration("claude", tc.paths); ok {
					t.Errorf("inactive import accepted: %q %s", inactive, detail)
				}
			}
		})
	}
}

func TestRulesIntegrationRejectsMissingProjectRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ok, detail := rulesIntegration("claude", config.InstallPaths{Mode: config.ModeProject})
	if ok || detail != config.Text("config.project_install_paths_are_missing_the_main_worktree") {
		t.Fatalf("missing project root: integrated=%v detail=%s", ok, detail)
	}
}
