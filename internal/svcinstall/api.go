package svcinstall

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/process"
)

// Installed is the definition found at the location of a service.
type Installed struct {
	Kind     Kind
	Manager  string
	Location string
	// Managed is false for a definition without the Kander marker, which Kander never changes.
	Managed bool
	// Spec is what the marker of a managed definition records.
	Spec Spec
}

// Manager names the service manager of this host: systemd, launchd, schtasks, or none.
func Manager() string { return current().name() }

// Location is where the definition of k lives on this host.
func Location(k Kind) string { return current().location(k) }

// Available reports why no service can be installed on this host, nil when one can.
func Available(ctx context.Context) error { return current().available(ctx) }

// Lookup finds the definition of k. found is false when there is none.
func Lookup(ctx context.Context, k Kind) (Installed, bool, error) {
	p := current()
	data, found, err := p.read(ctx, k)
	if err != nil || !found {
		return Installed{}, false, err
	}
	inst := Installed{Kind: k, Manager: p.name(), Location: p.location(k)}
	if m, ok := decodeMarker(data); ok && m.Kind == k {
		inst.Managed, inst.Spec = true, m.Spec
	}
	return inst, true, nil
}

// ManagedFor returns the managed definition of k that serves the client home of this process:
// the one the service manager runs in its place. It is false for a process that itself runs in
// managed mode, for a missing or foreign definition, and for a definition installed for another
// client home. Both client homes unset counts as the same home.
func ManagedFor(ctx context.Context, k Kind) (Installed, bool, error) {
	home, err := config.ClientHome()
	if err != nil {
		return Installed{}, false, err
	}
	return ManagedForHome(ctx, k, home)
}

// ManagedForHome is ManagedFor for the client home home.
func ManagedForHome(ctx context.Context, k Kind, home string) (Installed, bool, error) {
	if process.Managed() {
		return Installed{}, false, nil
	}
	inst, found, err := Lookup(ctx, k)
	if err != nil || !found || !inst.Managed {
		return Installed{}, false, err
	}
	if !ServesHome(inst.Spec, home) {
		return Installed{}, false, nil
	}
	return inst, true, nil
}

// SpecHome is the client home of a definition: the one it names, or, when it names none, the
// default client home, which the service process resolves the same way.
func SpecHome(s Spec) (string, error) {
	if s.ClientHome != "" {
		return s.ClientHome, nil
	}
	paths, err := config.GlobalInstallPaths()
	if err != nil {
		return "", err
	}
	return filepath.Join(paths.ShareDir, "client"), nil
}

// ServesHome reports whether a definition belongs to the client home home.
func ServesHome(s Spec, home string) bool {
	own, err := SpecHome(s)
	return err == nil && samePath(own, home)
}

// ManagerStops reports whether the service manager stops a service itself; on Windows the
// caller stops the process through the Kander protocol (ErrProtocolStop).
func ManagerStops() bool {
	_, protocol := current().(schtasks)
	return !protocol
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// ForeignError reports a definition at the location of a service that Kander did not write.
type ForeignError struct{ Location string }

func (e *ForeignError) Error() string { return config.Text("service.foreign_definition", e.Location) }

// Prepared is a rendered definition compared with the installed one.
type Prepared struct {
	Kind     Kind
	Location string
	// Found reports an installed managed definition; Changed that it differs from the rendered one.
	Found, Changed bool
	// Previous is the Spec the installed managed definition records.
	Previous Spec
	data     []byte
}

// Prepare renders the definition of s and compares it with the installed one. A definition
// without the Kander marker is refused with a *ForeignError, and nothing is changed.
func Prepare(ctx context.Context, s Spec) (Prepared, error) {
	p := current()
	data, err := p.render(s)
	if err != nil {
		return Prepared{}, err
	}
	installed, found, err := p.read(ctx, s.Kind)
	if err != nil {
		return Prepared{}, err
	}
	prepared := Prepared{Kind: s.Kind, Location: p.location(s.Kind), Found: found, Changed: true, data: data}
	if found {
		m, ok := decodeMarker(installed)
		if !ok || m.Kind != s.Kind {
			return Prepared{}, &ForeignError{Location: prepared.Location}
		}
		prepared.Previous, prepared.Changed = m.Spec, !p.same(installed, data)
	}
	return prepared, nil
}

// Write publishes the prepared definition and registers it, without starting the service.
func (p Prepared) Write(ctx context.Context) error {
	if p.data == nil {
		return errors.New("svcinstall: nothing prepared")
	}
	return current().write(ctx, p.Kind, p.data)
}

// Start starts the service; one that runs is left alone.
func Start(ctx context.Context, k Kind) error { return current().start(ctx, k) }

// Stop stops the service, or returns ErrProtocolStop where the caller stops it.
func Stop(ctx context.Context, k Kind) error { return current().stop(ctx, k) }

// Restart restarts the service with its current definition, or returns ErrProtocolStop.
func Restart(ctx context.Context, k Kind) error { return current().restart(ctx, k) }

// Remove stops the service where the manager stops it, unregisters it, and deletes its
// definition. The caller checked that the definition is managed.
func Remove(ctx context.Context, k Kind) error { return current().remove(ctx, k) }

// StateOf asks the service manager about k.
func StateOf(ctx context.Context, k Kind) (State, error) { return current().state(ctx, k) }

// Linger reports whether systemd keeps the user manager running without a login, which a
// service needs to start at boot before anyone logs in. applies is false on another manager;
// known is false when loginctl cannot tell.
func Linger(ctx context.Context) (enabled, known, applies bool) {
	s, ok := current().(systemd)
	if !ok {
		return false, false, false
	}
	enabled, known = s.linger(ctx)
	return enabled, known, true
}

// LingerUser is the user name the linger hint names.
func LingerUser() string {
	if s, ok := current().(systemd); ok {
		return s.user
	}
	return ""
}
