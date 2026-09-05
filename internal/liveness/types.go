package liveness

import (
	"os/exec"
	"regexp"
	"runtime"

	"github.com/dualface/kander/internal/config"
)

const (
	Alive   = "alive"
	Stopped = "stopped"
	Drifted = "drifted"
	Unknown = "unknown"
)

var (
	sessionReferenceRe = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	herdrWindowRe      = regexp.MustCompile(`^herdr:([^:\s]+:[^:\s]+):([^:\s]+:[^:\s]+)$`)
	tmuxWindowRe       = regexp.MustCompile(`^(tmux|tmux-session):([^:\s]+):([^:\s]+):([^:\s]+)$`)
	taskGroupRe        = regexp.MustCompile(`^\d{8}-[a-z0-9]+(?:-[a-z0-9]+)*-group$`)

	lookPath  = exec.LookPath
	isWindows = func() bool { return runtime.GOOS == "windows" }
)

// TaskSession 是卡片「会话」字段解析结果.
type TaskSession struct {
	Agent     string
	Reference string
}

// TmuxPaneLocation 是 tmux 反查命中的 pane 坐标.
type TmuxPaneLocation struct {
	SessionID   string
	SessionName string
	WindowID    string
	PaneID      string
}

// Report 是一张卡的只读存活分类, 永不写卡.
type Report struct {
	TaskID    string
	Agent     string
	Status    string
	Channel   string
	Container string
	Detail    string
	NewWindow string
}

func t(id string, args ...any) string {
	return config.Text(id, args...)
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
