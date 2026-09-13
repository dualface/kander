package launch

import (
	"os"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/terminal"
	_ "github.com/dualface/kander/internal/terminal/builtin"
	"github.com/dualface/kander/internal/terminal/direct"
	"github.com/dualface/kander/internal/window"
)

const (
	sessionField = board.FieldSession
	windowField  = board.FieldWindow

	notifyDefaultTimeout = 120.0
	notifyPollInterval   = 100 * time.Millisecond
	sessionDiscoverWait  = 10 * time.Second
	sessionReportBudget  = time.Second
	resumeOutputLimit    = 8192
)

// AgentSession is the session identity written into the task card so resume can wake it.
type AgentSession struct {
	Agent     string
	Reference string
}

func (s AgentSession) Render() string {
	if s.Reference == "" {
		return s.Agent
	}
	return s.Agent + " " + s.Reference
}

// LaunchPlan is the result of the launcher preflight checks run before claiming; a failed check does not claim the card.
type LaunchPlan struct {
	warning        func(string)
	Launcher       string
	Target         terminal.Target
	PromptDelivery config.PromptDelivery
	Prompt         string
}

// backend returns the terminal backend of the resolved launcher. An unknown
// name launches in the foreground, as the launcher switch always did.
func (p LaunchPlan) backend() terminal.Backend {
	if backend, ok := terminal.Lookup(p.Launcher); ok {
		return backend
	}
	backend, _ := terminal.Lookup(direct.Foreground)
	return backend
}

func (p LaunchPlan) capabilities() terminal.Capabilities {
	return p.backend().Capabilities()
}

// OccupiesTerminal reports that the agent runs in the current terminal, so the
// caller must wait for it to exit.
func (p LaunchPlan) OccupiesTerminal() bool {
	caps := p.capabilities()
	return !caps.Container && !caps.Detached
}

// address is the terminal address of a launch outcome.
func (p LaunchPlan) address(outcome LaunchOutcome) terminal.Address {
	return terminal.Address{Launcher: p.Launcher, Session: p.Target.Session, Container: outcome.Container, Pane: outcome.Pane}
}

// OpaqueAddress renders the backend part of the container address of a
// launch, without the launcher prefix; it is empty without a container.
func OpaqueAddress(plan LaunchPlan, outcome LaunchOutcome) string {
	if !plan.capabilities().Container {
		return ""
	}
	return plan.backend().OpaqueAddress(plan.address(outcome))
}

// spawnConn runs launch-time terminal commands as plain child processes.
func spawnConn(plan LaunchPlan) terminal.Conn {
	return terminal.Conn{Program: plan.Target.Program, Run: terminal.SpawnRunner}
}

// boundedSpawnConn bounds each launch-time command by the probe budget.
func boundedSpawnConn(plan LaunchPlan) terminal.Conn {
	return terminal.Conn{Program: plan.Target.Program, Run: terminal.SpawnRunnerWithin(probe.DefaultCommandTimeout)}
}

// probeConn runs pane probes with process-tree ownership and probe budgets.
func probeConn(plan LaunchPlan) terminal.Conn {
	return terminal.Conn{Program: plan.Target.Program, Run: terminal.ProbeRunner}
}

// LaunchOutcome is the process or terminal address of one launch.
type LaunchOutcome struct {
	Process   *os.Process
	Wait      func() (int, error)
	Poll      func() *int
	Container string
	Pane      string
}

// LaunchFailure is a failed launch, together with the result of closing the container created by this attempt.
type LaunchFailure struct {
	DeliveryUnknown bool
	Err             error
	CloseError      string
}

func (f *LaunchFailure) Error() string {
	if f == nil || f.Err == nil {
		return ""
	}
	return f.Err.Error()
}

func (f *LaunchFailure) Unwrap() error {
	if f == nil {
		return nil
	}
	return f.Err
}

// CleanupResult is the outcome of cleaning up the old container after a takeover. The dismiss/takeover package fills in the hook.
type CleanupResult struct {
	Cleaned   bool
	OldWindow string
	Channel   string
	Container string
	Detail    string
}

// CleanupTakeover is assigned by the dismiss package once it exposes its API; while unset, start/resume report N/A.
var CleanupTakeover func(oldWindow string, oldSession AgentSession, newWindow string, timeout float64) CleanupResult

var (
	lookPath            = lookPathExec
	resolveAgent        = process.ResolveAgentProgram
	newInvocation       = process.NewProcessInvocation
	newShellInvocation  = process.NewShellInvocation
	createTaskFile      = process.CreateTaskFile
	removeTaskFile      = os.Remove
	taskInstruction     = process.TaskFileInstruction
	loadEffective       = func() (*config.Config, error) { return config.Load(false) }
	currentInstallPaths = config.CurrentInstallPaths
	nowStamp            = func() string { return time.Now().Format("2006-01-02 15:04") }
	newUUID             = randomUUID
	runtimeWindows      = func() bool { return isWindowsGOOS() }
	stdinIsTTY          = func() bool { return fileIsTTY(os.Stdin) }
	stdoutIsTTY         = func() bool { return fileIsTTY(os.Stdout) }
	stderrIsTTY         = func() bool { return fileIsTTY(os.Stderr) }
	sleepFn             = time.Sleep
	nowFn               = time.Now
	writeDocumentFn     = window.WriteDocument
	startProcessFn      = startProcess
	boardRootFn         = board.BoardRoot
	loadBoardFn         = board.LoadBoard
	locateFn            = board.Locate
	readDocumentFn      = board.ReadDocument
	moveEntryFn         = board.MoveEntry
)
