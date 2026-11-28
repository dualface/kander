package tui

import (
	"encoding/json"
	"github.com/dualface/kander/v2/internal/install/installtest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/install"
	"github.com/dualface/kander/v2/internal/menu"
)

// stubVersionRefresh makes the version refresh of Run report outcome, so a test decides whether
// the startup doctor gate opens.
func stubVersionRefresh(t *testing.T, outcome menu.VersionRefresh) {
	t.Helper()
	old := refreshForVersion
	refreshForVersion = func() menu.VersionRefresh { return outcome }
	t.Cleanup(func() { refreshForVersion = old })
}

// stampStartupVersion makes the version refresh find the running version recorded, so Run skips
// the startup doctor gate entirely, matching the pre-gate behavior these tests assert.
func stampStartupVersion(t *testing.T) {
	t.Helper()
	stubVersionRefresh(t, menu.VersionRefresh{Status: menu.VersionRefreshCurrent})
}

// stubDoctorAck replaces the Enter wait so a TTY-backed test run never blocks
// on os.Stdin; the real doctor still runs where the test needs it.
func stubDoctorAck(t *testing.T) {
	t.Helper()
	old := confirmDoctorContinue
	confirmDoctorContinue = func() {}
	t.Cleanup(func() { confirmDoctorContinue = old })
}

func TestPostInstallOpensInterfaceWithoutBoard(t *testing.T) {
	previousCheck := checkStartupCopy
	checkStartupCopy = func() (bool, int) { t.Fatal("post-install startup repeated PATH confirmation"); return true, 1 }
	t.Cleanup(func() { checkStartupCopy = previousCheck })
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	installtest.IsolateAgentConfigEnv(t)
	t.Setenv(config.EnvLang, "cn")
	t.Setenv(install.EnvSkipInstall, "1")
	_ = os.Unsetenv(board.EnvBoardDir)
	stampStartupVersion(t)

	cfgPath := filepath.Join(home, ".config", "kander", "config.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvConfig, cfgPath)
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Language = "cn"
	payload, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	config.ApplyLanguageArgument(nil)
	config.BindConfigLanguage(nil)

	cwd := t.TempDir()
	t.Chdir(cwd)

	t.Setenv(install.EnvPostInstall, "1")
	origTTY := isInteractiveTerminal
	isInteractiveTerminal = func() bool { return true }
	t.Cleanup(func() { isInteractiveTerminal = origTTY })

	var captured *App
	origRun := runBoardTUI
	runBoardTUI = func(app *App) error {
		captured = app
		return nil
	}
	t.Cleanup(func() { runBoardTUI = origRun })

	bindStartupService(t)
	if code := Run(); code != 0 {
		t.Fatalf("Run exit=%d", code)
	}
	if os.Getenv(install.EnvPostInstall) != "" {
		t.Fatal("KANDER_POST_INSTALL should be cleared before any child can inherit it")
	}
	if captured == nil || captured.Options == nil {
		t.Fatal("expected options panel")
	}
	if captured.Options.initial != sectionInterface && captured.Options.current != sectionInterface {
		t.Fatalf("want interface section, initial=%q current=%q", captured.Options.initial, captured.Options.current)
	}
	if captured.shouldShowWelcome() {
		t.Fatal("welcome must wait until the first-launch options close")
	}
	captured.Options.close()
	if captured.Options != nil {
		t.Fatal("options close failed")
	}
	if !captured.shouldShowWelcome() {
		t.Fatal("empty board should show welcome after options close")
	}
}

// TestPostInstallFirstLaunchCreatesConfigInInstallLanguage covers a fresh install:
// the handoff carries the wizard language only as KANDER_LANG, and the config
// doctor creates must still use it without leaving a CLI override behind.
func TestPostInstallFirstLaunchCreatesConfigInInstallLanguage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	installtest.IsolateAgentConfigEnv(t)
	// An empty PATH makes every probed agent unavailable without running any CLI.
	t.Setenv("PATH", t.TempDir())
	t.Setenv(config.EnvLang, "ja")
	t.Setenv(config.EnvLangCLI, "")
	t.Setenv(install.EnvSkipInstall, "1")
	_ = os.Unsetenv(board.EnvBoardDir)
	cfgPath := filepath.Join(home, ".config", "kander", "config.json")
	t.Setenv(config.EnvConfig, cfgPath)
	config.ApplyLanguageArgument(nil)
	config.BindConfigLanguage(nil)
	t.Cleanup(func() { config.BindConfigLanguage(nil) })
	t.Chdir(t.TempDir())

	stubDoctorAck(t)
	t.Setenv(install.EnvPostInstall, "1")
	origTTY := isInteractiveTerminal
	isInteractiveTerminal = func() bool { return true }
	t.Cleanup(func() { isInteractiveTerminal = origTTY })
	origRun := runBoardTUI
	runBoardTUI = func(*App) error { return nil }
	t.Cleanup(func() { runBoardTUI = origRun })

	bindStartupService(t)
	if code := Run(); code != 0 {
		t.Fatalf("Run exit=%d", code)
	}
	created, err := config.LoadScope(true)
	if err != nil {
		t.Fatal(err)
	}
	if created.Language != "ja" || created.AgentLanguage != "ja" {
		t.Fatalf("created config language=%q agent_language=%q want ja", created.Language, created.AgentLanguage)
	}
	if lang := config.CLILanguage(); lang != "" || os.Getenv(config.EnvLangCLI) != "" {
		t.Fatalf("CLI language override left behind: %q", lang)
	}
	if got := config.ResolveLanguage(); got != "ja" {
		t.Fatalf("first screen language=%q want ja", got)
	}
	config.BindConfigLanguage(&config.Config{Language: "en"})
	if got := config.ResolveLanguage(); got != "en" {
		t.Fatalf("a later config language change resolved %q want en", got)
	}
}

// TestMissingConfigOpensInterfaceWithoutWizard covers a bare first launch with no
// scope config: skip the install wizard and PATH copy prompt, tolerate a missing
// board, run doctor, and open the interface options section.
func TestMissingConfigOpensInterfaceWithoutWizard(t *testing.T) {
	previousCheck := checkStartupCopy
	checkStartupCopy = func() (bool, int) {
		t.Fatal("missing-config startup must not offer PATH confirmation")
		return true, 1
	}
	t.Cleanup(func() { checkStartupCopy = previousCheck })

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	installtest.IsolateAgentConfigEnv(t)
	t.Setenv("PATH", t.TempDir())
	t.Setenv(config.EnvLang, "cn")
	_ = os.Unsetenv(install.EnvSkipInstall)
	_ = os.Unsetenv(install.EnvPostInstall)
	_ = os.Unsetenv(board.EnvBoardDir)

	cfgPath := filepath.Join(home, ".config", "kander", "config.json")
	t.Setenv(config.EnvConfig, cfgPath)
	config.ApplyLanguageArgument(nil)
	config.BindConfigLanguage(nil)
	t.Cleanup(func() { config.BindConfigLanguage(nil) })
	t.Chdir(t.TempDir())
	stubDoctorAck(t)

	origTTY := isInteractiveTerminal
	isInteractiveTerminal = func() bool { return true }
	t.Cleanup(func() { isInteractiveTerminal = origTTY })

	var captured *App
	origRun := runBoardTUI
	runBoardTUI = func(app *App) error {
		captured = app
		return nil
	}
	t.Cleanup(func() { runBoardTUI = origRun })

	bindStartupService(t)
	if code := Run(); code != 0 {
		t.Fatalf("Run exit=%d", code)
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("doctor should create config: %v", err)
	}
	if captured == nil || captured.Options == nil {
		t.Fatal("expected options panel")
	}
	if captured.Options.initial != sectionInterface && captured.Options.current != sectionInterface {
		t.Fatalf("want interface section, initial=%q current=%q", captured.Options.initial, captured.Options.current)
	}
	if captured.shouldShowWelcome() {
		t.Fatal("welcome must wait until the missing-config options close")
	}
	captured.Options.close()
	if !captured.shouldShowWelcome() {
		t.Fatal("empty board should show welcome after options close")
	}
}

// TestExistingConfigWithEmptyServiceShowsWelcome covers a normal launch with a
// scope config already present but no kanban/ in the CWD: the TUI must still
// start and show the empty-board welcome instead of exiting.
func TestExistingConfigWithEmptyServiceShowsWelcome(t *testing.T) {
	previousCheck := checkStartupCopy
	checkStartupCopy = func() (bool, int) { return false, 0 }
	t.Cleanup(func() { checkStartupCopy = previousCheck })

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	installtest.IsolateAgentConfigEnv(t)
	t.Setenv(config.EnvLang, "cn")
	t.Setenv(install.EnvSkipInstall, "1")
	_ = os.Unsetenv(install.EnvPostInstall)
	_ = os.Unsetenv(board.EnvBoardDir)

	cfgPath := filepath.Join(home, ".config", "kander", "config.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvConfig, cfgPath)
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Language = "cn"
	payload, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	config.ApplyLanguageArgument(nil)
	config.BindConfigLanguage(nil)
	stampStartupVersion(t)

	t.Chdir(t.TempDir())

	origTTY := isInteractiveTerminal
	isInteractiveTerminal = func() bool { return true }
	t.Cleanup(func() { isInteractiveTerminal = origTTY })

	var captured *App
	origRun := runBoardTUI
	runBoardTUI = func(app *App) error {
		captured = app
		return nil
	}
	t.Cleanup(func() { runBoardTUI = origRun })

	bindStartupService(t)
	if code := Run(); code != 0 {
		t.Fatalf("Run exit=%d", code)
	}
	if captured == nil {
		t.Fatal("expected board app")
	}
	if captured.Options != nil {
		t.Fatal("existing config must not auto-open options")
	}
	if !captured.shouldShowWelcome() {
		t.Fatal("missing board with an existing config should show welcome")
	}
	if captured.missingBoard || captured.StartChat == nil || !strings.HasPrefix(captured.boardRoot, "kander-server:") {
		t.Fatal("an empty service project must remain bound without a local board")
	}
	captured.HandleKey("c")
	if captured.BoardInit != nil || captured.Chat == nil {
		t.Fatal("c must open chat without offering local board initialization")
	}
	if _, err := captured.GetTask("any"); err == nil {
		t.Fatal("detail lookup must refuse without a board")
	}
}

// TestExistingConfigDoesNotOpenOptions keeps the normal board entry when a scope
// config already exists and this is not a post-install handoff.
func TestExistingConfigDoesNotOpenOptions(t *testing.T) {
	previousCheck := checkStartupCopy
	checkStartupCopy = func() (bool, int) { return false, 0 }
	t.Cleanup(func() { checkStartupCopy = previousCheck })

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	installtest.IsolateAgentConfigEnv(t)
	t.Setenv(config.EnvLang, "cn")
	t.Setenv(install.EnvSkipInstall, "1")
	_ = os.Unsetenv(install.EnvPostInstall)
	_ = os.Unsetenv(board.EnvBoardDir)

	cfgPath := filepath.Join(home, ".config", "kander", "config.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvConfig, cfgPath)
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Language = "cn"
	payload, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	config.ApplyLanguageArgument(nil)
	config.BindConfigLanguage(nil)
	stampStartupVersion(t)

	cwd := t.TempDir()
	t.Chdir(cwd)
	if _, err := os.Lstat(filepath.Join(cwd, "kanban")); !os.IsNotExist(err) {
		t.Fatal("fixture unexpectedly has a local board")
	}

	origTTY := isInteractiveTerminal
	isInteractiveTerminal = func() bool { return true }
	t.Cleanup(func() { isInteractiveTerminal = origTTY })

	var captured *App
	origRun := runBoardTUI
	runBoardTUI = func(app *App) error {
		captured = app
		return nil
	}
	t.Cleanup(func() { runBoardTUI = origRun })

	bindStartupService(t)
	if code := Run(); code != 0 {
		t.Fatalf("Run exit=%d", code)
	}
	if captured == nil {
		t.Fatal("expected board app")
	}
	if captured.Options != nil {
		t.Fatal("existing config must not auto-open options")
	}
	if !captured.shouldShowWelcome() {
		t.Fatal("empty board with an existing config should show welcome")
	}
}

// startupGateTestEnv builds the common fixture: temp HOME, an existing config,
// interactive TTY stubs, and stubbed doctor/ack hooks. It returns flags recording
// whether the startup doctor and the report acknowledgement ran.
func startupGateTestEnv(t *testing.T) (doctorRan, ackRan *bool) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	installtest.IsolateAgentConfigEnv(t)
	t.Setenv(install.EnvSkipInstall, "1")
	_ = os.Unsetenv(board.EnvBoardDir)
	cfgPath := filepath.Join(home, ".config", "kander", "config.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvConfig, cfgPath)
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	payload, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	config.ApplyLanguageArgument(nil)
	config.BindConfigLanguage(nil)
	t.Chdir(t.TempDir())

	origTTY := isInteractiveTerminal
	isInteractiveTerminal = func() bool { return true }
	t.Cleanup(func() { isInteractiveTerminal = origTTY })
	origRun := runBoardTUI
	runBoardTUI = func(*App) error { return nil }
	t.Cleanup(func() { runBoardTUI = origRun })

	doctorRan, ackRan = new(bool), new(bool)
	origDoctor := runStartupDoctor
	runStartupDoctor = func(bool) { *doctorRan = true }
	t.Cleanup(func() { runStartupDoctor = origDoctor })
	origAck := confirmDoctorContinue
	confirmDoctorContinue = func() { *ackRan = true }
	t.Cleanup(func() { confirmDoctorContinue = origAck })
	return doctorRan, ackRan
}

// A version refresh that ran, to the end or to a failure, shows the doctor report and waits
// for the acknowledgement; one that did not run opens the board directly.
func TestStartupVersionGateRunsDoctorAfterRefresh(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outcome menu.VersionRefresh
		doctor  bool
	}{
		{"done", menu.VersionRefresh{Status: menu.VersionRefreshDone, Elapsed: time.Second}, true},
		{"failed", menu.VersionRefresh{Status: menu.VersionRefreshFailed, Elapsed: time.Second, Failures: []string{"x"}}, true},
		{"current", menu.VersionRefresh{Status: menu.VersionRefreshCurrent}, false},
		{"busy", menu.VersionRefresh{Status: menu.VersionRefreshBusy}, false},
		{"check failed", menu.VersionRefresh{Status: menu.VersionRefreshCheckFailed, Failures: []string{"x"}}, false},
		{"not the global entry", menu.VersionRefresh{Status: menu.VersionRefreshNotGlobalEntry, Running: "dev"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doctorRan, ackRan := startupGateTestEnv(t)
			stubVersionRefresh(t, tc.outcome)
			bindStartupService(t)
			if code := Run(); code != 0 {
				t.Fatalf("Run exit=%d", code)
			}
			if *doctorRan != tc.doctor || *ackRan != tc.doctor {
				t.Fatalf("doctor=%v ack=%v, want both %v", *doctorRan, *ackRan, tc.doctor)
			}
		})
	}
}

func TestUnconfiguredTUIDoesNotOpenLocalBoard(t *testing.T) {
	startupGateTestEnv(t)
	stampStartupVersion(t)
	root := filepath.Join(t.TempDir(), "forbidden-board")
	t.Setenv(board.EnvBoardDir, root)
	called := false
	old := runBoardTUI
	runBoardTUI = func(*App) error { called = true; return nil }
	t.Cleanup(func() { runBoardTUI = old })
	if code := Run(); code == 0 || called {
		t.Fatal("unconfigured TUI opened a board")
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatal("unconfigured TUI used KANBAN_DIR")
	}
}
