package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/install"
)

func TestPostInstallOpensInterfaceWithoutBoard(t *testing.T) {
	previousCheck := checkStartupCopy
	checkStartupCopy = func() (bool, int) { t.Fatal("post-install startup repeated PATH confirmation"); return true, 1 }
	t.Cleanup(func() { checkStartupCopy = previousCheck })
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(config.EnvLang, "cn")
	t.Setenv(install.EnvSkipInstall, "1")
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

	if code := Run(nil); code != 0 {
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

	t.Setenv(install.EnvPostInstall, "1")
	origTTY := isInteractiveTerminal
	isInteractiveTerminal = func() bool { return true }
	t.Cleanup(func() { isInteractiveTerminal = origTTY })
	origRun := runBoardTUI
	runBoardTUI = func(*App) error { return nil }
	t.Cleanup(func() { runBoardTUI = origRun })

	if code := Run(nil); code != 0 {
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

	if code := Run(nil); code != 0 {
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

// TestExistingConfigWithoutBoardShowsWelcome covers a normal launch with a
// scope config already present but no kanban/ in the CWD: the TUI must still
// start and show the empty-board welcome instead of exiting.
func TestExistingConfigWithoutBoardShowsWelcome(t *testing.T) {
	previousCheck := checkStartupCopy
	checkStartupCopy = func() (bool, int) { return false, 0 }
	t.Cleanup(func() { checkStartupCopy = previousCheck })

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
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

	if code := Run(nil); code != 0 {
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
	if !captured.missingBoard || captured.StartChat != nil {
		t.Fatal("a missing board must stay unbound until init")
	}
	captured.HandleKey("c")
	if captured.BoardInit == nil || captured.Chat != nil {
		t.Fatal("c must offer to initialize kanban/ instead of opening chat")
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

	cwd := t.TempDir()
	t.Chdir(cwd)
	if _, _, _, err := board.InitBoard(""); err != nil {
		t.Fatal(err)
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

	if code := Run(nil); code != 0 {
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
