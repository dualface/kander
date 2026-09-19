package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

// codexOverrideSetup prepares a global scope where codex is a covered agent and returns the
// AGENTS.md target together with its AGENTS.override.md sibling path.
func codexOverrideSetup(t *testing.T) (config.InstallPaths, string, string) {
	t.Helper()
	home := setupInstallHome(t)
	paths := globalIntegrationPaths(t, home)
	target := filepath.Join(home, ".codex", "AGENTS.md")
	override := filepath.Join(home, ".codex", "AGENTS.override.md")
	return paths, target, override
}

func TestEnsureRulesIntegrationWritesExistingOverride(t *testing.T) {
	paths, target, override := codexOverrideSetup(t)
	writeIntegrateFile(t, target, "# Personal rules\n")
	writeIntegrateFile(t, override, "# Override rules\n")

	outcome, err := EnsureRulesIntegration("codex", paths)
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
	spelling := entrySpelling(paths)
	for _, file := range []string{target, override} {
		data, err := os.ReadFile(file)
		if err != nil || !strings.Contains(string(data), spelling) {
			t.Fatalf("%s lacks the entry reference: %q err=%v", file, data, err)
		}
	}

	again, err := EnsureRulesIntegration("codex", paths)
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

func TestEnsureRulesIntegrationNeverCreatesOverride(t *testing.T) {
	paths, target, override := codexOverrideSetup(t)
	writeIntegrateFile(t, target, "# Personal rules\n")

	outcome, err := EnsureRulesIntegration("codex", paths)
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

func TestEnsureRulesIntegrationOverrideWithoutMainFile(t *testing.T) {
	paths, _, override := codexOverrideSetup(t)
	writeIntegrateFile(t, override, "# Override rules\n")

	outcome, err := EnsureRulesIntegration("codex", paths)
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
	if !strings.Contains(string(data), entrySpelling(paths)) {
		t.Fatalf("override lacks the entry reference: %q", data)
	}
}

func TestEnsureRulesIntegrationCleansOverride(t *testing.T) {
	paths, target, override := codexOverrideSetup(t)
	entry := RulesEntry(paths)
	writeIntegrateFile(t, target, referenceBlock("codex", paths))
	legacy := filepath.Join(filepath.Dir(paths.RulesDir), "KANDER-AGENTS.md")
	writeIntegrateFile(t, override,
		"# Override rules\n"+
			"see `"+legacy+"` for the old entry\n"+
			"@/dead/KANDER-AGENTS.md\n"+
			"@"+entry+"\n"+
			"@"+entry+"\n")

	outcome, err := EnsureRulesIntegration("codex", paths)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != IntegrationPresent {
		t.Fatalf("main outcome: %+v", outcome)
	}
	if outcome.Override == nil {
		t.Fatal("override outcome missing")
	}
	if outcome.Override.Status != IntegrationRewritten && outcome.Override.Status != IntegrationCleaned {
		t.Fatalf("override outcome: %+v", outcome.Override)
	}
	if outcome.Override.Removed != 2 {
		t.Fatalf("override removed %d, want 2 (invalid + duplicate)", outcome.Override.Removed)
	}
	data, _ := os.ReadFile(override)
	text := string(data)
	if strings.Contains(text, "/dead/") || strings.Contains(text, ".agents/KANDER-AGENTS.md") {
		t.Fatalf("stale references survived: %q", text)
	}
	// The rewritten legacy line and the surviving load command both name the current entry.
	if strings.Count(text, entry) != 2 {
		t.Fatalf("override does not hold the rewritten and surviving references: %q", text)
	}
}

func TestRulesIntegrationRequiresBothFiles(t *testing.T) {
	paths, target, override := codexOverrideSetup(t)
	writeIntegrateFile(t, target, referenceBlock("codex", paths))
	writeIntegrateFile(t, override, "# Override rules\n")

	ok, detail := RulesIntegration("codex", paths)
	if ok {
		t.Fatal("override without a reference must report not integrated")
	}
	if !strings.Contains(detail, override) {
		t.Fatalf("detail must name the override file, got %q", detail)
	}

	writeIntegrateFile(t, override, "# Override rules\n\n"+referenceBlock("codex", paths))
	ok, detail = RulesIntegration("codex", paths)
	if !ok {
		t.Fatalf("both files referencing must report integrated, got %q", detail)
	}
	if detail != override {
		t.Fatalf("integrated detail must name the file the agent reads, got %q", detail)
	}
}

func TestInspectRulesReferencesCoversOverride(t *testing.T) {
	paths, target, override := codexOverrideSetup(t)
	entry := RulesEntry(paths)
	writeIntegrateFile(t, target, referenceBlock("codex", paths))
	writeIntegrateFile(t, override, "@/dead/KANDER-AGENTS.md\n@"+entry+"\n@"+entry+"\n")

	issues, err := InspectRulesReferences("codex", paths)
	if err != nil {
		t.Fatal(err)
	}
	if issues.Invalid != 1 || issues.Duplicates != 1 {
		t.Fatalf("aggregate counts: %+v", issues)
	}
	if len(issues.Files) != 2 {
		t.Fatalf("files inspected: %+v", issues.Files)
	}
	if issues.Files[1].Target != override || issues.Files[1].Invalid != 1 || issues.Files[1].Duplicates != 1 {
		t.Fatalf("override counts: %+v", issues.Files[1])
	}
	data, _ := os.ReadFile(override)
	if strings.Count(string(data), "KANDER-AGENTS.md") != 3 {
		t.Fatalf("inspect wrote the override: %q", data)
	}
}

func TestClaudeTargetIgnoresOverrideSibling(t *testing.T) {
	home := setupInstallHome(t)
	paths := globalIntegrationPaths(t, home)
	override := filepath.Join(home, ".claude", "AGENTS.override.md")
	writeIntegrateFile(t, override, "# Override rules\n")

	outcome, err := EnsureRulesIntegration("claude", paths)
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
	ok, _ := RulesIntegration("claude", paths)
	if !ok {
		t.Fatal("claude integration must ignore the override sibling")
	}
}

func TestEnsureRulesIntegrationProjectOverride(t *testing.T) {
	setupInstallHome(t)
	project := t.TempDir()
	paths := config.InstallPaths{Mode: config.ModeProject, ProjectRoot: project, RulesDir: filepath.Join(project, ".kander", "rules")}
	writeIntegrateFile(t, RulesEntry(paths), "# Kander entry\n")
	override := filepath.Join(project, "AGENTS.override.md")
	writeIntegrateFile(t, override, "# Override rules\n")

	outcome, err := EnsureRulesIntegration("pi", paths)
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
	if !strings.Contains(string(data), ".kander/rules/KANDER-AGENTS.md") {
		t.Fatalf("override lacks the project entry reference: %q", data)
	}
}

func TestEnsureRulesIntegrationProjectOverrideSymlinkEscape(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("symlink creation needs privileges on Windows")
	}
	setupInstallHome(t)
	project := t.TempDir()
	paths := config.InstallPaths{Mode: config.ModeProject, ProjectRoot: project, RulesDir: filepath.Join(project, ".kander", "rules")}
	writeIntegrateFile(t, RulesEntry(paths), "# Kander entry\n")
	override := filepath.Join(project, "AGENTS.override.md")
	outside := filepath.Join(t.TempDir(), "outside.md")
	writeIntegrateFile(t, outside, "# foreign\n")
	if err := os.Symlink(outside, override); err != nil {
		t.Fatal(err)
	}

	if _, err := EnsureRulesIntegration("pi", paths); err == nil {
		t.Fatal("an override symlink escaping the project must fail")
	}
	data, _ := os.ReadFile(outside)
	if string(data) != "# foreign\n" {
		t.Fatalf("the escaped file was rewritten: %q", data)
	}
}
