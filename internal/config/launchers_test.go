package config

import (
	"reflect"
	"testing"
)

func resetLauncherRegistry(t *testing.T) {
	t.Helper()
	launcherRegistry.Lock()
	saved := launcherRegistry.names
	launcherRegistry.names = nil
	launcherRegistry.Unlock()
	t.Cleanup(func() {
		launcherRegistry.Lock()
		launcherRegistry.names = saved
		launcherRegistry.Unlock()
	})
}

func TestLauncherNamesDefaultWithoutRegistration(t *testing.T) {
	resetLauncherRegistry(t)
	want := []string{"auto", "tmux", "tmux-session", "herdr", "foreground", "console"}
	if got := LauncherNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("LauncherNames()=%q, want %q", got, want)
	}
	if got := RegisteredLauncherNames(); len(got) != 0 {
		t.Fatalf("RegisteredLauncherNames()=%q, want none", got)
	}
	for _, name := range want {
		if !ValidLauncherName(name) {
			t.Fatalf("default launcher %q rejected", name)
		}
	}
	if _, err := validateLauncher("tmux"); err != nil {
		t.Fatalf("default launcher rejected by config validation: %v", err)
	}
	if _, err := validateLauncher("wezterm"); err == nil {
		t.Fatal("unregistered launcher accepted")
	}
}

func TestRegisterLauncherNamesAddsNames(t *testing.T) {
	resetLauncherRegistry(t)
	RegisterLauncherNames("wezterm")
	if !ValidLauncherName("wezterm") {
		t.Fatal("registered launcher rejected")
	}
	if _, err := validateLauncher("wezterm"); err != nil {
		t.Fatalf("registered launcher rejected by config validation: %v", err)
	}
	want := []string{"auto", "tmux", "tmux-session", "herdr", "foreground", "console", "wezterm"}
	if got := LauncherNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("LauncherNames()=%q, want %q", got, want)
	}
}

func TestRegisterLauncherNamesIsIdempotent(t *testing.T) {
	resetLauncherRegistry(t)
	RegisterLauncherNames("tmux", "wezterm", "")
	RegisterLauncherNames("wezterm", "tmux")
	if got, want := RegisteredLauncherNames(), []string{"tmux", "wezterm"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("RegisteredLauncherNames()=%q, want %q", got, want)
	}
	want := []string{"auto", "tmux", "tmux-session", "herdr", "foreground", "console", "wezterm"}
	if got := LauncherNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("LauncherNames()=%q, want %q", got, want)
	}
}
