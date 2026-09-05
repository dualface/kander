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
	sessionField = "会话"
	windowField  = "窗口"

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

// AgentSession 是写进任务卡供 resume 唤醒的会话标识.
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

// LaunchPlan 是领取前完成的 launcher 前置检查结果; 检查失败不领取.
type LaunchPlan struct {
	Launcher       string
	Project        string
	Tmux           string
	Session        string
	SessionExists  bool
	HerdrBin       string
	HerdrWorkspace string
}

// LaunchOutcome 是一次启动的进程或终端地址.
type LaunchOutcome struct {
	Process *os.Process
	Wait    func() (int, error)
	Poll    func() *int
	Window  string
	Tab     string
	Pane    string
}

// LaunchFailure 是启动失败; 附带关闭本次新建容器的结果.
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

// CleanupResult 是接管后旧容器清理结果. 由 dismiss/takeover 包填充 hook.
type CleanupResult struct {
	Cleaned   bool
	OldWindow string
	Channel   string
	Container string
	Detail    string
}

// CleanupTakeover 在 dismiss 卡提供包 API 后由该包赋值; 未提供时 start/resume 输出 N/A.
var CleanupTakeover func(oldWindow string, oldSession AgentSession, newWindow string, timeout float64) CleanupResult

var (
	lookPath            = lookPathExec
	resolveAgent        = process.ResolveAgentProgram
	newInvocation       = process.NewProcessInvocation
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
