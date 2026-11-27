package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

func TestCleanRulesReferencesRemovesDanglingImport(t *testing.T) {
	project, _ := projectFixture(t)
	target := filepath.Join(project.Root, "CLAUDE.md")
	writeIntegrateFile(t, target,
		"# Notes\n\n@/nonexistent/older/KANDER-AGENTS.md\n\n@docs/other.md\n")
	outcome, err := EnsureProjectReference("claude", project, config.InstallRulesOf(nil))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if strings.Contains(text, "/nonexistent/") {
		t.Fatalf("dangling reference kept: %q", text)
	}
	if !strings.Contains(text, "@docs/other.md") {
		t.Fatalf("foreign import removed: %q", text)
	}
	if outcome.Status != IntegrationUpdated || outcome.Removed != 1 {
		t.Fatalf("outcome=%+v", outcome)
	}
	if ok, detail := ProjectReferenceStatus("claude", project); !ok {
		t.Fatalf("not integrated after cleanup: %s", detail)
	}
	if strings.Count(text, "KANDER-AGENTS.md") != 1 {
		t.Fatalf("expected exactly one load command: %q", text)
	}
}

func TestCleanRulesReferencesDeduplicatesSpellings(t *testing.T) {
	project, home := projectFixture(t)
	paths := project.Paths
	entry := RulesEntry(paths)
	target := filepath.Join(project.Root, "CLAUDE.md")
	tilde := "~/" + filepath.ToSlash(mustRel(t, home, entry))
	writeIntegrateFile(t, target, "lead\n@"+tilde+"\nmid\n@"+entry+"\ntail\n")
	outcome, err := EnsureProjectReference("claude", project, config.InstallRulesOf(nil))
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != IntegrationCleaned || outcome.Removed != 1 {
		t.Fatalf("outcome=%+v", outcome)
	}
	got, _ := os.ReadFile(target)
	text := string(got)
	if strings.Count(text, "KANDER-AGENTS.md") != 1 {
		t.Fatalf("dedupe failed: %q", text)
	}
	if !strings.Contains(text, "@"+tilde) {
		t.Fatalf("first reference not kept: %q", text)
	}
}

func TestCleanRulesReferencesDeduplicatesMixedForms(t *testing.T) {
	project, _ := projectFixture(t)
	paths := project.Paths
	entry := RulesEntry(paths)
	target := filepath.Join(project.Root, "CLAUDE.md")
	writeIntegrateFile(t, target, "@"+entry+"\n\n"+entry+"\n")
	if _, err := EnsureProjectReference("claude", project, config.InstallRulesOf(nil)); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	text := string(got)
	if strings.Count(text, "KANDER-AGENTS.md") != 1 {
		t.Fatalf("mixed dedupe failed: %q", text)
	}
	if !strings.Contains(text, "@"+entry) {
		t.Fatalf("first form not kept: %q", text)
	}
}

func TestCleanRulesReferencesDeduplicatesMarkdownBlock(t *testing.T) {
	project, _ := projectFixture(t)
	paths := project.Paths
	entry := RulesEntry(paths)
	target := filepath.Join(project.Root, "AGENTS.md")
	block := "## Kander Rules Entry\n\nAt the start of every session, read `" + entry +
		"` and follow it as the Kander workflow rules entry.\n"
	writeIntegrateFile(t, target, "# Own rules\n\n"+block+"\n"+block+"\n# Other\n")
	outcome, err := EnsureProjectReference("codex", project, config.InstallRulesOf(nil))
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Removed == 0 {
		t.Fatalf("duplicate block kept: %+v", outcome)
	}
	got, _ := os.ReadFile(target)
	text := string(got)
	if strings.Count(text, "Kander Rules Entry") != 1 || strings.Count(text, "KANDER-AGENTS.md") != 1 {
		t.Fatalf("block dedupe failed: %q", text)
	}
	if !strings.Contains(text, "# Other") {
		t.Fatalf("following section damaged: %q", text)
	}
}

func TestCleanRulesReferencesRemovesInvalidMarkdownBlock(t *testing.T) {
	project, _ := projectFixture(t)
	target := filepath.Join(project.Root, "AGENTS.md")
	block := "## Kander Rules Entry\n\nAt the start of every session, read `/third/party/KANDER-AGENTS.md`" +
		" and follow it as the Kander workflow rules entry.\n"
	writeIntegrateFile(t, target, block+"\n# Keep me\n\nbody\n")
	if _, err := EnsureProjectReference("codex", project, config.InstallRulesOf(nil)); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	text := string(got)
	if strings.Contains(text, "/third/party/") || strings.Count(text, "Kander Rules Entry") != 1 {
		t.Fatalf("invalid block survived: %q", text)
	}
	if !strings.Contains(text, "# Keep me\n\nbody") {
		t.Fatalf("user section damaged: %q", text)
	}
}

func TestCleanRulesReferencesKeepsProseCommentsAndFences(t *testing.T) {
	project, _ := projectFixture(t)
	paths := project.Paths
	entry := RulesEntry(paths)
	target := filepath.Join(project.Root, "AGENTS.md")
	writeIntegrateFile(t, target,
		"# Doc\n\nSee `"+entry+"` for details.\n\n<!-- @/dead/KANDER-AGENTS.md -->\n\n```text\n/dead/KANDER-AGENTS.md\n```\n")
	if _, err := EnsureProjectReference("codex", project, config.InstallRulesOf(nil)); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	text := string(got)
	for _, want := range []string{"See `" + entry + "`", "<!-- @/dead/KANDER-AGENTS.md -->", "```text\n/dead/KANDER-AGENTS.md\n```"} {
		if !strings.Contains(text, want) {
			t.Fatalf("protected text removed: %q missing %q", text, want)
		}
	}
}

func TestCleanRulesReferencesKeepsBareFilenameMention(t *testing.T) {
	project, _ := projectFixture(t)
	target := filepath.Join(project.Root, "AGENTS.md")
	writeIntegrateFile(t, target, "# Doc\n\nKANDER-AGENTS.md\n")
	outcome, err := EnsureProjectReference("codex", project, config.InstallRulesOf(nil))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	if !strings.Contains(string(got), "\nKANDER-AGENTS.md\n") {
		t.Fatalf("bare filename mention removed: %q", got)
	}
	if outcome.Removed != 0 {
		t.Fatalf("removed=%d", outcome.Removed)
	}
}

func TestCleanRulesReferencesNegatedReferenceKeepsFirstSlot(t *testing.T) {
	project, _ := projectFixture(t)
	paths := project.Paths
	entry := RulesEntry(paths)
	target := filepath.Join(project.Root, "CLAUDE.md")
	// A negated but resolving reference is still a load command: it keeps the first slot,
	// so a later plain reference is the duplicate that gets dropped. The strict append
	// gate then leaves the file as-is, matching the pre-cleanup semantics.
	writeIntegrateFile(t, target, "## Disabled\n\n@"+entry+"\n\n@"+entry+"\n")
	outcome, err := EnsureProjectReference("claude", project, config.InstallRulesOf(nil))
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != IntegrationCleaned || outcome.Removed != 1 {
		t.Fatalf("outcome=%+v", outcome)
	}
	got, _ := os.ReadFile(target)
	text := string(got)
	if strings.Count(text, "KANDER-AGENTS.md") != 1 || !strings.Contains(text, "## Disabled\n\n@"+entry) {
		t.Fatalf("first reference not kept under its section: %q", text)
	}
}

func TestCleanRulesReferencesIdempotent(t *testing.T) {
	project, _ := projectFixture(t)
	paths := project.Paths
	entry := RulesEntry(paths)
	target := filepath.Join(project.Root, "CLAUDE.md")
	writeIntegrateFile(t, target, "@/dead/KANDER-AGENTS.md\n@"+entry+"\n@"+entry+"\n")
	first, err := EnsureProjectReference("claude", project, config.InstallRulesOf(nil))
	if err != nil || first.Removed == 0 {
		t.Fatalf("first: %+v %v", first, err)
	}
	before, _ := os.ReadFile(target)
	second, err := EnsureProjectReference("claude", project, config.InstallRulesOf(nil))
	if err != nil || second.Status != IntegrationPresent || second.Removed != 0 {
		t.Fatalf("second: %+v %v", second, err)
	}
	after, _ := os.ReadFile(target)
	if string(before) != string(after) {
		t.Fatalf("second run rewrote: %q -> %q", before, after)
	}
}

func TestCleanRulesReferencesCountsInvalidAndDuplicates(t *testing.T) {
	project, _ := projectFixture(t)
	entry := RulesEntry(project.Paths)
	text := "@/dead/KANDER-AGENTS.md\n@" + entry + "\n@" + entry + "\n@.kander/rules/KANDER-AGENTS.md\n"
	cleaned, removed := CleanRulesReferences(text, entry, project.Root)
	if removed != 3 || cleaned != "@"+entry+"\n" {
		t.Fatalf("removed=%d cleaned=%q", removed, cleaned)
	}
}

func mustRel(t *testing.T, base, path string) string {
	t.Helper()
	rel, err := filepath.Rel(base, path)
	if err != nil {
		t.Fatal(err)
	}
	return rel
}
