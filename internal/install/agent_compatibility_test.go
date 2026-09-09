package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/config"
)

// Expectations are pinned to integrate.go at 8abdfe2e, before embedded definitions.
func TestBuiltinIntegrationPreservesTargetsAndBytes(t *testing.T) {
	for _, mode := range []config.Mode{config.ModeGlobal, config.ModeProject} {
		for _, agent := range []string{"codex", "claude", "grok", "cursor"} {
			for _, existing := range []bool{false, true} {
				name := string(mode) + "/" + agent + "/create"
				if existing {
					name = string(mode) + "/" + agent + "/append"
				}
				t.Run(name, func(t *testing.T) {
					home := setupInstallHome(t)
					paths := globalIntegrationPaths(t, home)
					spelling := "~/.agents/KANDER-AGENTS.md"
					target := filepath.Join(home, "."+agent, "AGENTS.md")
					if agent == "claude" {
						target = filepath.Join(home, ".claude", "CLAUDE.md")
					}
					if mode == config.ModeProject {
						project := t.TempDir()
						paths = config.InstallPaths{Mode: mode, ProjectRoot: project, RulesDir: filepath.Join(project, ".kander", "rules")}
						writeIntegrateFile(t, RulesEntry(paths), "# Kander entry\n")
						spelling = ".kander/rules/KANDER-AGENTS.md"
						target = filepath.Join(project, "AGENTS.md")
						if agent == "claude" {
							target = filepath.Join(project, "CLAUDE.md")
						}
					}
					want := "## Kander Rules Entry\n\nAt the start of every session, read `" + spelling + "` and follow it as the Kander workflow rules entry.\n"
					if agent == "claude" {
						want = "@" + spelling + "\n"
					}
					status := IntegrationCreated
					if existing {
						writeIntegrateFile(t, target, "# Personal rules\n")
						want = "# Personal rules\n\n" + want
						status = IntegrationUpdated
					}
					for attempt := 0; attempt < 3; attempt++ {
						got, err := EnsureRulesIntegration(agent, paths)
						if err != nil || got.Target != target || got.Status != status {
							t.Fatalf("attempt %d: %+v, %v; target=%s status=%v", attempt, got, err, target, status)
						}
						data, err := os.ReadFile(target)
						if err != nil || string(data) != want {
							t.Fatalf("attempt %d: bytes=%q want=%q err=%v", attempt, data, want, err)
						}
						if ok, detail := RulesIntegration(agent, paths); !ok || detail != target {
							t.Fatalf("reference not recognized: %v %s", ok, detail)
						}
						status = IntegrationPresent
					}
				})
			}
		}
	}
}
