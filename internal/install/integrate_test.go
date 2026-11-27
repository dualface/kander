package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
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
	paths := config.InstallPaths{RulesDir: filepath.Join(home, ".agents", "kander")}
	writeIntegrateFile(t, RulesEntry(paths), "# Kander entry\n")
	return paths
}

// projectFixture is a temporary home with the global rules entry and a project root whose
// rules files kander init writes.
func projectFixture(t *testing.T) (ProjectRules, string) {
	t.Helper()
	home := setupInstallHome(t)
	paths := globalIntegrationPaths(t, home)
	project, err := NewProjectRules(t.TempDir(), paths)
	if err != nil {
		t.Fatal(err)
	}
	return project, home
}

const markdownProjectBlock = "## Kander Rules Entry\n\nAt the start of every session, read `~/.agents/kander/KANDER-AGENTS.md` and follow it as the Kander workflow rules entry; when that file does not exist, skip this section.\n"

func TestProjectReferenceBytesForEveryAgent(t *testing.T) {
	for _, agent := range ProjectReferenceAgents() {
		for _, existing := range []bool{false, true} {
			name := agent + "/create"
			if existing {
				name = agent + "/append"
			}
			t.Run(name, func(t *testing.T) {
				project, _ := projectFixture(t)
				target := filepath.Join(project.Root, "AGENTS.md")
				want := markdownProjectBlock
				if agent == "claude" {
					target = filepath.Join(project.Root, "CLAUDE.md")
					want = "@~/.agents/kander/KANDER-AGENTS.md\n"
				}
				status := IntegrationCreated
				if existing {
					writeIntegrateFile(t, target, "# Personal rules\n")
					want = "# Personal rules\n\n" + want
					status = IntegrationUpdated
				}
				for attempt := 0; attempt < 3; attempt++ {
					got, err := EnsureProjectReference(agent, project, config.InstallRulesOf(nil))
					if err != nil || got.Target != target || got.Status != status {
						t.Fatalf("attempt %d: %+v, %v; target=%s status=%v", attempt, got, err, target, status)
					}
					data, err := os.ReadFile(target)
					if err != nil || string(data) != want {
						t.Fatalf("attempt %d: bytes=%q want=%q err=%v", attempt, data, want, err)
					}
					if ok, detail := ProjectReferenceStatus(agent, project); !ok || detail != target {
						t.Fatalf("reference not recognized: %v %s", ok, detail)
					}
					status = IntegrationPresent
				}
			})
		}
	}
}

func TestEnsureProjectReferencesWritesEachFileOnce(t *testing.T) {
	project, home := projectFixture(t)
	results := EnsureProjectReferences(project, config.InstallRulesOf(nil))
	if len(results) != 2 || results[0].Agent != "claude" {
		t.Fatalf("results=%+v", results)
	}
	for _, item := range results {
		if item.Err != nil || item.Status != IntegrationCreated {
			t.Fatalf("item=%+v err=%v", item.IntegrationOutcome, item.Err)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(project.Root, "AGENTS.md")); string(got) != markdownProjectBlock {
		t.Fatalf("AGENTS.md=%q", got)
	}
	// The project reference never touches a global agent rules file.
	for _, dir := range []string{".claude", ".codex", ".cursor"} {
		if _, err := os.Lstat(filepath.Join(home, dir)); !os.IsNotExist(err) {
			t.Fatalf("%s written: %v", dir, err)
		}
	}
	for _, item := range EnsureProjectReferences(project, config.InstallRulesOf(nil)) {
		if item.Err != nil || item.Status != IntegrationPresent {
			t.Fatalf("rerun item=%+v err=%v", item.IntegrationOutcome, item.Err)
		}
	}
}

func TestEnsureProjectReferenceReplacesTheProjectInstallReference(t *testing.T) {
	project, _ := projectFixture(t)
	claude := filepath.Join(project.Root, "CLAUDE.md")
	agents := filepath.Join(project.Root, "AGENTS.md")
	writeIntegrateFile(t, claude, "# Mine\n\n@.kander/rules/KANDER-AGENTS.md\n")
	writeIntegrateFile(t, agents, "# Conventions\n\n## Kander Rules Entry\n\nAt the start of every session, read `.kander/rules/KANDER-AGENTS.md` and follow it as the Kander workflow rules entry.\n")
	// The retired entry still exists on disk: the reference is replaced all the same.
	writeIntegrateFile(t, filepath.Join(project.Root, ".kander", "rules", "KANDER-AGENTS.md"), "# old\n")
	for _, agent := range []string{"claude", "codex"} {
		outcome, err := EnsureProjectReference(agent, project, config.InstallRulesOf(nil))
		if err != nil || outcome.Status != IntegrationUpdated || outcome.Removed != 1 {
			t.Fatalf("%s: %+v %v", agent, outcome, err)
		}
	}
	if got, _ := os.ReadFile(claude); string(got) != "# Mine\n\n@~/.agents/kander/KANDER-AGENTS.md\n" {
		t.Fatalf("CLAUDE.md=%q", got)
	}
	got, _ := os.ReadFile(agents)
	if string(got) != "# Conventions\n\n"+markdownProjectBlock || HasLegacyProjectReference(string(got)) {
		t.Fatalf("AGENTS.md=%q", got)
	}
}

func TestEnsureProjectReferenceNegationWordsDoNotRepeatAppend(t *testing.T) {
	project, _ := projectFixture(t)
	for agent, target := range map[string]string{
		"claude": filepath.Join(project.Root, "CLAUDE.md"),
		"codex":  filepath.Join(project.Root, "AGENTS.md"),
	} {
		writeIntegrateFile(t, target, "# Notes\n\nThe old API is deprecated; ignore lint warnings there.\n")
		if outcome, err := EnsureProjectReference(agent, project, config.InstallRulesOf(nil)); err != nil || outcome.Status != IntegrationUpdated {
			t.Fatalf("%s first run: %+v %v", agent, outcome, err)
		}
		for i := 0; i < 2; i++ {
			if outcome, err := EnsureProjectReference(agent, project, config.InstallRulesOf(nil)); err != nil || outcome.Status != IntegrationPresent {
				t.Fatalf("%s rerun %d: %+v %v", agent, i, outcome, err)
			}
		}
		got, _ := os.ReadFile(target)
		if strings.Count(string(got), "KANDER-AGENTS.md") != 1 {
			t.Fatalf("%s reference repeated: %q", agent, got)
		}
	}
}

func TestEnsureProjectReferenceSurvivesMissingEntry(t *testing.T) {
	project, _ := projectFixture(t)
	if err := os.Remove(RulesEntry(project.Paths)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		outcome, err := EnsureProjectReference("claude", project, config.InstallRulesOf(nil))
		if err != nil || (i == 0) != (outcome.Status == IntegrationCreated) {
			t.Fatalf("run %d: %+v %v", i, outcome, err)
		}
	}
	got, _ := os.ReadFile(filepath.Join(project.Root, "CLAUDE.md"))
	if strings.Count(string(got), "KANDER-AGENTS.md") != 1 {
		t.Fatalf("reference repeated: %q", got)
	}
}

func TestEnsureProjectReferenceSymlinks(t *testing.T) {
	project, _ := projectFixture(t)
	outside := filepath.Join(t.TempDir(), "victim.md")
	writeIntegrateFile(t, outside, "precious\n")
	agents := filepath.Join(project.Root, "AGENTS.md")
	if err := os.Symlink(outside, agents); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := EnsureProjectReference("codex", project, config.InstallRulesOf(nil)); err == nil {
		t.Fatal("escaping symlink must be refused")
	}
	if got, _ := os.ReadFile(outside); string(got) != "precious\n" {
		t.Fatalf("outside file mutated: %q", got)
	}
	if ok, detail := ProjectReferenceStatus("codex", project); ok || !strings.Contains(detail, agents) {
		t.Fatalf("escaping link reported: %v %s", ok, detail)
	}

	if err := os.Remove(agents); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(project.Root, "docs", "shared-agents.md")
	writeIntegrateFile(t, inside, "# Shared\n")
	if err := os.Symlink(inside, agents); err != nil {
		t.Fatal(err)
	}
	if outcome, err := EnsureProjectReference("codex", project, config.InstallRulesOf(nil)); err != nil || outcome.Status != IntegrationUpdated {
		t.Fatalf("inside symlink: %+v %v", outcome, err)
	}
	if outcome, err := EnsureProjectReference("codex", project, config.InstallRulesOf(nil)); err != nil || outcome.Status != IntegrationPresent {
		t.Fatalf("inside symlink rerun: %+v %v", outcome, err)
	}
	got, _ := os.ReadFile(inside)
	if strings.Count(string(got), "KANDER-AGENTS.md") != 1 || !strings.HasPrefix(string(got), "# Shared\n") {
		t.Fatalf("inside content=%q", got)
	}
}

// CLAUDE.md -> AGENTS.md inside the main worktree keeps the ProjectSymlinkMayDiffer semantics:
// the import is written into the shared file once, and every agent reading it is satisfied.
func TestEnsureProjectReferencesClaudeLinkedToAgents(t *testing.T) {
	project, _ := projectFixture(t)
	agents := filepath.Join(project.Root, "AGENTS.md")
	writeIntegrateFile(t, agents, "# Shared\n")
	if err := os.Symlink("AGENTS.md", filepath.Join(project.Root, "CLAUDE.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	for _, item := range EnsureProjectReferences(project, config.InstallRulesOf(nil)) {
		if item.Err != nil {
			t.Fatalf("item=%+v err=%v", item.IntegrationOutcome, item.Err)
		}
	}
	got, _ := os.ReadFile(agents)
	if strings.Count(string(got), "KANDER-AGENTS.md") != 1 {
		t.Fatalf("AGENTS.md=%q", got)
	}
	for _, agent := range []string{"claude", "codex"} {
		if ok, detail := ProjectReferenceStatus(agent, project); !ok {
			t.Fatalf("%s not referenced: %s", agent, detail)
		}
	}
}

func TestEnsureProjectReferencesHonorsSharedFileSwitches(t *testing.T) {
	project, _ := projectFixture(t)
	cfg := config.DefaultConfig()
	off := false
	config.SetAgentInstallRules(cfg, "pi", &off)
	results := EnsureProjectReferences(project, config.InstallRulesOf(cfg))
	for _, item := range results {
		switch item.Agent {
		case "claude":
			if item.Status != IntegrationCreated {
				t.Fatalf("claude=%+v", item.IntegrationOutcome)
			}
		default:
			if item.Status != IntegrationDisabled || len(item.DisabledBy) != 1 || item.DisabledBy[0] != "pi" {
				t.Fatalf("shared AGENTS.md=%+v", item.IntegrationOutcome)
			}
		}
	}
	if _, err := os.Lstat(filepath.Join(project.Root, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("AGENTS.md written while a reader is off: %v", err)
	}
}

func TestInspectProjectReferencesReportsLegacyAndMissing(t *testing.T) {
	project, _ := projectFixture(t)
	writeIntegrateFile(t, filepath.Join(project.Root, "AGENTS.md"), "@.kander/rules/KANDER-AGENTS.md\n")
	states := InspectProjectReferences(project, config.InstallRulesOf(nil))
	if len(states) != 2 {
		t.Fatalf("states=%+v", states)
	}
	claude, agents := states[0], states[1]
	if claude.Present || claude.Legacy || len(claude.Agents) != 1 {
		t.Fatalf("claude=%+v", claude)
	}
	if agents.Present || !agents.Legacy || len(agents.Agents) < 2 {
		t.Fatalf("AGENTS.md=%+v", agents)
	}
	for _, path := range []string{filepath.Join(project.Root, "CLAUDE.md")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("inspection wrote %s", path)
		}
	}
}
