// Package direct implements the launchers without a terminal container:
// foreground runs the agent in the current terminal, console starts it in a
// separate Windows console. Every container operation returns
// terminal.ErrUnsupported, and WINDOW records the bare launcher name.
package direct

import (
	"context"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

const (
	// Foreground occupies the current terminal until the agent exits.
	Foreground = "foreground"
	// Console starts the agent in a separate Windows console and returns.
	Console = "console"
)

// Backend is one direct launcher.
type Backend struct {
	name string
}

// New returns the foreground or console backend.
func New(name string) *Backend { return &Backend{name: name} }

var _ terminal.Backend = (*Backend)(nil)

func (b *Backend) Name() string          { return b.name }
func (b *Backend) Executable() string    { return "" }
func (b *Backend) VersionArgs() []string { return nil }

func (b *Backend) Capabilities() terminal.Capabilities {
	if b.name == Console {
		return terminal.Capabilities{Detached: true}
	}
	return terminal.Capabilities{}
}

func (b *Backend) Prepare(request terminal.PrepareRequest) (terminal.Target, error) {
	if b.name == Console {
		if !request.Windows {
			return terminal.Target{}, &probe.Error{Message: config.Text("launch.the_console_launcher_is_available_only_on_windows")}
		}
		return terminal.Target{Project: request.Project}, nil
	}
	if !request.TTY() {
		fallback := "tmux"
		if request.Windows {
			fallback = Console
		}
		return terminal.Target{}, &probe.Error{Message: config.Text(
			"launch.foreground_mode_requires_an_interactive_terminal_stdin_stdout_stderr", request.Command, fallback,
		)}
	}
	return terminal.Target{Project: request.Project}, nil
}

func (b *Backend) OpaqueAddress(terminal.Address) string        { return "" }
func (b *Backend) ParseAddress(string) (terminal.Address, bool) { return terminal.Address{}, false }
func (b *Backend) ParseFocusAddress([]string) (terminal.Address, bool) {
	return terminal.Address{}, false
}
func (b *Backend) AutoDetect(func(string) string) bool { return false }
func (b *Backend) SetSessionMarker(terminal.Conn, string, string) error {
	return terminal.ErrUnsupported
}
func (b *Backend) ReportSession(terminal.SessionReport) error { return terminal.ErrUnsupported }
func (b *Backend) WaitReady(terminal.Conn, string) error      { return terminal.ErrUnsupported }
func (b *Backend) RunCommand(terminal.Conn, string, string, bool) error {
	return terminal.ErrUnsupported
}

// StartedLines is rendered by the caller, which owns the process handle.
func (b *Backend) StartedLines(string, terminal.Target, terminal.Address, func(string) string) []string {
	return nil
}

func (b *Backend) CreateContainer(terminal.Conn, terminal.Target, string, string) (terminal.Address, error) {
	return terminal.Address{}, terminal.ErrUnsupported
}

func (b *Backend) PaneFacts(context.Context, terminal.Conn, string) (terminal.PaneFacts, error) {
	return terminal.PaneFacts{}, terminal.ErrUnsupported
}

func (b *Backend) ReadOutput(context.Context, terminal.Conn, string) (string, error) {
	return "", terminal.ErrUnsupported
}

func (b *Backend) WaitOutput(context.Context, terminal.Conn, string, string, int) error {
	return terminal.ErrUnsupported
}

func (b *Backend) DeliverText(context.Context, terminal.Conn, string, string) error {
	return terminal.ErrUnsupported
}

func (b *Backend) Topology(context.Context, terminal.Conn, terminal.Address) (terminal.Topology, error) {
	return terminal.Topology{}, terminal.ErrUnsupported
}

func (b *Backend) ContainerExists(context.Context, terminal.Conn, terminal.Address) (bool, error) {
	return false, terminal.ErrUnsupported
}

func (b *Backend) ReverseLookup(context.Context, terminal.Conn, terminal.Identity) (terminal.Address, error) {
	return terminal.Address{}, terminal.ErrUnsupported
}

func (b *Backend) Focus(context.Context, terminal.Conn, terminal.Address) terminal.FocusResult {
	return terminal.FocusResult{ID: "focus.unsupported"}
}

func (b *Backend) CloseContainer(context.Context, terminal.Conn, terminal.Address) error {
	return terminal.ErrUnsupported
}
