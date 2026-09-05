// Package board 实现看板定位、卡片校验与状态迁移, 对标 onevoke 不启动 Agent 的生命周期命令.
package board

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"time"

	"github.com/dualface/kander/internal/config"
)

const (
	EnvBoardDir = "KANBAN_DIR"
)

var (
	States = []string{"backlog", "todo", "working", "review", "done", "archived", "trash"}

	deferredCheckStates = map[string]struct{}{
		"done":     {},
		"archived": {},
	}

	transitions = map[string]map[string]struct{}{
		"backlog":  {"todo": {}, "archived": {}, "trash": {}},
		"todo":     {"backlog": {}, "working": {}, "archived": {}, "trash": {}},
		"working":  {"review": {}, "done": {}, "archived": {}, "trash": {}},
		"review":   {"working": {}, "done": {}, "archived": {}, "trash": {}},
		"done":     {"archived": {}, "trash": {}},
		"archived": {"trash": {}},
		"trash":    {},
	}

	legacyStates = []string{"backlog", "todo", "working", "done", "archived", "trash"}

	taskIDRe    = regexp.MustCompile(`^\d{8}-[a-z0-9]+(?:-[a-z0-9]+)*-task$`)
	taskGroupRe = regexp.MustCompile(`^\d{8}-[a-z0-9]+(?:-[a-z0-9]+)*-group$`)
	slugRe      = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

	typeNames = map[string]string{
		"feature":  "Feature",
		"bug":      "Bug",
		"chore":    "Chore",
		"research": "Research",
	}

	readySections  = []string{"任务目标", "预期成果", "验收条件", "不在本轮范围"}
	archiveResults = map[string]struct{}{
		"completed": {}, "cancelled": {}, "duplicate": {}, "wontfix": {},
	}

	stateSet = func() map[string]struct{} {
		out := make(map[string]struct{}, len(States))
		for _, s := range States {
			out[s] = struct{}{}
		}
		return out
	}()
)

// Error 是看板命令的可展示失败.
type Error struct {
	Message string
}

func (e *Error) Error() string { return e.Message }

func kanbanError(id string, args ...any) *Error {
	return &Error{Message: t(id, args...)}
}

func t(id string, args ...any) string {
	return config.Text(id, args...)
}

func untitled() string {
	return t("board.untitled")
}

// Entry 是一张任务卡入口.
type Entry struct {
	TaskID   string
	State    string
	Path     string
	Document string
	Kind     string
}

// Board 是一次扫描结果. 无效入口进入 Problems; 与任务 ID 绑定的违规进入 Blocked.
type Board struct {
	Entries  map[string]Entry
	Blocked  map[string]string
	Problems []Problem
}

// Problem 是 check 汇报的结构问题.
type Problem struct {
	Path         string
	Message      string
	RelatedPaths []string
}

// TaskDependencies 是一张卡的前置依赖及任务组展开结果.
type TaskDependencies struct {
	PrerequisiteIDs []string
	InternalTasks   []string
	ExternalTasks   []string
	TaskGroups      []string
	ExpandedTaskIDs []string
}

func nowStamp() string {
	return time.Now().Format("2006-01-02 15:04")
}

func todayPrefix() string {
	return time.Now().Format("20060102")
}

func isWindows() bool {
	return runtime.GOOS == "windows"
}

func volumeAnchor(abs string) (string, error) {
	if isWindows() {
		vol := filepath.VolumeName(abs)
		if vol == "" {
			return "", kanbanError("board.invalid_board_path", abs)
		}
		return vol + `\`, nil
	}
	return string(os.PathSeparator), nil
}

func isNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}

func isExist(err error) bool {
	return errors.Is(err, os.ErrExist)
}

func wrapFS(err error, id string, args ...any) error {
	if err == nil {
		return nil
	}
	var ke *Error
	if errors.As(err, &ke) {
		return err
	}
	return &Error{Message: t(id, args...)}
}

func allowedMove(from, to string) bool {
	next, ok := transitions[from]
	if !ok {
		return false
	}
	_, ok = next[to]
	return ok
}

func stateIndex(state string) int {
	for i, s := range States {
		if s == state {
			return i
		}
	}
	return len(States)
}

func joinBoard(root string, elems ...string) string {
	parts := append([]string{root}, elems...)
	return filepath.Join(parts...)
}

func uniqueKeepOrder(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
