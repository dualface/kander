package install

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
)

// enableAutoCleanup lets the test run the automatic cleanup, which every other test skips, in
// a home where an earlier release installed the rules root.
func enableAutoCleanup(t *testing.T) {
	t.Helper()
	autoCleanInjected.Store(true)
	t.Cleanup(func() { autoCleanInjected.Store(false) })
	paths, err := config.GlobalInstallPaths()
	if err != nil {
		t.Fatal(err)
	}
	writeIntegrateFile(t, RulesEntry(paths), "# Kander entry\n")
}

func readMarker(t *testing.T, paths config.InstallPaths) globalCleanupState {
	t.Helper()
	state, err := loadGlobalCleanup(paths)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestAutoCleanupRunsOncePerConfigDirectory(t *testing.T) {
	home := setupInstallHome(t)
	enableAutoCleanup(t)
	paths, _ := config.GlobalInstallPaths()
	claude := filepath.Join(home, ".claude", "CLAUDE.md")
	writeIntegrateFile(t, claude, "# Mine\n\n@~/.agents/kander/KANDER-AGENTS.md\n")

	first := AutoCleanGlobalInjection(paths)
	if !first.Ran || first.Err != nil || !slices.Equal(first.Removed, []string{claude}) {
		t.Fatalf("first=%+v", first)
	}
	if got := readBytes(t, claude); got != "# Mine\n" {
		t.Fatalf("CLAUDE.md=%q", got)
	}
	recorded := readMarker(t, paths).ConfigDirs
	if !slices.Contains(recorded, configDirKey(filepath.Join(home, ".claude"))) {
		t.Fatalf("marker=%v", recorded)
	}
	// A reference written afterwards is left alone: the directory is recorded.
	writeIntegrateFile(t, claude, "@~/.agents/kander/KANDER-AGENTS.md\n")
	if again := AutoCleanGlobalInjection(paths); again.Ran {
		t.Fatalf("recorded directory cleaned again: %+v", again)
	}
	// Another configuration directory runs the cleanup once more.
	other := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", other)
	writeIntegrateFile(t, filepath.Join(other, "CLAUDE.md"), "@~/.agents/kander/KANDER-AGENTS.md\n")
	switched := AutoCleanGlobalInjection(paths)
	if !switched.Ran || switched.Err != nil || len(switched.Removed) != 1 {
		t.Fatalf("switched=%+v", switched)
	}
	if _, err := os.Lstat(filepath.Join(other, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("file left with blank lines only must be removed: %v", err)
	}
	state := readMarker(t, paths).ConfigDirs
	if !slices.Contains(state, configDirKey(other)) || !slices.Contains(state, configDirKey(filepath.Join(home, ".claude"))) {
		t.Fatalf("marker lost a directory: %v", state)
	}
	notices := GlobalCleanupNotices(switched)
	if len(notices) != 1 || notices[0].Code != NoticeGlobalRulesCleaned || !strings.Contains(notices[0].Text, "kander init") {
		t.Fatalf("notices=%+v", notices)
	}
}

func TestAutoCleanupSkipsWhileAnotherProcessHoldsTheLock(t *testing.T) {
	home := setupInstallHome(t)
	enableAutoCleanup(t)
	paths, _ := config.GlobalInstallPaths()
	claude := filepath.Join(home, ".claude", "CLAUDE.md")
	writeIntegrateFile(t, claude, "@~/.agents/kander/KANDER-AGENTS.md\n")
	if err := fs.EnsureInheritedDirectoryPath(paths.RulesDir); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(paths.RulesDir, globalCleanupLockName)
	anchor, err := fileAnchor(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	file, err := fs.OpenLockFile(anchor, lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	lock, taken, err := fs.TryLockExclusive(file)
	if err != nil || !taken {
		t.Fatalf("lock taken=%v err=%v", taken, err)
	}
	result := AutoCleanGlobalInjection(paths)
	if result.Ran || result.Err != nil || len(GlobalCleanupNotices(result)) != 0 {
		t.Fatalf("cleanup ran under another holder: %+v", result)
	}
	if got := readBytes(t, claude); !strings.Contains(got, "KANDER-AGENTS.md") {
		t.Fatalf("file changed under another holder: %q", got)
	}
	if err := lock.Unlock(); err != nil {
		t.Fatal(err)
	}
	if result := AutoCleanGlobalInjection(paths); !result.Ran || len(result.Removed) != 1 {
		t.Fatalf("cleanup after release=%+v", result)
	}
}

func TestAutoCleanupFailureWritesNoMarker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	home := setupInstallHome(t)
	enableAutoCleanup(t)
	paths, _ := config.GlobalInstallPaths()
	dir := filepath.Join(home, ".codex")
	writeIntegrateFile(t, filepath.Join(dir, "AGENTS.md"), "# Mine\n\n@~/.agents/kander/KANDER-AGENTS.md\n")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	result := AutoCleanGlobalInjection(paths)
	if !result.Ran || result.Err == nil {
		t.Fatalf("result=%+v", result)
	}
	notices := GlobalCleanupNotices(result)
	if len(notices) == 0 || notices[len(notices)-1].Code != NoticeGlobalRulesCleanupFailed {
		t.Fatalf("notices=%+v", notices)
	}
	if state := readMarker(t, paths); len(state.ConfigDirs) != 0 {
		t.Fatalf("failed cleanup recorded: %v", state.ConfigDirs)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if retry := AutoCleanGlobalInjection(paths); !retry.Ran || retry.Err != nil || len(retry.Removed) != 1 {
		t.Fatalf("retry=%+v", retry)
	}
}

// A global rules file that links to another account's file is edited in the real file; the
// link stays, and nothing is deleted through it.
func TestGlobalCleanupEditsTheRealFileOfALinkedRulesFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := setupInstallHome(t)
	paths, _ := config.GlobalInstallPaths()
	real := filepath.Join(home, ".claude", "CLAUDE.md")
	writeIntegrateFile(t, real, "@~/.agents/kander/KANDER-AGENTS.md\n")
	account := filepath.Join(home, ".claude-account2")
	if err := os.MkdirAll(account, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(account, "CLAUDE.md")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", account)
	result := CleanGlobalInjection(paths)
	if result.Err != nil || len(result.Removed) != 1 {
		t.Fatalf("result=%+v", result)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link removed: %v", err)
	}
	if got := readBytes(t, real); got != "" {
		t.Fatalf("real file=%q", got)
	}
}

func TestGlobalCleanupKeepsUserContent(t *testing.T) {
	home := setupInstallHome(t)
	paths, _ := config.GlobalInstallPaths()
	codex := filepath.Join(home, ".codex", "AGENTS.md")
	body := "# Mine\n\nSee `~/.agents/kander/KANDER-AGENTS.md` for the workflow.\n\n<!-- @~/.agents/kander/KANDER-AGENTS.md -->\n"
	writeIntegrateFile(t, codex, body)
	if result := CleanGlobalInjection(paths); result.Err != nil || result.Changed() {
		t.Fatalf("result=%+v", result)
	}
	if got := readBytes(t, codex); got != body {
		t.Fatalf("user content changed: %q", got)
	}
}

// A home without the rules root and without Kander content gets no marker or lock; once a
// global rules file holds a Kander reference, the first command removes it.
func TestAutoCleanupWithoutARulesRootRunsOnlyForKanderContent(t *testing.T) {
	home := setupInstallHome(t)
	autoCleanInjected.Store(true)
	t.Cleanup(func() { autoCleanInjected.Store(false) })
	paths, _ := config.GlobalInstallPaths()
	claude := filepath.Join(home, ".claude", "CLAUDE.md")
	writeIntegrateFile(t, claude, "# Mine\n")
	if result := AutoCleanGlobalInjection(paths); result.Ran {
		t.Fatalf("cleanup ran in a home without Kander content: %+v", result)
	}
	if _, err := os.Lstat(paths.RulesDir); !os.IsNotExist(err) {
		t.Fatalf("cleanup created the rules root: %v", err)
	}
	writeIntegrateFile(t, claude, "# Mine\n\n@~/.agents/kander/KANDER-AGENTS.md\n")
	if result := AutoCleanGlobalInjection(paths); !result.Ran || len(result.Removed) != 1 {
		t.Fatalf("cleanup with a reference=%+v", result)
	}
	if got := readBytes(t, claude); got != "# Mine\n" {
		t.Fatalf("CLAUDE.md=%q", got)
	}
}

// A home with only the flat layout of earlier releases (~/.agents/KANDER-AGENTS.md, no
// ~/.agents/kander/) still has its global injection removed by the first command.
func TestAutoCleanupRunsForTheFlatLegacyLayout(t *testing.T) {
	home := setupInstallHome(t)
	autoCleanInjected.Store(true)
	t.Cleanup(func() { autoCleanInjected.Store(false) })
	paths, _ := config.GlobalInstallPaths()
	writeIntegrateFile(t, legacyRulesEntry(paths), "# Kander entry\n")
	claude := filepath.Join(home, ".claude", "CLAUDE.md")
	writeIntegrateFile(t, claude, "# Mine\n\n@~/.agents/KANDER-AGENTS.md\n")
	codex := filepath.Join(home, ".codex", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(codex), 0o755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(legacyRulesEntry(paths), codex); err != nil {
			t.Fatal(err)
		}
	}
	result := AutoCleanGlobalInjection(paths)
	if !result.Ran || result.Err != nil || !slices.Contains(result.Removed, claude) {
		t.Fatalf("result=%+v", result)
	}
	if got := readBytes(t, claude); got != "# Mine\n" {
		t.Fatalf("CLAUDE.md=%q", got)
	}
	if runtime.GOOS != "windows" {
		if _, err := os.Lstat(codex); !os.IsNotExist(err) {
			t.Fatalf("link to the flat entry kept: %v", err)
		}
	}
	if notices := GlobalCleanupNotices(result); len(notices) == 0 || notices[0].Code != NoticeGlobalRulesCleaned {
		t.Fatalf("notices=%+v", notices)
	}
	if again := AutoCleanGlobalInjection(paths); again.Ran {
		t.Fatalf("recorded directories cleaned again: %+v", again)
	}
}

func TestAutoCleanupIsOffInOtherTests(t *testing.T) {
	home := setupInstallHome(t)
	paths, _ := config.GlobalInstallPaths()
	claude := filepath.Join(home, ".claude", "CLAUDE.md")
	writeIntegrateFile(t, claude, "@~/.agents/kander/KANDER-AGENTS.md\n")
	if result := AutoCleanGlobalInjection(paths); result.Ran {
		t.Fatalf("automatic cleanup ran in a test that did not opt in: %+v", result)
	}
}
