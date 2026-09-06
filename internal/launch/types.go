package launch

import (
	"os"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/window"
)

const (
	sessionField = board.FieldSession
	windowField  = board.FieldWindow

	paneSessionOption   = "@kander_session"
	projectSessionOpt   = "@kander_project"
	legacyProjectOpt    = "@onevoke_project"
	tmuxSessionHint     = "tmux new -A -s kander"
	projectSessionTries = 9

	notifyDefaultTimeout = 120.0
	notifyPollInterval   = 100 * time.Millisecond
	sessionDiscoverWait  = 10 * time.Second
	herdrReadyTimeoutMS  = 15000
	herdrReportBudget    = time.Second
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
	Launcher       string
	Project        string
	Tmux           string
	Session        string
	SessionExists  bool
	HerdrBin       string
	HerdrWorkspace string
}

// LaunchOutcome is the process or terminal address of one launch.
type LaunchOutcome struct {
	Process *os.Process
	Wait    func() (int, error)
	Poll    func() *int
	Window  string
	Tab     string
	Pane    string
}

// LaunchFailure is a failed launch, together with the result of closing the container created by this attempt.
type LaunchFailure struct {
	Err        error
	CloseError string
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
	loadEffective       = func() (*config.Config, error) { return config.Effective(nil) }
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
