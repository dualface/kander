// Package terminal is the only entry point callers use to reach a terminal
// backend (herdr, tmux, or a direct process launcher). Callers pick a Backend
// by launcher name from the registry and degrade by Capabilities, never by
// comparing launcher names or building a terminal command line themselves.
package terminal

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dualface/kander/internal/probe"
)

// Runner executes one argv command. Callers choose the runner per call site so
// the process ownership and deadline semantics of that call stay unchanged:
// ProbeRunner bounds the command with the probe budget and owns its process
// tree, SpawnRunner only enforces a deadline present on the context.
type Runner func(ctx context.Context, program string, args []string) (probe.Result, error)

// Conn binds a resolved backend executable to the runner used for one call.
type Conn struct {
	Program string
	Run     Runner
}

// Capabilities describe what a backend can do; callers degrade on these flags.
type Capabilities struct {
	// Container: the backend creates a tab or window whose pane address is
	// recorded in WINDOW. Without it the agent runs as a direct child process
	// and WINDOW holds the bare launcher name.
	Container bool
	// Focus: the local terminal can be switched to a recorded pane.
	Focus bool
	// PaneMetadata: pane user options carry the session marker, written after
	// the agent starts (so a session discovered after start can be recorded).
	PaneMetadata bool
	// ForegroundProcess: pane facts report the foreground command, the dead
	// state and copy mode.
	ForegroundProcess bool
	// AgentIdentity: pane facts report the agent name, the agent status, the
	// reported session identity and the owning container.
	AgentIdentity bool
	// SessionReport: the session identity is reported out of band after start.
	SessionReport bool
	// WaitOutput: the backend blocks on output itself; otherwise callers poll
	// ReadOutput.
	WaitOutput bool
	// POSIXOnly: unavailable on native Windows.
	POSIXOnly bool
	// Detached: a direct launcher that returns immediately with a process ID
	// instead of occupying the current terminal.
	Detached bool
}

// Address is a parsed WINDOW address. WINDOW is "<launcher>:<opaque>", where
// only the backend encodes and decodes the opaque part.
type Address struct {
	// Session is the tmux session id or name; empty for backends without one.
	Session string
	// Container is the herdr tab id or the tmux window id.
	Container string
	Pane      string
}

// Target is the launch preflight result of one backend.
type Target struct {
	Program       string
	Session       string
	SessionExists bool
	Workspace     string
	Project       string
}

// PrepareRequest carries the caller-owned environment of a launch preflight.
type PrepareRequest struct {
	Project  string
	Command  string
	Windows  bool
	LookPath func(string) (string, error)
	Getenv   func(string) string
	TTY      func() bool
}

// PaneFacts are the facts of one pane. Gone means the pane (or its server) no
// longer exists; the remaining fields follow the backend capabilities.
type PaneFacts struct {
	Gone       bool
	GoneDetail string

	// ForegroundProcess backends.
	Command string
	InMode  string
	Dead    string
	// PaneMetadata backends.
	SessionMarker string
	// AgentIdentity backends.
	Agent        string
	AgentStatus  string
	AgentSession string
	Container    string
}

// Topology is the container that owns a pane.
type Topology struct {
	// Session is the session coordinate this launcher records in WINDOW.
	Session   string
	Container string
	// PaneCount is reported by backends that count panes (tmux window_panes).
	PaneCount string
	// Panes lists pane ids for backends that enumerate them (herdr).
	Panes []string
}

// Identity is the agent session a reverse lookup searches for.
type Identity struct {
	Agent     string
	Reference string
	// ProcessName resolves the expected foreground command lazily, so a
	// collection failure is reported before an agent definition error.
	ProcessName func() (string, error)
}

// SessionReport is one out-of-band session identity report.
type SessionReport struct {
	// Conn runs the read-back probe of the reported identity.
	Conn      Conn
	Pane      string
	Agent     string
	Reference string
	Deadline  time.Time
	Now       func() time.Time
}

// FocusResult is a localizable focus notice: a message ID and its arguments.
type FocusResult struct {
	Success bool
	ID      string
	Args    []any
}

// Backend is the operation set every terminal backend implements. Operations
// a backend cannot perform return ErrUnsupported.
type Backend interface {
	Name() string
	Capabilities() Capabilities
	// Executable is the command resolved on PATH; empty without a container.
	Executable() string
	// VersionArgs is the argv that prints the executable's version.
	VersionArgs() []string

	// OpaqueAddress renders the part of WINDOW after "<launcher>:".
	OpaqueAddress(Address) string
	// ParseAddress strictly parses a WINDOW value owned by this backend.
	ParseAddress(value string) (Address, bool)
	// ParseFocusAddress parses the colon fields of a read-only focus target,
	// which also accepts legacy herdr ids without a workspace prefix.
	ParseFocusAddress(fields []string) (Address, bool)

	// AutoDetect reports whether auto resolution selects this backend.
	AutoDetect(getenv func(string) string) bool
	Prepare(PrepareRequest) (Target, error)
	// StartedLines renders the launch report after the given head line.
	StartedLines(head string, target Target, address Address, getenv func(string) string) []string

	CreateContainer(conn Conn, target Target, cwd, label string) (Address, error)
	WaitReady(conn Conn, pane string) error
	RunCommand(conn Conn, pane, command string, posix bool) error
	SetSessionMarker(conn Conn, pane, value string) error
	ReportSession(SessionReport) error

	PaneFacts(ctx context.Context, conn Conn, pane string) (PaneFacts, error)
	ReadOutput(ctx context.Context, conn Conn, pane string) (string, error)
	WaitOutput(ctx context.Context, conn Conn, pane, match string, timeoutMS int) error
	DeliverText(ctx context.Context, conn Conn, pane, text string) error
	Topology(ctx context.Context, conn Conn, address Address) (Topology, error)
	ContainerExists(ctx context.Context, conn Conn, address Address) (bool, error)
	ReverseLookup(ctx context.Context, conn Conn, identity Identity) (Address, error)
	Focus(ctx context.Context, conn Conn, address Address) FocusResult
	CloseContainer(ctx context.Context, conn Conn, address Address) error
}

// ErrUnsupported is returned by operations a backend does not provide.
var ErrUnsupported = errors.New("terminal: operation not supported by this backend")

// ErrNoReportChannel means the out-of-band session report has no channel.
var ErrNoReportChannel = errors.New("terminal: session report channel is not configured")

// MatchError distinguishes a completed reverse lookup with zero or several
// matches from a collection failure.
type MatchError struct {
	Matches int
	Cause   error
}

func (e *MatchError) Error() string { return e.Cause.Error() }
func (e *MatchError) Unwrap() error { return e.Cause }

// FormatAddress renders the complete WINDOW value of an address.
// A backend without a container records its bare launcher name.
func FormatAddress(backend Backend, address Address) string {
	if !backend.Capabilities().Container {
		return backend.Name()
	}
	return backend.Name() + ":" + backend.OpaqueAddress(address)
}

// ErrorKind classifies a failed backend command.
type ErrorKind int

const (
	// KindExec: the command could not run; Cause holds the error.
	KindExec ErrorKind = iota + 1
	// KindExit: the command exited non-zero; Code and Stderr hold it.
	KindExit
	// KindNotJSON: the response is not JSON; Cause holds the decode error.
	KindNotJSON
	// KindNotObject: the response is JSON but not an object.
	KindNotObject
	// KindMissingResult: the response object has no result.
	KindMissingResult
	// KindInvalidResponse: the result does not describe the requested object.
	KindInvalidResponse
)

// CommandError is a failed backend command. Message is the diagnostic of the
// operation's primary caller; the raw fields let other callers render their
// own diagnostic from the same failure.
type CommandError struct {
	Kind    ErrorKind
	Message string
	Cause   error
	Code    int
	Stderr  string
}

func (e *CommandError) Error() string { return e.Message }

func (e *CommandError) Unwrap() error { return e.Cause }

// Detail is the raw diagnostic: the run error, the trimmed stderr, or the exit
// status.
func (e *CommandError) Detail() string {
	if e.Kind == KindExec && e.Cause != nil {
		return e.Cause.Error()
	}
	if detail := strings.TrimSpace(e.Stderr); detail != "" {
		return detail
	}
	return "exit " + strconv.Itoa(e.Code)
}

// AsCommandError extracts a CommandError from err.
func AsCommandError(err error) (*CommandError, bool) {
	var commandErr *CommandError
	ok := errors.As(err, &commandErr)
	return commandErr, ok
}

// RunStep runs one focus or control step and renders its failure as
// "<subcommand>: <detail>", preferring stderr, then stdout, then the exit code.
func RunStep(ctx context.Context, conn Conn, args []string) error {
	result, err := conn.Run(ctx, conn.Program, args)
	if err != nil {
		return fmt.Errorf("%s: %s", args[0], probe.FailureDetail(err))
	}
	if result.Code != 0 {
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			detail = strings.TrimSpace(result.Stdout)
		}
		if detail == "" {
			detail = fmt.Sprintf("exit %d", result.Code)
		}
		return fmt.Errorf("%s: %s", args[0], detail)
	}
	return nil
}
