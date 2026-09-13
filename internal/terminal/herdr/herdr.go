// Package herdr implements the herdr terminal backend: tabs in the current
// herdr workspace, agent-aware pane facts, and the herdr socket for session
// reports and pane focus.
package herdr

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

const (
	// Name is the herdr launcher.
	Name = "herdr"
	// Executable is the herdr command resolved on PATH.
	Executable = "herdr"

	readyTimeoutMS = 15000
)

var windowRe = regexp.MustCompile(`^herdr:([^:\s]+:[^:\s]+):([^:\s]+:[^:\s]+)$`)

// Backend is the herdr launcher.
type Backend struct {
	getenv    func(string) string
	paneFocus func(ctx context.Context, socket, pane string) error
}

// New returns the herdr backend. paneFocus switches a pane through the herdr
// socket; nil uses the socket protocol.
func New(getenv func(string) string, paneFocus func(ctx context.Context, socket, pane string) error) *Backend {
	if paneFocus == nil {
		paneFocus = focusPane
	}
	return &Backend{getenv: getenv, paneFocus: paneFocus}
}

var _ terminal.Backend = (*Backend)(nil)

func (b *Backend) Name() string          { return Name }
func (b *Backend) Executable() string    { return Executable }
func (b *Backend) VersionArgs() []string { return []string{"--version"} }

// DefaultBinaries lists where the official herdr installer puts the binary,
// most preferred first.
func DefaultBinaries(windows bool) []string {
	if windows {
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			return nil
		}
		return []string{filepath.Join(local, "Programs", "Herdr", "bin", "herdr.exe")}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".local", "bin", "herdr")}
}

func (b *Backend) Capabilities() terminal.Capabilities {
	return terminal.Capabilities{
		Container:     true,
		Focus:         true,
		AgentIdentity: true,
		SessionReport: true,
		WaitOutput:    true,
	}
}

func (b *Backend) OpaqueAddress(address terminal.Address) string {
	return address.Container + ":" + address.Pane
}

func (b *Backend) ParseAddress(value string) (terminal.Address, bool) {
	match := windowRe.FindStringSubmatch(value)
	if match == nil {
		return terminal.Address{}, false
	}
	return terminal.Address{Container: match[1], Pane: match[2]}, true
}

func (b *Backend) ParseFocusAddress(fields []string) (terminal.Address, bool) {
	if len(fields) == 0 || fields[0] != Name {
		return terminal.Address{}, false
	}
	switch len(fields) {
	case 3:
		return terminal.Address{Container: fields[1], Pane: fields[2]}, true
	case 5:
		// Public herdr IDs include their workspace prefix, for example w1:t2 and w1:p3.
		return terminal.Address{Container: strings.Join(fields[1:3], ":"), Pane: strings.Join(fields[3:5], ":")}, true
	}
	return terminal.Address{}, false
}

// AutoDetect selects herdr when kander runs inside herdr.
func (b *Backend) AutoDetect(getenv func(string) string) bool {
	return getenv("HERDR_ENV") == "1"
}

func (b *Backend) Prepare(request terminal.PrepareRequest) (terminal.Target, error) {
	if request.Getenv("HERDR_ENV") != "1" {
		return terminal.Target{}, textError("launch.not_currently_in_herdr_the_herdr_launcher_requires_herdr")
	}
	program, err := request.LookPath(Executable)
	if err != nil {
		return terminal.Target{}, textError("launch.herdr_is_not_in_path_run_kander_welcome_to")
	}
	workspace := strings.TrimSpace(request.Getenv("HERDR_WORKSPACE_ID"))
	if workspace == "" {
		return terminal.Target{}, textError("launch.herdr_workspace_id_is_missing_cannot_create_a_tab")
	}
	return terminal.Target{Program: program, Workspace: workspace, Project: request.Project}, nil
}

func (b *Backend) StartedLines(head string, _ terminal.Target, address terminal.Address, _ func(string) string) []string {
	return []string{config.Text("launch.launcher_herdr_tab_pane", head, address.Container, address.Pane)}
}

// SetSessionMarker is not available: herdr panes carry no user options.
func (b *Backend) SetSessionMarker(terminal.Conn, string, string) error {
	return terminal.ErrUnsupported
}

// ContainerExists is not needed: a herdr tab is probed through Topology.
func (b *Backend) ContainerExists(context.Context, terminal.Conn, terminal.Address) (bool, error) {
	return false, terminal.ErrUnsupported
}

func textError(id string, args ...any) *probe.Error {
	return &probe.Error{Message: config.Text(id, args...)}
}

func orNA(value string) string {
	if value == "" {
		return "N/A"
	}
	return value
}

// publicID validates a public herdr id: non-empty and free of NUL.
func publicID(value any) string {
	s, ok := value.(string)
	if !ok || s == "" || strings.ContainsRune(s, 0) {
		return ""
	}
	return s
}

func failureDetail(res probe.Result) string {
	if s := strings.TrimSpace(res.Stderr); s != "" {
		return s
	}
	return "exit " + strconv.Itoa(res.Code)
}

func errorCode(res probe.Result) string {
	for _, output := range []string{res.Stderr, res.Stdout} {
		var payload any
		if err := json.Unmarshal([]byte(output), &payload); err != nil {
			continue
		}
		obj, _ := payload.(map[string]any)
		if obj == nil {
			continue
		}
		errObj, _ := obj["error"].(map[string]any)
		if errObj == nil {
			continue
		}
		code, _ := errObj["code"].(string)
		if code != "" {
			return code
		}
	}
	return ""
}

func sessionReference(pane map[string]any) string {
	identity, _ := pane["agent_session"].(map[string]any)
	if identity == nil {
		return ""
	}
	ref, _ := identity["value"].(string)
	return ref
}

// commandError keeps the caller-facing message and the raw result together.
func commandError(kind terminal.ErrorKind, message string, res probe.Result, cause error) *terminal.CommandError {
	return &terminal.CommandError{Kind: kind, Message: message, Cause: cause, Code: res.Code, Stderr: res.Stderr}
}
