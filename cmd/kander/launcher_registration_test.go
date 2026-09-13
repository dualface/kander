package main

import (
	"testing"

	"github.com/dualface/kander/internal/config"
)

// Package initialization of the full binary registers every terminal launcher
// before main (and any test in this package) can load or validate config.
func TestTerminalLaunchersRegisteredBeforeConfigValidation(t *testing.T) {
	registered := map[string]bool{}
	for _, name := range config.RegisteredLauncherNames() {
		registered[name] = true
	}
	for _, name := range []string{"auto", "herdr", "tmux", "tmux-session", "foreground", "console"} {
		if !registered[name] {
			t.Errorf("launcher %q was not registered by package initialization; registered=%q", name, config.RegisteredLauncherNames())
		}
		if !config.ValidLauncherName(name) {
			t.Errorf("launcher %q rejected by config validation", name)
		}
	}
}
