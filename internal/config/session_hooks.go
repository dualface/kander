package config

import "strings"

const sessionHookPrefix = "hook:"

// SessionHook is a named Go capability that argv templates cannot express.
type SessionHook struct {
	Name string
	// DiscoverAfterStart snapshots existing sessions and waits for a new id after tmux launch.
	DiscoverAfterStart bool
	// ResolveEmptyReference scans for an existing session when SESSION has no id.
	ResolveEmptyReference bool
	// AllocateBeforeStart obtains a session id before the agent process starts.
	AllocateBeforeStart bool
}

var registeredSessionHooks = []SessionHook{
	{Name: "codex-rollout", DiscoverAfterStart: true, ResolveEmptyReference: true},
	{Name: "cursor-create-chat", AllocateBeforeStart: true},
}

// RegisteredSessionHooks returns the built-in hook list in registration order.
func RegisteredSessionHooks() []SessionHook {
	return append([]SessionHook(nil), registeredSessionHooks...)
}

// ParseSessionHook reports the hook name when mode is hook:<name>.
func ParseSessionHook(mode string) (string, bool) {
	if !strings.HasPrefix(mode, sessionHookPrefix) {
		return "", false
	}
	name := mode[len(sessionHookPrefix):]
	if name == "" || strings.Contains(name, ":") {
		return "", false
	}
	return name, true
}

// LookupSessionHook resolves a registered hook from session.mode.
func LookupSessionHook(mode string) (SessionHook, bool) {
	name, ok := ParseSessionHook(mode)
	if !ok {
		return SessionHook{}, false
	}
	for _, hook := range registeredSessionHooks {
		if hook.Name == name {
			return hook, true
		}
	}
	return SessionHook{}, false
}

func validDeclaredSessionMode(mode string) bool {
	if contains([]string{"generated", "allocated", "none"}, mode) {
		return true
	}
	_, ok := LookupSessionHook(mode)
	return ok
}

func SessionDiscoversAfterStart(mode string) bool {
	hook, ok := LookupSessionHook(mode)
	return ok && hook.DiscoverAfterStart
}

func SessionResolvesEmptyReference(mode string) bool {
	hook, ok := LookupSessionHook(mode)
	return ok && hook.ResolveEmptyReference
}

func SessionAllocatesBeforeStart(mode string) bool {
	hook, ok := LookupSessionHook(mode)
	return ok && hook.AllocateBeforeStart
}
