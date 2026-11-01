package install

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestRemoveRulesReferences(t *testing.T) {
	block := "## Kander Rules Entry\n\nAt the start of every session, read `~/.agents/kander/KANDER-AGENTS.md` and follow it as the Kander workflow rules entry.\n"
	for _, tt := range []struct {
		name, text, want string
		removed          int
	}{
		{"appended block", "# Mine\n\n" + block, "# Mine\n", 1},
		{"claude import", "# Mine\n\n@~/.agents/kander/KANDER-AGENTS.md\n", "# Mine\n", 1},
		{"kander only", block, "", 1},
		{"block before heading", "a\n\n" + block + "\n# Next\nb\n", "a\n\n# Next\nb\n", 1},
		{"legacy and invalid", "# Mine\n\n@~/.agents/KANDER-AGENTS.md\n/gone/KANDER-AGENTS.md\n", "# Mine\n", 2},
		{"comment and prose stay", "<!-- @~/.agents/kander/KANDER-AGENTS.md -->\nSee ~/.agents/kander/KANDER-AGENTS.md for rules.\n", "", 0},
		{"no trailing newline", "# Mine\n\n@~/.agents/kander/KANDER-AGENTS.md", "# Mine", 1},
		{"comment on the command line", "@~/.agents/kander/KANDER-AGENTS.md <!-- user: run checks before committing -->\n", "<!-- user: run checks before committing -->\n", 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, removed := RemoveRulesReferences(tt.text)
			want := tt.want
			if tt.removed == 0 {
				want = tt.text
			}
			if got != want || removed != tt.removed {
				t.Fatalf("got %q removed=%d, want %q removed=%d", got, removed, want, tt.removed)
			}
		})
	}
}

// installForUninstall runs a global install over existing claude, codex, cursor, and pi
// config directories and returns the install paths.
func installForUninstall(t *testing.T, home string) config.InstallPaths {
	t.Helper()
	for _, dir := range []string{".claude", ".codex", ".cursor", ".pi/agent"} {
		if err := os.MkdirAll(filepath.Join(home, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeIntegrateFile(t, filepath.Join(home, ".codex", "AGENTS.md"), "# Personal codex rules\n")
	writeIntegrateFile(t, filepath.Join(home, ".codex", "AGENTS.override.md"), "# Override\n")
	if _, err := Perform(Request{Language: "cn", Source: stubBinary(t)}); err != nil {
		t.Fatal(err)
	}
	paths, _ := config.GlobalInstallPaths()
	return paths
}

func TestUninstallRemovesKanderWritesOnly(t *testing.T) {
	home := setupInstallHome(t)
	paths := installForUninstall(t, home)
	userFile := filepath.Join(paths.RulesDir, "notes.txt")
	writeIntegrateFile(t, userFile, "mine\n")
	piRules := filepath.Join(home, ".pi", "agent", "AGENTS.md")
	writeIntegrateFile(t, piRules, "# Pi\n\n"+readBytes(t, piRules))

	plan := PlanUninstall(paths)
	var out bytes.Buffer
	if err := ApplyUninstall(plan, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	for _, gone := range []string{
		filepath.Join(home, ".claude", "CLAUDE.md"),
		filepath.Join(home, ".cursor", "rules", "kander.mdc"),
		piExtensionTarget(t, home, false),
		RulesEntry(paths),
		filepath.Join(paths.RulesDir, stateFileName),
		filepath.Join(paths.RulesDir, "snippets"),
	} {
		if _, err := os.Lstat(gone); !os.IsNotExist(err) {
			t.Errorf("%s still exists: %v", gone, err)
		}
	}
	for path, want := range map[string]string{
		filepath.Join(home, ".codex", "AGENTS.md"):          "# Personal codex rules\n",
		filepath.Join(home, ".codex", "AGENTS.override.md"): "# Override\n",
		piRules:  "# Pi\n",
		userFile: "mine\n",
	} {
		if got := readBytes(t, path); got != want {
			t.Errorf("%s=%q want %q", path, got, want)
		}
	}
	switches, err := config.ScopeInstallRules(paths)
	if err != nil || switches.Global || len(switches.Agents) != 0 {
		t.Fatalf("switches=%+v err=%v", switches, err)
	}
	for _, kept := range []string{config.Text("install.uninstall_not_kander_file"), userFile} {
		if !strings.Contains(planText(plan), kept) {
			t.Errorf("plan does not report %q:\n%s", kept, planText(plan))
		}
	}

	// A later doctor repair restores the rules root but injects nothing.
	if _, err := RepairRules(paths); err != nil {
		t.Fatal(err)
	}
	outcome, err := EnsureRulesIntegration("claude", paths)
	if err != nil || outcome.Status != IntegrationDisabled {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".claude", "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("doctor rewrote the claude reference: %v", err)
	}
}

func planText(plan UninstallPlan) string {
	var out bytes.Buffer
	printUninstallPlan(&out, plan)
	return out.String()
}

func TestUninstallKeepsModifiedExtensionAndEditsSymlinkedRules(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := setupInstallHome(t)
	paths := installForUninstall(t, home)
	ext := piExtensionTarget(t, home, false)
	writeIntegrateFile(t, ext, "// my edit\n")
	real := filepath.Join(home, "dotfiles", "CLAUDE.md")
	writeIntegrateFile(t, real, "# Dotfiles\n\n@~/.agents/kander/KANDER-AGENTS.md\n")
	link := filepath.Join(home, ".claude", "CLAUDE.md")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	plan := PlanUninstall(paths)
	if err := ApplyUninstall(plan, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if got := readBytes(t, ext); got != "// my edit\n" {
		t.Fatalf("modified extension changed: %q", got)
	}
	if !strings.Contains(planText(plan), config.Text("install.uninstall_extension_modified")) {
		t.Fatal("modified extension not reported")
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlinked rules file removed: %v", err)
	}
	if got := readBytes(t, real); got != "# Dotfiles\n" {
		t.Fatalf("symlinked rules file=%q", got)
	}
}

func TestUninstallRefusesSymlinkedRulesDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := setupInstallHome(t)
	paths, _ := config.GlobalInstallPaths()
	elsewhere := filepath.Join(home, "elsewhere")
	writeIntegrateFile(t, filepath.Join(elsewhere, "KANDER-AGENTS.md"), "# victim\n")
	if err := os.MkdirAll(filepath.Dir(paths.RulesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, paths.RulesDir); err != nil {
		t.Fatal(err)
	}
	plan := PlanUninstall(paths)
	if err := ApplyUninstall(plan, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if got := readBytes(t, filepath.Join(elsewhere, "KANDER-AGENTS.md")); got != "# victim\n" {
		t.Fatalf("deleted through the link: %q", got)
	}
	if !strings.Contains(planText(plan), paths.RulesDir) {
		t.Fatal("symlinked rules dir not reported")
	}
}

func TestUninstallProjectRefusesRulesFileOutsideWorktree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := setupInstallHome(t)
	project := initGitRepo(t, filepath.Join(t.TempDir(), "app"))
	outside := filepath.Join(home, "dotfiles", "AGENTS.md")
	writeIntegrateFile(t, outside, "# Mine\n\n@~/.agents/kander/KANDER-AGENTS.md\n")
	if err := os.Symlink(outside, filepath.Join(project, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	paths, err := config.ProjectInstallPaths(project)
	if err != nil {
		t.Fatal(err)
	}
	plan := PlanUninstall(paths)
	if err := ApplyUninstall(plan, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if got := readBytes(t, outside); !strings.Contains(got, "KANDER-AGENTS.md") {
		t.Fatalf("file outside the worktree edited: %q", got)
	}
	if !strings.Contains(planText(plan), filepath.Join(project, "AGENTS.md")) {
		t.Fatal("escaping rules file not reported")
	}
}

func TestRunUninstallConfirmation(t *testing.T) {
	home := setupInstallHome(t)
	paths := installForUninstall(t, home)
	claude := filepath.Join(home, ".claude", "CLAUDE.md")
	terminal, in := false, ""
	uninstallTerminal = func() bool { return terminal }
	t.Cleanup(func() {
		uninstallTerminal = func() bool { return isTTY(os.Stdin) && isTTY(os.Stderr) }
		uninstallPromptIn, uninstallPromptOut = os.Stdin, os.Stderr
	})
	uninstallPromptOut = &bytes.Buffer{}
	run := func(args ...string) int {
		uninstallPromptIn = strings.NewReader(in)
		return RunUninstall(args)
	}
	if code := run(); code != 2 {
		t.Fatalf("no terminal without --yes: code=%d", code)
	}
	terminal, in = true, "n\n"
	if code := run(); code != 1 {
		t.Fatalf("declined: code=%d", code)
	}
	in = "\n"
	if code := run(); code != 1 {
		t.Fatalf("default answer: code=%d", code)
	}
	if _, err := os.Stat(claude); err != nil {
		t.Fatalf("declined uninstall changed files: %v", err)
	}
	if switches, _ := config.ScopeInstallRules(paths); !switches.Global {
		t.Fatal("declined uninstall wrote the switch")
	}
	in = "y\n"
	if code := run(); code != 0 {
		t.Fatalf("confirmed: code=%d", code)
	}
	for _, gone := range []string{claude, paths.RulesDir} {
		if _, err := os.Lstat(gone); !os.IsNotExist(err) {
			t.Fatalf("%s kept: %v", gone, err)
		}
	}
	for _, kept := range []string{paths.ConfigPath, paths.ShareDir} {
		if _, err := os.Stat(kept); err != nil {
			t.Fatalf("uninstall removed %s: %v", kept, err)
		}
	}
	if code := run("--force"); code != 2 {
		t.Fatalf("unknown option: code=%d", code)
	}
}

func TestUninstallEmptiesFileBehindSymlinkedDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := setupInstallHome(t)
	dotfiles := filepath.Join(home, "dotfiles", "codex")
	if err := os.MkdirAll(dotfiles, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dotfiles, filepath.Join(home, ".codex")); err != nil {
		t.Fatal(err)
	}
	paths, _ := config.GlobalInstallPaths()
	real := filepath.Join(dotfiles, "AGENTS.md")
	writeIntegrateFile(t, real, referenceBlock("codex", paths))
	plan := PlanUninstall(paths)
	if err := ApplyUninstall(plan, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if got := readBytes(t, real); got != "" {
		t.Fatalf("file behind the symlinked directory=%q", got)
	}
}
