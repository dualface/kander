package menu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dualface/kander/internal/config"
)

// writeFakeExecutable only has to be discoverable by lookPath; it never runs.
func writeFakeExecutable(t *testing.T, dir, name string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".cmd"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func launcherValues(choices []Choice) []string {
	out := make([]string, 0, len(choices))
	for _, choice := range choices {
		out = append(out, choice.Value)
	}
	return out
}

func TestLauncherChoicesWindowsOffersHerdrWhenInstalled(t *testing.T) {
	orig := windowsOS
	windowsOS = true
	t.Cleanup(func() { windowsOS = orig })

	home := t.TempDir()
	cfgPath := filepath.Join(home, ".config", "kander", "config.json")
	t.Setenv("HOME", home)
	t.Setenv("KANDER_CONFIG", cfgPath)
	t.Setenv("KANDER_LANG", "cn")

	cfg := config.DefaultConfig()
	cfg.Launcher = "console"
	cfg.WelcomeComplete = true
	session := &Session{Config: cfg, existing: cfg}

	// Without herdr: Windows offers console and foreground only, never tmux.
	t.Setenv("PATH", t.TempDir())
	got := launcherValues(session.LauncherChoices())
	if len(got) != 2 || got[0] != "console" || got[1] != "foreground" {
		t.Fatalf("without herdr: %v", got)
	}

	// With herdr installed: it has a native Windows build, so it must be selectable.
	bin := t.TempDir()
	writeFakeExecutable(t, bin, "herdr")
	t.Setenv("PATH", bin)
	got = launcherValues(session.LauncherChoices())
	if len(got) != 3 || got[0] != "console" || got[1] != "herdr" || got[2] != "foreground" {
		t.Fatalf("with herdr: %v", got)
	}

	session.SetLauncher("herdr")
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved["launcher"] != "herdr" {
		t.Fatalf("%v", saved)
	}
}

// The first welcome normalization must only push tmux off Windows and keep an
// installed herdr; otherwise welcome quietly undoes herdr on Windows entirely.
func TestNormalizeLauncherWindowsOnlyDropsTmux(t *testing.T) {
	orig := windowsOS
	windowsOS = true
	t.Cleanup(func() { windowsOS = orig })

	bin := t.TempDir()
	writeFakeExecutable(t, bin, "herdr")
	t.Setenv("PATH", bin)

	fresh := func(launcher string) *config.Config {
		cfg := config.DefaultConfig()
		cfg.Launcher = launcher
		cfg.WelcomeComplete = false
		return cfg
	}
	for _, tc := range []struct{ launcher, want string }{
		{"herdr", "herdr"},
		{"auto", "auto"},
		{"console", "console"},
		{"foreground", "foreground"},
		{"tmux", "console"},
		{"tmux-session", "console"},
	} {
		cfg := fresh(tc.launcher)
		session := &Session{Config: cfg, existing: fresh(tc.launcher)}
		session.normalizeLauncher(cfg)
		if cfg.Launcher != tc.want {
			t.Fatalf("%s -> %s, want %s", tc.launcher, cfg.Launcher, tc.want)
		}
	}

	// Without herdr, Windows sends herdr/auto to console rather than tmux/foreground.
	t.Setenv("PATH", t.TempDir())
	for _, launcher := range []string{"herdr", "auto"} {
		cfg := fresh(launcher)
		session := &Session{Config: cfg, existing: fresh(launcher)}
		session.normalizeLauncher(cfg)
		if cfg.Launcher != "console" {
			t.Fatalf("%s without herdr -> %s", launcher, cfg.Launcher)
		}
	}
}
