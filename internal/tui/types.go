package tui

import (
	"encoding/json"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

const (
	cardHeight = 4
	// 栏目最小宽度. 低于这个宽度的卡片已经读不出内容, 所以宁可少显示一栏.
	minColumnWidth     = config.DefaultTUIMinColumnWidth
	minMinColumnWidth  = config.MinTUIMinColumnWidth
	maxMinColumnWidth  = config.MaxTUIMinColumnWidth
	minColumnWidthStep = 4
	// 用户设置的是同屏栏目数, 宽度由终端宽度平分得出.
	defaultColumns = config.DefaultTUIColumns
	minColumns     = config.MinTUIColumns
	minBoardHeight = 8
	headerRow      = 0
	spacerRow      = 1
	panelTopRow    = 2
	bodyTop        = 3
	// 详情面板: 上边框, 元信息行, 分隔线, 然后才是正文.
	detailPanelTopRow  = 2
	detailMetaRow      = 3
	detailRuleRow      = 4
	detailBodyTop      = 5
	mouseScrollStep    = 3
	copyNoticeSeconds  = 2.0
	defaultRefreshSecs = config.DefaultTUIRefresh
	minRefreshSecs     = config.MinTUIRefresh
	maxRefreshSecs     = config.MaxTUIRefresh
	refreshStep        = 5
)

var (
	activeStates = []string{"backlog", "todo", "working", "review", "done"}
	allStates    = append([]string(nil), board.States...)
	themes       = config.TUIThemes
)

// Task 与 BoardPayload 直接复用 board 包的领域视图, 避免解析规则分叉.
type Task = board.TaskSummary
type BoardPayload = board.BoardView

func knownState(state string) bool {
	for _, item := range allStates {
		if item == state {
			return true
		}
	}
	return false
}

func stateIndex(state string) int {
	for i, item := range allStates {
		if item == state {
			return i
		}
	}
	return len(allStates)
}

func boardContentKey(tasks []Task) string {
	type row map[string]string
	rows := make([]row, 0, len(tasks))
	for _, task := range tasks {
		item := row{
			"assignee":     task.Assignee,
			"completed_at": task.CompletedAt,
			"created_at":   task.CreatedAt,
			"document":     task.Document,
			"kind":         task.Kind,
			"result":       task.Result,
			"started_at":   task.StartedAt,
			"state":        task.State,
			"task_group":   task.TaskGroup,
			"task_id":      task.TaskID,
			"time":         task.Time,
			"title":        task.Title,
			"type":         task.Type,
		}
		if task.Document == "" {
			delete(item, "document")
		}
		rows = append(rows, item)
	}
	data, err := json.Marshal(rows)
	if err != nil {
		return ""
	}
	return string(data)
}

func containsString(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// maxColumns 是栏目数上限: 活跃栏目全部显示出来就到头了.
func maxColumns() int {
	return config.MaxTUIColumns
}

func clampColumns(count int) int {
	if count < minColumns {
		return minColumns
	}
	if limit := maxColumns(); count > limit {
		return limit
	}
	return count
}

func clampMinColumnWidth(width int) int {
	if width < minMinColumnWidth {
		return minMinColumnWidth
	}
	if width > maxMinColumnWidth {
		return maxMinColumnWidth
	}
	return width
}

func clampRefresh(seconds int) int {
	if seconds < minRefreshSecs {
		return minRefreshSecs
	}
	if seconds > maxRefreshSecs {
		return maxRefreshSecs
	}
	return seconds
}

func themeIndex(name string) int {
	for i, item := range themes {
		if item == name {
			return i
		}
	}
	return 0
}

func joinNonEmpty(parts ...string) string {
	var out []string
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, " | ")
}
