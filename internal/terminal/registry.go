package terminal

import (
	"math"
	"sort"
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

// lookupGoBackend returns a backend registered as Go code.
func lookupGoBackend(name string) (Backend, bool) {
	registry.RLock()
	defer registry.RUnlock()
	backend, ok := registry.backends[name]
	return backend, ok
}

// Lookup returns the backend of a launcher name: a Go backend, or a launcher
// of the active terminal definitions.
func Lookup(name string) (Backend, bool) {
	if backend, ok := lookupGoBackend(name); ok {
		return backend, true
	}
	for _, backend := range definitionBackends() {
		if backend.Name() == name {
			return backend, true
		}
	}
	return nil, false
}

// Backends returns the Go backends in registration order, followed by the
// launchers of the active definitions.
func Backends() []Backend {
	registry.RLock()
	out := make([]Backend, 0, len(registry.order))
	for _, name := range registry.order {
		out = append(out, registry.backends[name])
	}
	registry.RUnlock()
	return append(out, definitionBackends()...)
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

// autoPrioritizer is implemented by definition launchers.
type autoPrioritizer interface {
	AutoPriority() int
}

// ResolveAuto returns the backend auto resolution selects on this platform;
// ok is false when none applies. Go backends keep registration order and come
// first; definition launchers follow in descending auto_priority.
func ResolveAuto(windows bool, getenv func(string) string) (Backend, bool) {
	backends := Backends()
	sort.SliceStable(backends, func(i, j int) bool {
		return autoPriority(backends[i]) > autoPriority(backends[j])
	})
	for _, backend := range backends {
		if windows && backend.Capabilities().POSIXOnly {
			continue
		}
		if backend.AutoDetect(getenv) {
			return backend, true
		}
	}
	return nil, false
}

// autoPriority ranks Go backends above every definition launcher.
func autoPriority(backend Backend) int {
	if prioritized, ok := backend.(autoPrioritizer); ok {
		return prioritized.AutoPriority()
	}
	return math.MaxInt
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
