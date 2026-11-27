package install

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/rules"
)

// legacyRuleNames returns the top-level rule files. Earlier releases shipped only these; the
// templates/ and snippets/ directories never existed at the flat legacy location.
func legacyRuleNames() []string {
	var names []string
	for _, name := range rules.Names() {
		if !strings.Contains(name, "/") {
			names = append(names, name)
		}
	}
	return names
}

// writeLegacyRules places the official rule files at the flat ~/.agents root earlier
// releases used, together with the installer state file.
func writeLegacyRules(t *testing.T, home string) string {
	t.Helper()
	legacy := filepath.Join(home, ".agents")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range legacyRuleNames() {
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
	if len(result.LegacyRules.Removed) != len(legacyRuleNames())+1 {
		t.Fatalf("removed=%v", result.LegacyRules.Removed)
	}
	for _, name := range append(legacyRuleNames(), stateFileName) {
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

// Install never rewrites a global rules file to the current entry any more: every Kander load
// command goes, whichever entry it names, and a link to the previous entry goes as a link.
func TestPerformRemovesLegacyGlobalReferences(t *testing.T) {
	home := setupInstallHome(t)
	writeLegacyRules(t, home)
	claude := filepath.Join(home, ".claude", "CLAUDE.md")
	writeIntegrateFile(t, claude, "# Personal\n\n@~/.agents/KANDER-AGENTS.md\n\n@~/.agents/kander/KANDER-AGENTS.md\n")
	agents := filepath.Join(home, ".codex", "AGENTS.md")
	block := "## Kander Rules Entry\n\nAt the start of every session, read `~/.agents/KANDER-AGENTS.md` and follow it as the Kander workflow rules entry.\n"
	writeIntegrateFile(t, agents, "# Mine\n\n"+block)
	result, err := Perform(Request{CopyBinary: true, Language: "cn", Source: stubBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	if result.GlobalCleanup.Err != nil || len(result.GlobalCleanup.Removed) != 2 {
		t.Fatalf("cleanup=%+v", result.GlobalCleanup)
	}
	if got, _ := os.ReadFile(claude); string(got) != "# Personal\n" {
		t.Fatalf("CLAUDE.md=%q", got)
	}
	if got, _ := os.ReadFile(agents); string(got) != "# Mine\n" {
		t.Fatalf("AGENTS.md=%q", got)
	}
}

func TestPerformRemovesLegacyEntrySymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	home := setupInstallHome(t)
	legacy := writeLegacyRules(t, home)
	relocated := t.TempDir()
	for _, link := range []string{filepath.Join(home, ".codex", "AGENTS.md"), filepath.Join(relocated, "CLAUDE.md")} {
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(legacy, "KANDER-AGENTS.md"), link); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CLAUDE_CONFIG_DIR", relocated)
	result, err := Perform(Request{CopyBinary: true, Language: "cn", Source: stubBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	if result.GlobalCleanup.Err != nil || len(result.GlobalCleanup.Removed) != 2 {
		t.Fatalf("cleanup=%+v", result.GlobalCleanup)
	}
	for _, link := range []string{filepath.Join(home, ".codex", "AGENTS.md"), filepath.Join(relocated, "CLAUDE.md")} {
		if _, err := os.Lstat(link); !os.IsNotExist(err) {
			t.Fatalf("%s kept: %v", link, err)
		}
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
	if len(result.LegacyRules.Removed) != 0 || len(result.LegacyRules.Kept) != 0 || result.GlobalCleanup.Changed() {
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

func TestLegacyMigrationWaitsForResolvableTargets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	home := setupInstallHome(t)
	legacy := writeLegacyRules(t, home)
	entry := filepath.Join(legacy, "KANDER-AGENTS.md")
	claude := filepath.Join(home, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(claude), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(entry, claude); err != nil {
		t.Fatal(err)
	}
	paths := globalExtensionPaths(t, home)
	paths.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	writeScopeConfig(t, paths.ConfigPath, "codex", "relative")
	if out := migrateLegacyRules(paths); len(out.Removed) != 0 {
		t.Fatalf("migrated with an unresolvable target: %+v", out)
	}
	if _, err := os.Stat(claude); err != nil {
		t.Fatalf("rules link dangling: %v", err)
	}
	writeScopeConfig(t, paths.ConfigPath, "codex", t.TempDir())
	if result := CleanGlobalInjection(paths); result.Err != nil || len(result.Removed) != 1 {
		t.Fatalf("cleanup=%+v", result)
	}
	if out := migrateLegacyRules(paths); len(out.Removed) == 0 {
		t.Fatal("migration did not resume after the config was fixed")
	}
	if _, err := os.Lstat(claude); !os.IsNotExist(err) {
		t.Fatalf("link kept: %v", err)
	}
}
