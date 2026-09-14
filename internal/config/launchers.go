package config

import "sync"

// defaultLauncherNames is the built-in launcher set. It stays valid whether or
// not a terminal backend registered anything, so this package keeps its
// validation behavior when used alone or in single-package tests.
var defaultLauncherNames = []string{"auto", "tmux", "tmux-session", "herdr", "foreground", "console"}

var launcherRegistry struct {
	sync.Mutex
	names   []string
	sources []func() []string
}

// DefaultLauncherNames returns the built-in launcher set.
func DefaultLauncherNames() []string {
	return append([]string{}, defaultLauncherNames...)
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

// RegisterLauncherNameSource adds a function that supplies launcher names when
// they are needed. The terminal layer uses it for definitions loaded lazily
// from share directories, which cannot be known at package init. A source must
// not call back into launcher name validation.
func RegisterLauncherNameSource(source func() []string) {
	launcherRegistry.Lock()
	defer launcherRegistry.Unlock()
	launcherRegistry.sources = append(launcherRegistry.sources, source)
}

// RegisteredLauncherNames returns only the explicitly registered names, in
// registration order.
func RegisteredLauncherNames() []string {
	launcherRegistry.Lock()
	defer launcherRegistry.Unlock()
	return append([]string{}, launcherRegistry.names...)
}

// LauncherNames returns every valid launcher name: the built-in defaults in
// their fixed order, followed by registered names and then source-supplied
// names that are not already listed.
func LauncherNames() []string {
	launcherRegistry.Lock()
	out := append([]string{}, defaultLauncherNames...)
	registered := append([]string{}, launcherRegistry.names...)
	sources := append([]func() []string{}, launcherRegistry.sources...)
	launcherRegistry.Unlock()
	// Sources run outside the lock: they may read files and configuration paths.
	for _, source := range sources {
		registered = append(registered, source()...)
	}
	for _, name := range registered {
		if name != "" && !contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

// ValidLauncherName reports whether name is a default or registered launcher.
func ValidLauncherName(name string) bool {
	return contains(LauncherNames(), name)
}
