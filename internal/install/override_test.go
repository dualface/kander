package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

// codexOverrideSetup prepares a project and returns its AGENTS.md target together with the
// AGENTS.override.md sibling path.
func codexOverrideSetup(t *testing.T) (ProjectRules, string, string) {
	t.Helper()
	project, _ := projectFixture(t)
	target := filepath.Join(project.Root, "AGENTS.md")
	override := filepath.Join(project.Root, "AGENTS.override.md")
	return project, target, override
}

var allSwitches = config.InstallRulesOf(nil)

func TestEnsureProjectReferenceWritesExistingOverride(t *testing.T) {
	project, target, override := codexOverrideSetup(t)
	writeIntegrateFile(t, target, "# Personal rules\n")
	writeIntegrateFile(t, override, "# Override rules\n")

	outcome, err := EnsureProjectReference("codex", project, allSwitches)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != IntegrationUpdated || outcome.Target != target {
		t.Fatalf("main outcome: %+v", outcome)
	}
	if outcome.Override == nil {
		t.Fatal("override outcome missing")
	}
	if outcome.Override.Target != override || outcome.Override.Status != IntegrationUpdated {
		t.Fatalf("override outcome: %+v", outcome.Override)
	}
	spelling := ProjectEntrySpelling
	for _, file := range []string{target, override} {
		data, err := os.ReadFile(file)
		if err != nil || !strings.Contains(string(data), spelling) {
			t.Fatalf("%s lacks the entry reference: %q err=%v", file, data, err)
		}
	}

	again, err := EnsureProjectReference("codex", project, allSwitches)
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != IntegrationPresent {
		t.Fatalf("second run on main: %+v", again)
	}
	if again.Override == nil || again.Override.Status != IntegrationPresent {
		t.Fatalf("second run on override: %+v", again.Override)
	}
}

func TestEnsureProjectReferenceNeverCreatesOverride(t *testing.T) {
	project, target, override := codexOverrideSetup(t)
	writeIntegrateFile(t, target, "# Personal rules\n")

	outcome, err := EnsureProjectReference("codex", project, allSwitches)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Override != nil {
		t.Fatalf("absent override produced an outcome: %+v", outcome.Override)
	}
	if _, err := os.Lstat(override); !os.IsNotExist(err) {
		t.Fatalf("override was created: %v", err)
	}
}

func TestEnsureProjectReferenceOverrideWithoutMainFile(t *testing.T) {
	project, _, override := codexOverrideSetup(t)
	writeIntegrateFile(t, override, "# Override rules\n")

	outcome, err := EnsureProjectReference("codex", project, allSwitches)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != IntegrationCreated {
		t.Fatalf("main outcome: %+v", outcome)
	}
	if outcome.Override == nil || outcome.Override.Status != IntegrationUpdated {
		t.Fatalf("override outcome: %+v", outcome.Override)
	}
	data, _ := os.ReadFile(override)
	if !strings.Contains(string(data), ProjectEntrySpelling) {
		t.Fatalf("override lacks the entry reference: %q", data)
	}
}

func TestEnsureProjectReferenceCleansOverride(t *testing.T) {
	project, target, override := codexOverrideSetup(t)
	entry := RulesEntry(project.Paths)
	writeIntegrateFile(t, target, referenceBlock("codex"))
	writeIntegrateFile(t, override,
		"# Override rules\n"+
			"@/dead/KANDER-AGENTS.md\n"+
			"@"+entry+"\n"+
			"@"+entry+"\n")

	outcome, err := EnsureProjectReference("codex", project, allSwitches)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != IntegrationPresent {
		t.Fatalf("main outcome: %+v", outcome)
	}
	if outcome.Override == nil {
		t.Fatal("override outcome missing")
	}
	if outcome.Override.Status != IntegrationCleaned {
		t.Fatalf("override outcome: %+v", outcome.Override)
	}
	if outcome.Override.Removed != 2 {
		t.Fatalf("override removed %d, want 2 (invalid + duplicate)", outcome.Override.Removed)
	}
	data, _ := os.ReadFile(override)
	text := string(data)
	if strings.Contains(text, "/dead/") || strings.Count(text, entry) != 1 {
		t.Fatalf("stale or duplicate references survived: %q", text)
	}
}

func TestProjectReferenceStatusRequiresBothFiles(t *testing.T) {
	project, target, override := codexOverrideSetup(t)
	writeIntegrateFile(t, target, referenceBlock("codex"))
	writeIntegrateFile(t, override, "# Override rules\n")

	ok, detail := ProjectReferenceStatus("codex", project)
	if ok {
		t.Fatal("override without a reference must report not integrated")
	}
	if !strings.Contains(detail, override) {
		t.Fatalf("detail must name the override file, got %q", detail)
	}

	writeIntegrateFile(t, override, "# Override rules\n\n"+referenceBlock("codex"))
	ok, detail = ProjectReferenceStatus("codex", project)
	if !ok {
		t.Fatalf("both files referencing must report integrated, got %q", detail)
	}
	if detail != override {
		t.Fatalf("integrated detail must name the file the agent reads, got %q", detail)
	}
}

func TestClaudeTargetIgnoresOverrideSibling(t *testing.T) {
	project, _ := projectFixture(t)
	override := filepath.Join(project.Root, "AGENTS.override.md")
	writeIntegrateFile(t, override, "# Override rules\n")

	outcome, err := EnsureProjectReference("claude", project, allSwitches)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Override != nil {
		t.Fatalf("claude target produced an override outcome: %+v", outcome.Override)
	}
	data, _ := os.ReadFile(override)
	if string(data) != "# Override rules\n" {
		t.Fatalf("claude run touched the override: %q", data)
	}
	ok, _ := ProjectReferenceStatus("claude", project)
	if !ok {
		t.Fatal("claude integration must ignore the override sibling")
	}
}

func TestEnsureProjectReferenceProjectOverride(t *testing.T) {
	project, _ := projectFixture(t)
	override := filepath.Join(project.Root, "AGENTS.override.md")
	writeIntegrateFile(t, override, "# Override rules\n")

	outcome, err := EnsureProjectReference("pi", project, allSwitches)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != IntegrationCreated {
		t.Fatalf("main outcome: %+v", outcome)
	}
	if outcome.Override == nil || outcome.Override.Status != IntegrationUpdated {
		t.Fatalf("override outcome: %+v", outcome.Override)
	}
	data, _ := os.ReadFile(override)
	if !strings.Contains(string(data), ProjectEntrySpelling) {
		t.Fatalf("override lacks the project reference: %q", data)
	}
}

func TestEnsureProjectReferenceProjectOverrideSymlinkEscape(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("symlink creation needs privileges on Windows")
	}
	project, _ := projectFixture(t)
	override := filepath.Join(project.Root, "AGENTS.override.md")
	outside := filepath.Join(t.TempDir(), "outside.md")
	writeIntegrateFile(t, outside, "# foreign\n")
	if err := os.Symlink(outside, override); err != nil {
		t.Fatal(err)
	}

	if _, err := EnsureProjectReference("pi", project, allSwitches); err == nil {
		t.Fatal("an override symlink escaping the project must fail")
	}
	data, _ := os.ReadFile(outside)
	if string(data) != "# foreign\n" {
		t.Fatalf("the escaped file was rewritten: %q", data)
	}
}
