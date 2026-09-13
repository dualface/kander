package terminal

import (
	"sync"

	"github.com/dualface/kander/internal/config"
)

// Auto is the launcher name resolved at start time rather than a backend.
const Auto = "auto"

var registry struct {
	sync.RWMutex
	order    []string
	backends map[string]Backend
}

func init() {
	config.RegisterLauncherNames(Auto)
}

// Register adds a backend and registers its launcher name with configuration
// validation. Registering the same name again replaces nothing and is a no-op.
func Register(backend Backend) {
	registry.Lock()
	defer registry.Unlock()
	if registry.backends == nil {
		registry.backends = map[string]Backend{}
	}
	name := backend.Name()
	if _, exists := registry.backends[name]; exists {
		return
	}
	registry.backends[name] = backend
	registry.order = append(registry.order, name)
	config.RegisterLauncherNames(name)
}

// Lookup returns the backend registered under a launcher name.
func Lookup(name string) (Backend, bool) {
	registry.RLock()
	defer registry.RUnlock()
	backend, ok := registry.backends[name]
	return backend, ok
}

// Backends returns the registered backends in registration order.
func Backends() []Backend {
	registry.RLock()
	defer registry.RUnlock()
	out := make([]Backend, 0, len(registry.order))
	for _, name := range registry.order {
		out = append(out, registry.backends[name])
	}
	return out
}

// ParseWindow finds the container backend that owns a WINDOW value.
func ParseWindow(value string) (Backend, Address, bool) {
	for _, backend := range Backends() {
		if !backend.Capabilities().Container {
			continue
		}
		if address, ok := backend.ParseAddress(value); ok {
			return backend, address, true
		}
	}
	return nil, Address{}, false
}

// ResolveAuto returns the first registered backend that auto resolution
// selects on this platform; ok is false when none applies.
func ResolveAuto(windows bool, getenv func(string) string) (Backend, bool) {
	for _, backend := range Backends() {
		if windows && backend.Capabilities().POSIXOnly {
			continue
		}
		if backend.AutoDetect(getenv) {
			return backend, true
		}
	}
	return nil, false
}

// HasCapability reports whether the named launcher has a capability; unknown
// names (including auto) have none.
func HasCapability(name string, has func(Capabilities) bool) bool {
	backend, ok := Lookup(name)
	return ok && has(backend.Capabilities())
}

// FindBackend returns the first registered backend whose capabilities match.
func FindBackend(match func(Capabilities) bool) (Backend, bool) {
	for _, backend := range Backends() {
		if match(backend.Capabilities()) {
			return backend, true
		}
	}
	return nil, false
}
