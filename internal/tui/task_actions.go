package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/x/ansi"
	"github.com/dualface/kander/internal/board"
)

type taskAction string

const (
	actionStart   taskAction = "start"
	actionFocus   taskAction = "focus"
	actionPick    taskAction = "pick"
	actionBacklog taskAction = "backlog"
	actionArchive taskAction = "archive"
	actionTrash   taskAction = "trash"
)

type taskActionSource struct {
	root     string
	snapshot board.Snapshot
	warnings *board.WarningLog
}

type taskActions struct {
	id               string
	sequence         uint64
	source           taskActionSource
	items            []taskAction
	cursor           int
	loading, running bool
	form             *huh.Form
	action           taskAction
	options          board.MoveOptions
}

type taskActionsLoaded struct {
	id       string
	sequence uint64
	source   taskActionSource
	err      error
}

type taskActionResult struct {
	id              string
	sequence        uint64
	target          string
	payload         BoardPayload
	err, refreshErr error
	warnings        []string
}

func availableTaskActions(state, window string) []taskAction {
	var actions []taskAction
	switch state {
	case "backlog":
		actions = []taskAction{actionStart, actionPick, actionArchive, actionTrash}
	case "todo":
		actions = []taskAction{actionStart, actionBacklog, actionArchive, actionTrash}
	case "done":
		actions = []taskAction{actionArchive}
	}
	if strings.TrimSpace(window) != "" {
		actions = append(actions, actionFocus)
	}
	return actions
}

func loadTaskActionSource(id string) (taskActionSource, error) {
	root, err := board.BoardRoot()
	if err != nil {
		return taskActionSource{}, err
	}
	warnings := &board.WarningLog{}
	snapshot, err := board.ReadSnapshotWithWarnings(root, id, warnings)
	return taskActionSource{root: root, snapshot: snapshot, warnings: warnings}, err
}

func (a *App) openTaskActions() {
	if a.pendingWork != nil || a.focusRunning {
		return
	}
	selected := a.Model.SelectedTask()
	if selected == nil {
		a.showFocusNotice(t("actions.no_selection"))
		return
	}
	a.actionSequence++
	id, sequence, load := selected.TaskID, a.actionSequence, a.LoadTaskActions
	a.TaskActions = &taskActions{id: id, sequence: sequence, loading: true}
	a.resetMouseSelection()
	a.pendingWork = func() any {
		source, err := load(id)
		return taskActionsLoaded{id, sequence, source, err}
	}
}

func (a *App) applyTaskActionsLoaded(result taskActionsLoaded) {
	dialog := a.TaskActions
	if dialog == nil || !dialog.loading || dialog.id != result.id || dialog.sequence != result.sequence {
		return
	}
	if result.err != nil {
		a.TaskActions = nil
		a.showFocusNotice(strings.Join(append([]string{result.err.Error()}, result.source.warningMessages()...), "\n"))
		return
	}
	dialog.source, dialog.loading = result.source, false
	if warnings := result.source.warningMessages(); len(warnings) > 0 {
		a.showFocusNotice(strings.Join(warnings, "\n"))
	}
	dialog.items = availableTaskActions(result.source.snapshot.Entry.State, board.MetadataFrom(result.source.snapshot.Text, board.FieldWindow))
	if len(dialog.items) == 0 {
		a.TaskActions = nil
		a.showFocusNotice(strings.Join(append([]string{t("actions.none")}, result.source.warningMessages()...), "\n"))
	}
}

func (a *App) handleTaskActionKey(key string) tea.Cmd {
	dialog := a.TaskActions
	if dialog.running {
		return nil
	}
	if key == "esc" || (dialog.form == nil && (key == "q" || key == "m")) {
		a.TaskActions = nil
		return nil
	}
	if dialog.loading {
		return nil
	}
	switch key {
	case "up", "k":
		dialog.cursor = (dialog.cursor + len(dialog.items) - 1) % len(dialog.items)
	case "down", "j":
		dialog.cursor = (dialog.cursor + 1) % len(dialog.items)
	case "enter":
		action := dialog.items[dialog.cursor]
		switch action {
		case actionStart, actionFocus:
			selected := a.Model.SelectedTask()
			a.TaskActions = nil
			if selected == nil || selected.TaskID != dialog.id {
				a.showFocusNotice(t("actions.selection_changed"))
			} else if action == actionStart {
				a.confirmSelectedStart()
			} else {
				a.focusSelectedTask()
			}
		case actionArchive, actionTrash:
			return a.openTaskActionForm(action)
		default:
			dialog.action = action
			a.queueTaskAction()
		}
	}
	return nil
}

func runTaskAction(source taskActionSource, action taskAction, options board.MoveOptions) (string, error) {
	target := string(action)
	switch action {
	case actionPick:
		target = "todo"
	case actionArchive:
		target = "archived"
	}
	var err error
	switch action {
	case actionPick, actionBacklog:
		_, err = board.MoveEntry(source.snapshot.Entry, source.root, target)
	case actionArchive, actionTrash:
		_, err = board.MoveWithOptions(source.snapshot.Entry, source.root, target, options)
	default:
		err = fmt.Errorf("%s", t("actions.none"))
	}
	return target, err
}

func (a *App) queueTaskAction() {
	dialog := a.TaskActions
	if dialog.running || a.pendingWork != nil {
		return
	}
	dialog.running = true
	a.invalidateBoardReads()
	id, sequence, source, action, options := dialog.id, dialog.sequence, dialog.source, dialog.action, dialog.options
	a.invalidateSummaries(id)
	index := a.summaries
	getBoard := a.GetBoard
	a.pendingWork = func() any {
		target, err := runTaskAction(source, action, options)
		if index != nil {
			index.Invalidate(id)
		}
		payload, refreshErr := getBoard()
		return taskActionResult{id: id, sequence: sequence, target: target, payload: payload, err: err, refreshErr: refreshErr, warnings: source.warningMessages()}
	}
}

func (a *App) applyTaskActionResult(result taskActionResult) {
	dialog := a.TaskActions
	if dialog == nil || !dialog.running || dialog.id != result.id || dialog.sequence != result.sequence {
		return
	}
	a.TaskActions = nil
	if result.refreshErr != nil {
		a.Model.RefreshError = result.refreshErr.Error()
	} else {
		a.Model.SetBoard(result.payload)
	}
	a.LastRefresh = a.Now()
	message := t("actions.moved", result.id, result.target)
	if result.err != nil {
		message = result.err.Error()
	}
	if result.refreshErr != nil {
		message += "\n" + result.refreshErr.Error()
	}
	warnings := append(result.warnings, result.payload.Warnings...)
	for _, warning := range warnings {
		if !strings.Contains(message, warning) {
			message += "\n" + warning
		}
	}
	a.showFocusNotice(message)
}

func (a *App) renderTaskActions() (popupBox, string) {
	dialog := a.TaskActions
	h, w := a.size()
	p := themePalette(a.Theme)
	frame := popup{Title: printableText(dialog.id), Hint: t("actions.menu_hint"), TightFit: true}
	inner := frame.inner(w, h, 64)
	body := ""
	switch {
	case dialog.loading:
		body = t("actions.loading")
	case dialog.running:
		body = t("actions.running")
	case dialog.form != nil:
		frame.Hint = t("actions.form_hint")
		body = strings.Join(trimTrailingBlank(strings.Split(dialog.form.View(), "\n")), "\n")
	default:
		rows := make([]string, len(dialog.items))
		for i, action := range dialog.items {
			line := "  " + t("actions."+string(action))
			if i == dialog.cursor {
				line = focusMarker + " " + t("actions."+string(action))
				line = p.ink(p.Accent).Render(line)
			}
			rows[i] = line
		}
		// Keep the selected row visible on short terminals.
		height := max(1, h-frame.chrome())
		first := max(0, dialog.cursor-height+1)
		body = strings.Join(rows[first:min(len(rows), first+height)], "\n")
	}
	box, _, out := frame.render(p, w, h, inner, ansi.Wrap(body, inner, ""))
	return box, out
}

// warningMessages is safe for injected sources without a collector.
func (source taskActionSource) warningMessages() []string {
	if source.warnings == nil {
		return nil
	}
	return source.warnings.Messages()
}
