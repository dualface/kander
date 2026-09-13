package terminal

import (
	"context"
	"fmt"
	"sync"
)

// HookStatus is the three-state outcome of a hook.
type HookStatus int

const (
	// HookOK: the hook did its work.
	HookOK HookStatus = iota + 1
	// HookDegraded: the hook could not do its work, and the operation
	// continues in a reduced form described by the note.
	HookDegraded
	// HookFailed: the operation fails with Err.
	HookFailed
)

// HookResult is what a hook returns to the operation that called it.
type HookResult struct {
	Status HookStatus
	// Note explains a degraded result.
	Note string
	// Err is the failure of a failed result.
	Err error
}

// HookCall is the context a hook receives: the launcher and mount point, the
// resolved executable and runner, the environment, and the operation inputs
// by placeholder name (pane, container, session, agent, reference).
type HookCall struct {
	Launcher string
	Point    string
	Conn     Conn
	Getenv   func(string) string
	Values   map[string]string
	// Report is set at the report_session mount point.
	Report *SessionReport
}

// Hook is Go code a definition binds to a mount point when argv templates
// cannot express the step (for example a socket protocol).
type Hook func(ctx context.Context, call HookCall) HookResult

var hookRegistry struct {
	sync.RWMutex
	hooks map[string]Hook
}

// RegisterHook records a hook under a name. Hooks are registered from package
// initialization, before any definition that names them is loaded; a
// duplicate name is a programming error.
func RegisterHook(name string, hook Hook) {
	hookRegistry.Lock()
	defer hookRegistry.Unlock()
	if hookRegistry.hooks == nil {
		hookRegistry.hooks = map[string]Hook{}
	}
	if _, exists := hookRegistry.hooks[name]; exists {
		panic(fmt.Sprintf("terminal: hook %q registered twice", name))
	}
	hookRegistry.hooks[name] = hook
}

// LookupHook returns the hook registered under name.
func LookupHook(name string) (Hook, bool) {
	hookRegistry.RLock()
	defer hookRegistry.RUnlock()
	hook, ok := hookRegistry.hooks[name]
	return hook, ok
}

// runHook calls a hook and normalizes a result without a known status into a
// failure, so a hook bug never reads as success.
func runHook(ctx context.Context, hook Hook, call HookCall) HookResult {
	result := hook(ctx, call)
	switch result.Status {
	case HookOK, HookDegraded:
		return result
	case HookFailed:
		if result.Err == nil {
			result.Err = fmt.Errorf("terminal: hook at %s failed without an error", call.Point)
		}
		return result
	default:
		return HookResult{Status: HookFailed, Err: fmt.Errorf("terminal: hook at %s returned an unknown status %d", call.Point, result.Status)}
	}
}
