package install

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/rules"
)

// writeLegacyRules places the official rule files at the flat ~/.agents root earlier
// releases used, together with the installer state file.
func writeLegacyRules(t *testing.T, home string) string {
	t.Helper()
	legacy := filepath.Join(home, ".agents")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range rules.Names() {
		data, err := rules.File(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(legacy, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(legacy, stateFileName), []byte("{\"files\":{}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return legacy
}

func TestPerformMovesOfficialLegacyRules(t *testing.T) {
	home := setupInstallHome(t)
	legacy := writeLegacyRules(t, home)
	result, err := Perform(Request{CopyBinary: true, Language: "cn", Source: stubBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.LegacyRules.Kept) != 0 {
		t.Fatalf("kept=%v", result.LegacyRules.Kept)
	}
	if len(result.LegacyRules.Removed) != len(rules.Names())+1 {
		t.Fatalf("removed=%v", result.LegacyRules.Removed)
	}
	for _, name := range append(rules.Names(), stateFileName) {
		if _, err := os.Lstat(filepath.Join(legacy, name)); !os.IsNotExist(err) {
			t.Fatalf("%s still at the previous location: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(legacy, "kander", name)); err != nil {
			t.Fatalf("%s missing at the current location: %v", name, err)
		}
	}
}

func TestPerformRemovesEditedLegacyRule(t *testing.T) {
	home := setupInstallHome(t)
	legacy := writeLegacyRules(t, home)
	edited := filepath.Join(legacy, "KANDER-CODE-RULES.md")
	if err := os.WriteFile(edited, []byte("# local policy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Perform(Request{CopyBinary: true, Language: "cn", Source: stubBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.LegacyRules.Kept) != 0 {
		t.Fatalf("kept=%v", result.LegacyRules.Kept)
	}
	if _, err := os.Lstat(edited); !os.IsNotExist(err) {
		t.Fatalf("edited legacy rule kept: %v", err)
	}
}

func TestPerformRemovesLegacyRuleSymlinkOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	home := setupInstallHome(t)
	legacy := writeLegacyRules(t, home)
	real := filepath.Join(t.TempDir(), "mine.md")
	if err := os.WriteFile(real, []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(legacy, "KANDER-GIT-RULES.md")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	result, err := Perform(Request{CopyBinary: true, Language: "cn", Source: stubBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.LegacyRules.Kept) != 0 {
		t.Fatalf("kept=%v", result.LegacyRules.Kept)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("symlink kept: %v", err)
	}
	if got, err := os.ReadFile(real); err != nil || string(got) != "# mine\n" {
		t.Fatalf("link target touched: %q %v", got, err)
	}
}

func TestPerformRewritesLegacyClaudeImport(t *testing.T) {
	home := setupInstallHome(t)
	writeLegacyRules(t, home)
	claude := filepath.Join(home, ".claude", "CLAUDE.md")
	writeIntegrateFile(t, claude, "# Personal\n\n@~/.agents/KANDER-AGENTS.md\n")
	result, err := Perform(Request{CopyBinary: true, Language: "cn", Source: stubBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Integrations) != 1 || result.Integrations[0].Status != IntegrationRewritten {
		t.Fatalf("integrations=%+v", result.Integrations)
	}
	got, err := os.ReadFile(claude)
	if err != nil || string(got) != "# Personal\n\n@~/.agents/kander/KANDER-AGENTS.md\n" {
		t.Fatalf("bytes=%q err=%v", got, err)
	}
	paths, err := config.GlobalInstallPaths()
	if err != nil {
		t.Fatal(err)
	}
	if ok, detail := RulesIntegration("claude", paths); !ok {
		t.Fatalf("not integrated after rewrite: %s", detail)
	}
	again, err := EnsureRulesIntegration("claude", paths)
	if err != nil || again.Status != IntegrationPresent {
		t.Fatalf("second pass=%+v err=%v", again, err)
	}
}

func TestPerformRewritesLegacyMarkdownReference(t *testing.T) {
	home := setupInstallHome(t)
	writeLegacyRules(t, home)
	agents := filepath.Join(home, ".codex", "AGENTS.md")
	block := "## Kander Rules Entry\n\nAt the start of every session, read `~/.agents/KANDER-AGENTS.md` and follow it as the Kander workflow rules entry.\n"
	writeIntegrateFile(t, agents, "# Mine\n\n"+block)
	if _, err := Perform(Request{CopyBinary: true, Language: "cn", Source: stubBinary(t)}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(agents)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(got), "KANDER-AGENTS.md") != 1 || !strings.Contains(string(got), "`~/.agents/kander/KANDER-AGENTS.md`") {
		t.Fatalf("bytes=%q", got)
	}
}

func TestPerformRewritesReferenceEvenWhenLegacyEntryWasEdited(t *testing.T) {
	home := setupInstallHome(t)
	legacy := writeLegacyRules(t, home)
	if err := os.WriteFile(filepath.Join(legacy, "KANDER-AGENTS.md"), []byte("# my entry\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	claude := filepath.Join(home, ".claude", "CLAUDE.md")
	writeIntegrateFile(t, claude, "@~/.agents/KANDER-AGENTS.md\n")
	result, err := Perform(Request{CopyBinary: true, Language: "cn", Source: stubBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(claude)
	if err != nil || string(got) != "@~/.agents/kander/KANDER-AGENTS.md\n" {
		t.Fatalf("bytes=%q err=%v", got, err)
	}
	if len(result.Integrations) != 1 || result.Integrations[0].Status != IntegrationRewritten {
		t.Fatalf("integrations=%+v", result.Integrations)
	}
}

func TestPerformReplacesLegacyEntrySymlinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	home := setupInstallHome(t)
	legacy := writeLegacyRules(t, home)
	claude := filepath.Join(home, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(claude), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(legacy, "KANDER-AGENTS.md"), claude); err != nil {
		t.Fatal(err)
	}
	result, err := Perform(Request{CopyBinary: true, Language: "cn", Source: stubBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.LegacyLinksRemoved) != 1 || result.LegacyLinksRemoved[0] != claude {
		t.Fatalf("links=%v", result.LegacyLinksRemoved)
	}
	info, err := os.Lstat(claude)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("still a symlink: %v", err)
	}
	got, err := os.ReadFile(claude)
	if err != nil || string(got) != "@~/.agents/kander/KANDER-AGENTS.md\n" {
		t.Fatalf("bytes=%q err=%v", got, err)
	}
}

func TestRewriteLegacyReferenceSkipsProjectScope(t *testing.T) {
	project := t.TempDir()
	paths := config.InstallPaths{Mode: config.ModeProject, ProjectRoot: project, RulesDir: filepath.Join(project, ".kander", "rules")}
	text := "@~/.agents/KANDER-AGENTS.md\n"
	if got, changed := rewriteLegacyReference(text, paths, project); changed || got != text {
		t.Fatalf("project scope rewrote: %q", got)
	}
}

func TestRepairRulesMigratesLegacyLayoutBeforeRewrite(t *testing.T) {
	home := setupInstallHome(t)
	legacy := writeLegacyRules(t, home)
	claude := filepath.Join(home, ".claude", "CLAUDE.md")
	writeIntegrateFile(t, claude, "@~/.agents/KANDER-AGENTS.md\n")
	paths, err := config.GlobalInstallPaths()
	if err != nil {
		t.Fatal(err)
	}
	// A binary upgraded without install: doctor repair runs first.
	migration, _, err := RepairRules(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(migration.Removed) == 0 {
		t.Fatalf("repair reported no migration: %+v", migration)
	}
	if _, err := os.Lstat(filepath.Join(legacy, "KANDER-AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("repair left the previous entry: %v", err)
	}
	outcome, err := EnsureRulesIntegration("claude", paths)
	if err != nil || outcome.Status != IntegrationRewritten {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	got, _ := os.ReadFile(claude)
	if string(got) != "@~/.agents/kander/KANDER-AGENTS.md\n" {
		t.Fatalf("bytes=%q", got)
	}
}

func TestRewriteRemovesStaleReferenceNextToCurrentOne(t *testing.T) {
	home := setupInstallHome(t)
	writeLegacyRules(t, home)
	claude := filepath.Join(home, ".claude", "CLAUDE.md")
	// Both references present: an earlier repair appended the current one while the old entry
	// still existed. After the migration the stale one must be rewritten, not kept.
	writeIntegrateFile(t, claude, "@~/.agents/KANDER-AGENTS.md\n\n@~/.agents/kander/KANDER-AGENTS.md\n")
	if _, err := Perform(Request{CopyBinary: true, Language: "cn", Source: stubBinary(t)}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(claude)
	if strings.Contains(string(got), "@~/.agents/KANDER-AGENTS.md\n") || strings.Count(string(got), "@~/.agents/kander/KANDER-AGENTS.md") != 1 {
		t.Fatalf("bytes=%q", got)
	}
}

func TestRewriteWaitsWhileDirectoryBlocksLegacyEntry(t *testing.T) {
	home := setupInstallHome(t)
	legacy := writeLegacyRules(t, home)
	entry := filepath.Join(legacy, "KANDER-AGENTS.md")
	if err := os.Remove(entry); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(entry, 0o755); err != nil {
		t.Fatal(err)
	}
	claude := filepath.Join(home, ".claude", "CLAUDE.md")
	writeIntegrateFile(t, claude, "@~/.agents/KANDER-AGENTS.md\n")
	result, err := Perform(Request{CopyBinary: true, Language: "cn", Source: stubBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.LegacyRules.Kept) != 1 || result.LegacyRules.Kept[0] != entry {
		t.Fatalf("kept=%v", result.LegacyRules.Kept)
	}
	got, _ := os.ReadFile(claude)
	if !strings.Contains(string(got), "@~/.agents/KANDER-AGENTS.md\n") || !strings.Contains(string(got), "@~/.agents/kander/KANDER-AGENTS.md\n") {
		t.Fatalf("bytes=%q", got)
	}
}

func TestPerformLegacyMigrationIsIdempotent(t *testing.T) {
	home := setupInstallHome(t)
	writeLegacyRules(t, home)
	if _, err := Perform(Request{CopyBinary: true, Language: "cn", Source: stubBinary(t)}); err != nil {
		t.Fatal(err)
	}
	result, err := Perform(Request{CopyBinary: true, Language: "cn", Source: stubBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.LegacyRules.Removed) != 0 || len(result.LegacyRules.Kept) != 0 || len(result.LegacyLinksRemoved) != 0 {
		t.Fatalf("second install migrated again: %+v", result)
	}
}

func TestPerformKeepsDirectoryAtLegacyRuleName(t *testing.T) {
	home := setupInstallHome(t)
	legacy := writeLegacyRules(t, home)
	dir := filepath.Join(legacy, "KANDER-GIT-RULES.md")
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := Perform(Request{CopyBinary: true, Language: "cn", Source: stubBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.LegacyRules.Kept) != 1 || result.LegacyRules.Kept[0] != dir {
		t.Fatalf("kept=%v", result.LegacyRules.Kept)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("directory removed: %v", err)
	}
}

func TestProjectInstallLeavesFlatGlobalRulesAlone(t *testing.T) {
	home := setupInstallHome(t)
	legacy := writeLegacyRules(t, home)
	project := initGitRepo(t, filepath.Join(t.TempDir(), "proj"))
	result, err := Perform(Request{Mode: config.ModeProject, Project: project, Language: "cn", Source: stubBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.LegacyRules.Removed) != 0 || len(result.LegacyRules.Kept) != 0 {
		t.Fatalf("project install touched global rules: %+v", result.LegacyRules)
	}
	for _, name := range rules.Names() {
		if _, err := os.Stat(filepath.Join(legacy, name)); err != nil {
			t.Fatalf("%s removed by a project install: %v", name, err)
		}
	}
}

func TestRewriteLegacyReferenceSpellings(t *testing.T) {
	home := setupInstallHome(t)
	writeLegacyRules(t, home)
	paths, err := config.GlobalInstallPaths()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Perform(Request{CopyBinary: true, Language: "cn", Source: stubBinary(t)}); err != nil {
		t.Fatal(err)
	}
	baseDir := filepath.Join(home, ".claude")
	legacyAbs := filepath.ToSlash(filepath.Join(home, ".agents", "KANDER-AGENTS.md"))
	currentAbs := filepath.ToSlash(filepath.Join(home, ".agents", "kander", "KANDER-AGENTS.md"))
	for _, tc := range []struct{ name, in, want string }{
		{"tilde", "@~/.agents/KANDER-AGENTS.md\n", "@~/.agents/kander/KANDER-AGENTS.md\n"},
		{"parent-relative", "@../.agents/KANDER-AGENTS.md\n", "@../.agents/kander/KANDER-AGENTS.md\n"},
		{"absolute", "@" + legacyAbs + "\n", "@" + currentAbs + "\n"},
		{"backup-suffix-untouched", "see ~/.agents/KANDER-AGENTS.md.bak\n", "see ~/.agents/KANDER-AGENTS.md.bak\n"},
		{"longer-prefix-untouched", "@/x" + legacyAbs + "\n", "@/x" + legacyAbs + "\n"},
		{"backticks", "read `~/.agents/KANDER-AGENTS.md` first\n", "read `~/.agents/kander/KANDER-AGENTS.md` first\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := rewriteLegacyReference(tc.in, paths, baseDir)
			if got != tc.want || changed != (got != tc.in) {
				t.Fatalf("got=%q changed=%v want=%q", got, changed, tc.want)
			}
		})
	}
}
