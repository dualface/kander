package config

import "sync"

// defaultLauncherNames is the built-in launcher set. It stays valid whether or
// not a terminal backend registered anything, so this package keeps its
// validation behavior when used alone or in single-package tests.
var defaultLauncherNames = []string{"auto", "tmux", "tmux-session", "herdr", "foreground", "console"}

var launcherRegistry struct {
	sync.Mutex
	names []string
}

// RegisterLauncherNames adds launcher names that configuration validation
// accepts. The terminal layer calls it from package init, before any
// configuration is validated; repeated names are ignored. This package never
// imports the terminal layer.
func RegisterLauncherNames(names ...string) {
	launcherRegistry.Lock()
	defer launcherRegistry.Unlock()
	for _, name := range names {
		if name == "" || contains(launcherRegistry.names, name) {
			continue
		}
		launcherRegistry.names = append(launcherRegistry.names, name)
	}
}

// RegisteredLauncherNames returns only the explicitly registered names, in
// registration order.
func RegisteredLauncherNames() []string {
	launcherRegistry.Lock()
	defer launcherRegistry.Unlock()
	return append([]string{}, launcherRegistry.names...)
}

// LauncherNames returns every valid launcher name: the built-in defaults in
// their fixed order, followed by registered names that are not defaults.
func LauncherNames() []string {
	launcherRegistry.Lock()
	defer launcherRegistry.Unlock()
	out := append([]string{}, defaultLauncherNames...)
	for _, name := range launcherRegistry.names {
		if !contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

// ValidLauncherName reports whether name is a default or registered launcher.
func ValidLauncherName(name string) bool {
	return contains(LauncherNames(), name)
}
