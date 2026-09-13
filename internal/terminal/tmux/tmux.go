// Package tmux implements the tmux terminal backend for the tmux and
// tmux-session launchers. Both share one implementation; tmux-session differs
// only by addressing a dedicated per-project session by name.
package tmux

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

const (
	// Name is the launcher that opens a window in the current tmux session.
	Name = "tmux"
	// SessionName is the launcher that opens a window in a per-project session.
	SessionName = "tmux-session"
	// Executable is the tmux command resolved on PATH.
	Executable = "tmux"
	// SessionHint is the command that enters a tmux session.
	SessionHint = "tmux new -A -s kander"

	paneSessionOption = "@kander_session"
	legacyPaneSession = "@onevoke_session"
	projectSessionOpt = "@kander_project"
	legacyProjectOpt  = "@onevoke_project"

	projectSessionTries = 9
)

var windowRe = regexp.MustCompile(`^(tmux|tmux-session):([^:\s]+):([^:\s]+):([^:\s]+)$`)

// Backend is one tmux launcher.
type Backend struct {
	name           string
	projectSession bool
	getenv         func(string) string
}

// New returns the backend of the tmux or tmux-session launcher.
func New(name string, getenv func(string) string) *Backend {
	return &Backend{name: name, projectSession: name == SessionName, getenv: getenv}
}

func (b *Backend) Name() string          { return b.name }
func (b *Backend) Executable() string    { return Executable }
func (b *Backend) VersionArgs() []string { return []string{"-V"} }

func (b *Backend) Capabilities() terminal.Capabilities {
	return terminal.Capabilities{
		Container:         true,
		Focus:             true,
		PaneMetadata:      true,
		ForegroundProcess: true,
		POSIXOnly:         true,
	}
}

func (b *Backend) OpaqueAddress(address terminal.Address) string {
	return address.Session + ":" + address.Container + ":" + address.Pane
}

func (b *Backend) ParseAddress(value string) (terminal.Address, bool) {
	match := windowRe.FindStringSubmatch(value)
	if match == nil || match[1] != b.name {
		return terminal.Address{}, false
	}
	return terminal.Address{Launcher: b.name, Session: match[2], Container: match[3], Pane: match[4]}, true
}

func (b *Backend) ParseFocusAddress(fields []string) (terminal.Address, bool) {
	if len(fields) != 4 || fields[0] != b.name {
		return terminal.Address{}, false
	}
	return terminal.Address{Launcher: b.name, Session: fields[1], Container: fields[2], Pane: fields[3]}, true
}

// AutoDetect selects plain tmux when kander runs inside a tmux client.
func (b *Backend) AutoDetect(getenv func(string) string) bool {
	return !b.projectSession && getenv("TMUX") != ""
}

func (b *Backend) StartedLines(head string, target terminal.Target, address terminal.Address, getenv func(string) string) []string {
	if !b.projectSession {
		return []string{config.Text("launch.launcher_tmux_window", head, address.Container)}
	}
	hint := "tmux attach -t " + target.Session
	if getenv("TMUX") != "" {
		hint = "tmux switch-client -t " + target.Session
	}
	return []string{
		config.Text("launch.session_window", head, target.Session, address.Container),
		config.Text("launch.view", hint),
	}
}

func textError(id string, args ...any) *probe.Error {
	return &probe.Error{Message: config.Text(id, args...)}
}

func orExit(detail string, code int) string {
	if detail != "" {
		return detail
	}
	return "exit " + strconv.Itoa(code)
}

func orNA(value string) string {
	if value == "" {
		return "N/A"
	}
	return value
}

func trimNL(s string) string {
	return strings.TrimRight(s, "\r\n")
}

// commandError keeps the caller-facing message and the raw result together.
func commandError(message string, res probe.Result, cause error) *terminal.CommandError {
	kind := terminal.KindExit
	if cause != nil {
		kind = terminal.KindExec
	}
	return &terminal.CommandError{Kind: kind, Message: message, Cause: cause, Code: res.Code, Stderr: res.Stderr}
}

func background() context.Context { return context.Background() }

var _ terminal.Backend = (*Backend)(nil)
