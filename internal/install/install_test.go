package install

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/rules"
)

func setupInstallHome(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(config.EnvLang, "cn")
	t.Setenv(config.EnvLangCLI, "")
	_ = os.Unsetenv(config.EnvConfig)
	_ = os.Unsetenv(EnvSkipInstall)
	config.ApplyLanguageArgument(nil)
	config.BindConfigLanguage(nil)
	return home
}

func stubBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), binaryName())
	script := []byte("#!/bin/sh\necho stub\n")
	if runtime.GOOS == "windows" {
		script = []byte("stub")
	}
	if err := os.WriteFile(path, script, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func initGitRepo(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, path, "init", "-q", path)
	runGit(t, path, "-C", path, "config", "user.email", "kander@example.com")
	runGit(t, path, "-C", path, "config", "user.name", "Kander Test")
	runGit(t, path, "-C", path, "commit", "--allow-empty", "-q", "-m", "init")
	return path
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func TestPerformGlobalInstall(t *testing.T) {
	home := setupInstallHome(t)
	src := stubBinary(t)
	result, err := Perform(Request{Language: "cn", Source: src})
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(home, ".local", "bin", binaryName())
	if result.DestBinary != dest {
		t.Fatalf("dest=%s", result.DestBinary)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := os.ReadFile(src)
	if !bytes.Equal(got, want) {
		t.Fatal("binary mismatch")
	}
	st, err := os.Stat(dest)
	if err != nil || (runtime.GOOS != "windows" && st.Mode()&0o111 == 0) {
		t.Fatalf("not executable: %v", err)
	}
	if len(rules.Names()) != 10 {
		t.Fatalf("names=%v", rules.Names())
	}
	for _, name := range rules.Names() {
		data, actual, err := rules.File("cn", name)
		if err != nil {
			t.Fatal(err)
		}
		if actual != "cn" {
			t.Fatalf("%s actual=%s", name, actual)
		}
		got, err := os.ReadFile(filepath.Join(home, ".agents", name))
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("%s mismatch: %v", name, err)
		}
	}
	link, err := os.Readlink(filepath.Join(home, ".agents", "AGENTS.md"))
	if err != nil {
		if runtime.GOOS == "windows" {
			got, _ := os.ReadFile(filepath.Join(home, ".agents", "AGENTS.md"))
			want, _ := os.ReadFile(filepath.Join(home, ".agents", "KANDER-AGENTS.md"))
			if !bytes.Equal(got, want) {
				t.Fatal("windows agents entry mismatch")
			}
		} else {
			t.Fatal(err)
		}
	} else if link != "KANDER-AGENTS.md" {
		t.Fatalf("link=%s", link)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "share", "kander")); err != nil {
		t.Fatal(err)
	}
}

func TestPerformPreservesExistingAgentsEntry(t *testing.T) {
	home := setupInstallHome(t)
	agents := filepath.Join(home, ".agents", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(agents), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agents, []byte("local-rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Perform(Request{Language: "cn", Source: stubBinary(t)}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(agents)
	if string(got) != "local-rules\n" {
		t.Fatalf("got %q", got)
	}
}

func TestPerformKeepsLegacyByDefault(t *testing.T) {
	home := setupInstallHome(t)
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"onevoke", "kanban"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("legacy\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	result, err := Perform(Request{Language: "cn", Source: stubBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Legacy) < 2 || result.LegacyRemoved {
		t.Fatalf("legacy=%v removed=%v", result.Legacy, result.LegacyRemoved)
	}
	for _, name := range []string{"onevoke", "kanban"} {
		got, _ := os.ReadFile(filepath.Join(bin, name))
		if string(got) != "legacy\n" {
			t.Fatalf("%s mutated", name)
		}
	}
}

func TestPerformRemovesLegacyAfterConfirm(t *testing.T) {
	home := setupInstallHome(t)
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "onevoke"), []byte("legacy\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := Perform(Request{Language: "cn", Source: stubBinary(t), DeleteLegacy: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.LegacyRemoved {
		t.Fatal("expected removal")
	}
	if _, err := os.Stat(filepath.Join(bin, "onevoke")); !os.IsNotExist(err) {
		t.Fatal("legacy remains")
	}
}

func TestPerformRejectsLegacyDirectory(t *testing.T) {
	home := setupInstallHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".local", "bin", "onevoke"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Perform(Request{Language: "cn", Source: stubBinary(t)})
	if err == nil || !strings.Contains(err.Error(), "onevoke") {
		t.Fatalf("err=%v", err)
	}
}

func TestPerformRejectsDirectoryTarget(t *testing.T) {
	home := setupInstallHome(t)
	blocked := filepath.Join(home, ".agents", "KANDER-AGENTS.md")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Perform(Request{Language: "cn", Source: stubBinary(t)})
	if err == nil || !strings.Contains(err.Error(), blocked) {
		t.Fatalf("err=%v", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".local", "bin", binaryName())); !os.IsNotExist(statErr) {
		t.Fatal("wrote binary after reject")
	}
}

func TestPerformRejectsSourceSymlink(t *testing.T) {
	setupInstallHome(t)
	real := stubBinary(t)
	link := filepath.Join(t.TempDir(), "linked-"+binaryName())
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	_, err := Perform(Request{Language: "cn", Source: link})
	if err == nil {
		t.Fatal("expected symlink reject")
	}
}

func TestPerformProjectSkipsGlobal(t *testing.T) {
	home := setupInstallHome(t)
	canary := filepath.Join(home, "keep")
	if err := os.WriteFile(canary, []byte("home-canary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	project := initGitRepo(t, filepath.Join(t.TempDir(), "app"))
	src := stubBinary(t)
	result, err := Perform(Request{Language: "cn", Mode: config.ModeProject, Project: project, Source: src})
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(project, ".kander", "bin", binaryName())
	if result.DestBinary != dest {
		t.Fatalf("dest=%s", result.DestBinary)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".kander", "rules", "KANDER-AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	exclude, _ := os.ReadFile(filepath.Join(project, ".git", "info", "exclude"))
	if !strings.Contains(string(exclude), "/.kander/") {
		t.Fatalf("exclude=%q", exclude)
	}
	if _, err := Perform(Request{Language: "cn", Mode: config.ModeProject, Project: project, Source: src}); err != nil {
		t.Fatal(err)
	}
	exclude2, _ := os.ReadFile(filepath.Join(project, ".git", "info", "exclude"))
	if strings.Count(string(exclude2), "/.kander/") != 1 {
		t.Fatalf("exclude duplicated: %q", exclude2)
	}
	if _, err := os.Stat(filepath.Join(home, ".local")); !os.IsNotExist(err) {
		t.Fatal("touched global bin")
	}
	if _, err := os.Stat(filepath.Join(home, ".agents")); !os.IsNotExist(err) {
		t.Fatal("touched global agents")
	}
	got, _ := os.ReadFile(canary)
	if string(got) != "home-canary\n" {
		t.Fatal("home mutated")
	}
}

func TestPerformProjectFromLinkedWorktree(t *testing.T) {
	setupInstallHome(t)
	main := initGitRepo(t, filepath.Join(t.TempDir(), "app"))
	linked := filepath.Join(t.TempDir(), "app-linked")
	runGit(t, main, "-C", main, "worktree", "add", "-q", linked, "HEAD")
	result, err := Perform(Request{Language: "cn", Mode: config.ModeProject, Project: linked, Source: stubBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.DestBinary, filepath.Join(main, ".kander", "bin")) {
		t.Fatalf("dest=%s", result.DestBinary)
	}
	if _, err := os.Stat(filepath.Join(linked, ".kander")); !os.IsNotExist(err) {
		t.Fatal("installed into linked worktree")
	}
}

func TestPerformProjectRejectsNonGitAndSymlink(t *testing.T) {
	setupInstallHome(t)
	_, err := Perform(Request{Language: "cn", Mode: config.ModeProject, Project: t.TempDir(), Source: stubBinary(t)})
	if err == nil {
		t.Fatal("expected non-git reject")
	}
	project := initGitRepo(t, filepath.Join(t.TempDir(), "app"))
	link := filepath.Join(t.TempDir(), "app-link")
	if err := os.Symlink(project, link); err != nil {
		t.Fatal(err)
	}
	_, err = Perform(Request{Language: "cn", Mode: config.ModeProject, Project: link, Source: stubBinary(t)})
	if err == nil {
		t.Fatal("expected symlink reject")
	}
}

func TestPerformProjectRejectsKanderSymlink(t *testing.T) {
	setupInstallHome(t)
	project := initGitRepo(t, filepath.Join(t.TempDir(), "app"))
	outside := filepath.Join(t.TempDir(), "payload")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "keep"), []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(project, ".kander")); err != nil {
		t.Fatal(err)
	}
	_, err := Perform(Request{Language: "cn", Mode: config.ModeProject, Project: project, Source: stubBinary(t)})
	if err == nil {
		t.Fatal("expected .kander symlink reject")
	}
	got, _ := os.ReadFile(filepath.Join(outside, "keep"))
	if string(got) != "outside\n" {
		t.Fatal("followed symlink")
	}
}

func TestPerformSkipsSelfCopy(t *testing.T) {
	home := setupInstallHome(t)
	src := stubBinary(t)
	if _, err := Perform(Request{Language: "cn", Source: src}); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(home, ".local", "bin", binaryName())
	result, err := Perform(Request{Language: "cn", Source: dest})
	if err != nil {
		t.Fatal(err)
	}
	if result.Copied {
		t.Fatal("expected skip")
	}
}

func TestRepairRulesLeavesModifiedFile(t *testing.T) {
	home := setupInstallHome(t)
	if _, err := Perform(Request{Language: "cn", Source: stubBinary(t)}); err != nil {
		t.Fatal(err)
	}
	edited := filepath.Join(home, ".agents", "KANDER-CODE-RULES.md")
	if err := os.WriteFile(edited, []byte("edited locally\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := config.GlobalInstallPaths()
	if err != nil {
		t.Fatal(err)
	}
	report, err := InspectRules(paths, "cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Modified) != 1 || report.Modified[0] != "KANDER-CODE-RULES.md" {
		t.Fatalf("modified=%v", report.Modified)
	}
	if err := os.Remove(filepath.Join(home, ".agents", "KANDER-AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	if err := RepairRules(paths, "cn"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "KANDER-AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(edited)
	if string(got) != "edited locally\n" {
		t.Fatalf("overwrote modified file: %q", got)
	}
}

func TestInspectRulesLanguageDriftIsNotUnhealthy(t *testing.T) {
	setupInstallHome(t)
	if _, err := Perform(Request{Language: "cn", Source: stubBinary(t)}); err != nil {
		t.Fatal(err)
	}
	paths, err := config.GlobalInstallPaths()
	if err != nil {
		t.Fatal(err)
	}
	report, err := InspectRules(paths, "en")
	if err != nil {
		t.Fatal(err)
	}
	if !report.LanguageDrift || len(report.Missing) != 0 || len(report.Outdated) != 0 {
		t.Fatalf("%+v", report)
	}
}

func TestRepairRulesUpgradesUnstampedOfficial(t *testing.T) {
	home := setupInstallHome(t)
	if _, err := Perform(Request{Language: "cn", Source: stubBinary(t)}); err != nil {
		t.Fatal(err)
	}
	agents := filepath.Join(home, ".agents")
	if err := os.Remove(filepath.Join(agents, stateFileName)); err != nil {
		t.Fatal(err)
	}
	legacy, err := os.ReadFile("testdata/legacy-KANDER-BASE-RULES.md")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agents, "KANDER-BASE-RULES.md"), legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := config.GlobalInstallPaths()
	if err != nil {
		t.Fatal(err)
	}
	report, err := InspectRules(paths, "cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Outdated) != 1 || report.Outdated[0] != "KANDER-BASE-RULES.md" {
		t.Fatalf("outdated=%v modified=%v", report.Outdated, report.Modified)
	}
	if err := RepairRules(paths, "cn"); err != nil {
		t.Fatal(err)
	}
	want, _, err := rules.File("cn", "KANDER-BASE-RULES.md")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(agents, "KANDER-BASE-RULES.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("did not upgrade official unstamped rule")
	}
	state, err := loadRulesState(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Files) != len(rules.Names()) {
		t.Fatalf("stamp files=%d", len(state.Files))
	}
}

func TestRepairRulesBootstrapsStampWhenCurrent(t *testing.T) {
	home := setupInstallHome(t)
	if _, err := Perform(Request{Language: "cn", Source: stubBinary(t)}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(home, ".agents", stateFileName)); err != nil {
		t.Fatal(err)
	}
	paths, err := config.GlobalInstallPaths()
	if err != nil {
		t.Fatal(err)
	}
	report, err := InspectRules(paths, "cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Missing)+len(report.Outdated)+len(report.Modified) != 0 {
		t.Fatalf("%+v", report)
	}
	if err := RepairRules(paths, "cn"); err != nil {
		t.Fatal(err)
	}
	state, err := loadRulesState(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Files) != len(rules.Names()) {
		t.Fatalf("stamp files=%d", len(state.Files))
	}
}

func TestPerformUpdatesExistingConfigLanguage(t *testing.T) {
	setupInstallHome(t)
	cfg := config.DefaultConfig()
	cfg.Language = "en"
	cfg.WelcomeComplete = true
	cfg.KanbanAgent = "claude"
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := Perform(Request{Language: "cn", Source: stubBinary(t)}); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Language != "cn" {
		t.Fatalf("language=%q", loaded.Language)
	}
	if loaded.KanbanAgent != "claude" || !loaded.WelcomeComplete {
		t.Fatalf("other fields changed: %+v", loaded)
	}
}

func TestPerformDoesNotCreateConfig(t *testing.T) {
	setupInstallHome(t)
	if _, err := Perform(Request{Language: "cn", Source: stubBinary(t)}); err != nil {
		t.Fatal(err)
	}
	exists, err := config.Exists()
	if err != nil || exists {
		t.Fatalf("exists=%v err=%v", exists, err)
	}
}

func TestGlobalSymlinkRuleIsNotFalseStamped(t *testing.T) {
	home := setupInstallHome(t)
	if _, err := Perform(Request{Language: "cn", Source: stubBinary(t)}); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(home, ".agents", "KANDER-CODE-RULES.md")
	target := filepath.Join(home, "dotfiles", "KANDER-CODE-RULES.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("dotfiles copy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, dest); err != nil {
		t.Skip(err)
	}
	if _, err := Perform(Request{Language: "cn", Source: stubBinary(t)}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "dotfiles copy\n" {
		t.Fatalf("symlink target changed: %q %v", got, err)
	}
	paths, err := config.GlobalInstallPaths()
	if err != nil {
		t.Fatal(err)
	}
	report, err := InspectRules(paths, "cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Outdated) != 0 {
		t.Fatalf("outdated=%v", report.Outdated)
	}
	found := false
	for _, name := range report.Modified {
		if name == "KANDER-CODE-RULES.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("modified=%v", report.Modified)
	}
}

func TestShouldRunWizardSkipsSourceTree(t *testing.T) {
	_ = setupInstallHome(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/dualface/kander\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(root, "cmd", "kander", binaryName())
	lookupExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { lookupExecutable = os.Executable })
	ok, err := ShouldRunWizard()
	if err != nil || ok {
		t.Fatalf("source tree: ok=%v err=%v", ok, err)
	}
}

func TestShouldRunWizardSkipsAlreadyInstalled(t *testing.T) {
	home := setupInstallHome(t)
	src := stubBinary(t)
	if _, err := Perform(Request{Language: "cn", Source: src}); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(home, ".local", "bin", binaryName())
	lookupExecutable = func() (string, error) { return dest, nil }
	t.Cleanup(func() { lookupExecutable = os.Executable })
	ok, err := ShouldRunWizard()
	if err != nil || ok {
		t.Fatalf("already installed: ok=%v err=%v", ok, err)
	}
}

func TestShouldRunWizard(t *testing.T) {
	home := setupInstallHome(t)
	t.Setenv(EnvSkipInstall, "1")
	ok, err := ShouldRunWizard()
	if err != nil || ok {
		t.Fatalf("skip: ok=%v err=%v", ok, err)
	}
	t.Setenv(EnvSkipInstall, "")
	lookupExecutable = func() (string, error) {
		return filepath.Join(t.TempDir(), "downloaded-kander"), nil
	}
	t.Cleanup(func() { lookupExecutable = os.Executable })
	ok, err = ShouldRunWizard()
	if err != nil || !ok {
		t.Fatalf("downloaded: ok=%v err=%v", ok, err)
	}
	if _, err := Perform(Request{Language: "cn", Source: stubBinary(t)}); err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(home, ".config", "kander")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	ok, err = ShouldRunWizard()
	if err != nil || ok {
		t.Fatalf("configured: ok=%v err=%v", ok, err)
	}
}

func TestWriteBinaryBusyRenameAside(t *testing.T) {
	home := setupInstallHome(t)
	dest := filepath.Join(home, ".local", "bin", binaryName())
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	origAside, origBusy, origWrite := asideOnBusy, fileIsBusy, writeExec
	t.Cleanup(func() {
		asideOnBusy = origAside
		fileIsBusy = origBusy
		writeExec = origWrite
	})
	calls := 0
	asideOnBusy = func() bool { return true }
	fileIsBusy = func(error) bool { return true }
	writeExec = func(root, path string, data []byte, replace bool) error {
		calls++
		if calls == 1 {
			return errors.New("sharing violation")
		}
		return origWrite(root, path, data, replace)
	}
	if err := writeBinary(dest, []byte("new-bytes")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "new-bytes" {
		t.Fatalf("dest=%q err=%v", got, err)
	}
	aside, err := os.ReadFile(dest + ".old")
	if err != nil || string(aside) != "old" {
		t.Fatalf("aside=%q err=%v", aside, err)
	}
}

func TestFinishSuccessfulInstallAlwaysHandoffs(t *testing.T) {
	home := setupInstallHome(t)
	src := stubBinary(t)
	result, err := Perform(Request{Language: "cn", Source: src})
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(home, ".local", "bin", binaryName())
	if result.DestBinary != dest {
		t.Fatalf("dest=%s", result.DestBinary)
	}

	var calls int
	var gotDest, gotLangFlag, gotLang string
	var gotEnv []string
	handoff = func(path string, argv, env []string) error {
		calls++
		gotDest = path
		gotEnv = append([]string(nil), env...)
		if len(argv) >= 3 {
			gotLangFlag, gotLang = argv[1], argv[2]
		}
		return nil
	}
	t.Cleanup(func() { handoff = defaultHandoff })

	if code := finishSuccessfulInstall(result, "cn"); code != 0 {
		t.Fatalf("code=%d", code)
	}
	if calls != 1 || gotDest != dest || gotLangFlag != "--lang" || gotLang != "cn" {
		t.Fatalf("first handoff: calls=%d dest=%q argv=%q %q", calls, gotDest, gotLangFlag, gotLang)
	}
	if !envHasPostInstall(gotEnv) {
		t.Fatalf("missing %s in env: %v", EnvPostInstall, gotEnv)
	}

	// Re-install from the destination itself (same file); handoff must still run.
	same, err := Perform(Request{Language: "en", Source: dest})
	if err != nil {
		t.Fatal(err)
	}
	calls = 0
	gotEnv = nil
	if code := finishSuccessfulInstall(same, "en"); code != 0 {
		t.Fatalf("same-file code=%d", code)
	}
	if calls != 1 || gotDest != dest || gotLang != "en" {
		t.Fatalf("same-file handoff: calls=%d dest=%q lang=%q", calls, gotDest, gotLang)
	}
	if !envHasPostInstall(gotEnv) {
		t.Fatalf("same-file missing %s in env: %v", EnvPostInstall, gotEnv)
	}
}

func envHasPostInstall(env []string) bool {
	want := EnvPostInstall + "=1"
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}
