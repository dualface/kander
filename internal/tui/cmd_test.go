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
}
