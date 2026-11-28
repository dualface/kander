// Package svcinstall writes, reads, and drives the service definitions that register the
// Kander server and worker with the service manager of the host: a systemd user unit on Linux,
// a launchd LaunchAgent on macOS, and a scheduled task on Windows. It knows nothing about the
// server or the worker themselves: the caller supplies the command line, and the packages that
// own those processes decide when to call it. Every call of a service manager goes through a
// Runner that tests replace, and a test binary never reads or changes the real system.
package svcinstall

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/config"
)

// Kind names one of the two services.
type Kind string

const (
	KindServer Kind = "server"
	KindWorker Kind = "worker"
)

// Kinds lists the services in the order status and uninstall show them.
var Kinds = []Kind{KindServer, KindWorker}

// ParseKind accepts the name of a service.
func ParseKind(name string) (Kind, bool) {
	switch Kind(name) {
	case KindServer, KindWorker:
		return Kind(name), true
	}
	return "", false
}

// Spec is everything a service definition carries. The definition sets KANDER_MANAGED=1, and
// KANDER_CLIENT_HOME only when ClientHome is not empty.
type Spec struct {
	Kind Kind `json:"kind"`
	// Executable is the global entry, the kander users run from PATH.
	Executable string `json:"executable"`
	// Args follow the executable.
	Args []string `json:"args"`
	// ClientHome is the KANDER_CLIENT_HOME the installing command ran with; empty when unset.
	ClientHome string `json:"client_home,omitempty"`
	// WorkDir is the working directory: the data directory of a server, the client home of a
	// worker, as for a detached start.
	WorkDir string `json:"work_dir"`
	// LogPath is the file standard output and standard error are appended to.
	LogPath string `json:"log"`
}

// Flag returns the value that follows name in the arguments, and whether it is there.
func (s Spec) Flag(name string) (string, bool) {
	for i, arg := range s.Args {
		if arg == name && i+1 < len(s.Args) {
			return s.Args[i+1], true
		}
	}
	return "", false
}

// Runner runs one service manager command and returns its combined output.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// commandTimeout bounds one service manager command.
const commandTimeout = 15 * time.Second

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// commandError reports a failed service manager command with its command line and output.
func commandError(name string, args []string, output []byte, err error) error {
	text := strings.TrimSpace(string(output))
	if text == "" {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, text)
}

// State is what the service manager reports about a service.
type State struct {
	// Loaded reports whether the manager knows the service at all.
	Loaded bool `json:"loaded"`
	// Active is the manager's own word: active, inactive, failed, activating, running,
	// loaded, registered, and so on.
	Active string `json:"active"`
	// Running is true only when the manager says the process runs; a manager that cannot say
	// (Windows) leaves it false and the caller looks at the process itself.
	Running bool `json:"running"`
}

// ErrProtocolStop is returned by Stop and Restart on a manager that does not stop the service
// itself (Windows): the caller stops the process through the Kander protocol instead, the
// stop request of a worker or the authenticated stop of a server.
var ErrProtocolStop = errors.New("the service manager does not stop this service")

// platform is one service manager.
type platform interface {
	name() string
	// available reports why no service can be installed on this host.
	available(ctx context.Context) error
	// location is where the definition lives: a file path, or the name of a scheduled task.
	location(k Kind) string
	render(s Spec) ([]byte, error)
	// read returns the installed definition; found is false when there is none.
	read(ctx context.Context, k Kind) (data []byte, found bool, err error)
	// same reports whether the installed definition equals the rendered one.
	same(installed, rendered []byte) bool
	// write publishes the definition and registers it with the manager, without starting it.
	write(ctx context.Context, k Kind, data []byte) error
	// start starts the service; a running service is left alone.
	start(ctx context.Context, k Kind) error
	stop(ctx context.Context, k Kind) error
	restart(ctx context.Context, k Kind) error
	// remove stops the service where the manager stops it, unregisters it, and deletes the
	// definition.
	remove(ctx context.Context, k Kind) error
	state(ctx context.Context, k Kind) (State, error)
}

var (
	overrideMu sync.Mutex
	override   platform
)

// current returns the manager of this host. A test binary gets one that finds no definition
// and runs nothing, unless the test installed one with UseForTest.
func current() platform {
	overrideMu.Lock()
	defer overrideMu.Unlock()
	if override != nil {
		return override
	}
	if testing.Testing() {
		return inert{}
	}
	return detect(runtime.GOOS, runCommand)
}

func detect(goos string, run Runner) platform {
	home, _ := os.UserHomeDir()
	switch goos {
	case "linux":
		dir := os.Getenv("XDG_CONFIG_HOME")
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(home, ".config")
		}
		return systemd{dir: filepath.Join(dir, "systemd", "user"), run: run, user: currentUserName()}
	case "darwin":
		return launchd{dir: filepath.Join(home, "Library", "LaunchAgents"), uid: fmt.Sprint(os.Getuid()), run: run}
	case "windows":
		return schtasks{run: run, user: currentUserName(), temp: os.TempDir()}
	}
	return unsupported{goos: goos}
}

// TestPlatform describes the manager a test installs with UseForTest.
type TestPlatform struct {
	// GOOS picks the manager: linux, darwin, or windows.
	GOOS string
	// Dir holds the definition files: the systemd user unit directory or LaunchAgents.
	Dir string
	// Run replaces every service manager command.
	Run Runner
	// User is the user name (systemd linger, Windows principal); UID the launchd domain.
	User, UID string
	// Temp is the directory the Windows task XML is written to.
	Temp string
}

// UseForTest installs a manager for the rest of the test, so tests of other packages can drive
// the service paths without touching the real system.
func UseForTest(t testing.TB, p TestPlatform) {
	t.Helper()
	var chosen platform
	switch p.GOOS {
	case "linux":
		chosen = systemd{dir: p.Dir, run: p.Run, user: p.User}
	case "darwin":
		chosen = launchd{dir: p.Dir, uid: p.UID, run: p.Run}
	case "windows":
		chosen = schtasks{run: p.Run, user: p.User, temp: p.Temp}
	default:
		t.Fatalf("svcinstall: no test platform for %q", p.GOOS)
	}
	overrideMu.Lock()
	previous := override
	override = chosen
	overrideMu.Unlock()
	t.Cleanup(func() {
		overrideMu.Lock()
		override = previous
		overrideMu.Unlock()
	})
}

// inert is the manager of a test binary that installed none: no definition, no command.
type inert struct{}

var errInert = errors.New("svcinstall: no service manager in a test binary")

func (inert) name() string                                     { return "none" }
func (inert) available(context.Context) error                  { return errInert }
func (inert) location(k Kind) string                           { return string(k) }
func (inert) render(Spec) ([]byte, error)                      { return nil, errInert }
func (inert) read(context.Context, Kind) ([]byte, bool, error) { return nil, false, nil }
func (inert) same([]byte, []byte) bool                         { return false }
func (inert) write(context.Context, Kind, []byte) error        { return errInert }
func (inert) start(context.Context, Kind) error                { return errInert }
func (inert) stop(context.Context, Kind) error                 { return errInert }
func (inert) restart(context.Context, Kind) error              { return errInert }
func (inert) remove(context.Context, Kind) error               { return errInert }
func (inert) state(context.Context, Kind) (State, error)       { return State{}, nil }

// unsupported is the manager of an operating system Kander has no service support for.
type unsupported struct{ goos string }

func (u unsupported) err() error                                       { return errors.New(config.Text("service.unsupported_os", u.goos)) }
func (u unsupported) name() string                                     { return "none" }
func (u unsupported) available(context.Context) error                  { return u.err() }
func (u unsupported) location(k Kind) string                           { return string(k) }
func (u unsupported) render(Spec) ([]byte, error)                      { return nil, u.err() }
func (u unsupported) read(context.Context, Kind) ([]byte, bool, error) { return nil, false, nil }
func (u unsupported) same([]byte, []byte) bool                         { return false }
func (u unsupported) write(context.Context, Kind, []byte) error        { return u.err() }
func (u unsupported) start(context.Context, Kind) error                { return u.err() }
func (u unsupported) stop(context.Context, Kind) error                 { return u.err() }
func (u unsupported) restart(context.Context, Kind) error              { return u.err() }
func (u unsupported) remove(context.Context, Kind) error               { return u.err() }
func (u unsupported) state(context.Context, Kind) (State, error)       { return State{}, nil }
