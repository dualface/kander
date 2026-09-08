package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/focus"
)

type focusFn func(context.Context, string) focus.Result

type focusResult struct{ message string }

func (a *App) focusSelectedTask() {
	if a.focusRunning || a.pendingWork != nil {
		return
	}
	selected := a.Model.SelectedTask()
	if selected == nil {
		a.showFocusNotice(t("focus.no_selection"))
		return
	}
	taskID, getTask, run := selected.TaskID, a.GetTask, a.FocusWindow
	a.focusRunning = true
	a.pendingWork = func() any {
		task, err := getTask(taskID)
		if err != nil {
			return focusResult{t("focus.load_failed", err.Error())}
		}
		result := run(context.Background(), board.MetadataFrom(task.Document, board.FieldWindow))
		return focusResult{result.Message}
	}
}

func (a *App) showFocusNotice(message string) {
	a.startNotice = nil
	a.CopyNotice = strings.ReplaceAll(printableText(ansi.Strip(message)), "\n", " ")
	a.CopyNoticeUntil = a.Now().Add(5 * time.Second)
}
