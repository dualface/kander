package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/launch"
)

type startRequest struct {
	launch.StartPreview
	root string
}

type startResult struct {
	result launch.StartResult
	err    error
}

func prepareTaskStart(id string) (startRequest, error) {
	root, err := board.BoardRoot()
	if err != nil {
		return startRequest{}, err
	}
	preview, err := launch.PreviewStart(root, id)
	return startRequest{StartPreview: preview, root: root}, err
}

func runTaskStart(request startRequest) (launch.StartResult, error) {
	if !backgroundStartLauncher(request.Launcher) {
		return launch.StartResult{}, fmt.Errorf("%s", t("tui.start_use_cli", request.Launcher))
	}
	if request.State == "backlog" {
		snapshot, err := board.ReadSnapshot(request.root, request.TaskID)
		if err != nil {
			return launch.StartResult{}, err
		}
		if snapshot.Entry.State != "backlog" {
			return launch.StartResult{}, fmt.Errorf("%s", t("tui.start_state_changed", request.TaskID))
		}
		if _, err := board.MoveEntry(snapshot.Entry, request.root, "todo"); err != nil {
			return launch.StartResult{}, err
		}
	}
	return launch.Start(request.root, request.Agent, request.Launcher, request.TaskID)
}

func backgroundStartLauncher(launcher string) bool {
	return launcher == "herdr" || launcher == "tmux" || launcher == "tmux-session"
}

func (a *App) confirmSelectedStart() {
	selected := a.Model.SelectedTask()
	if selected == nil {
		a.showFocusNotice(t("tui.start_no_selection"))
		return
	}
	if selected.State != "backlog" && selected.State != "todo" {
		a.showFocusNotice(t("tui.start_invalid_state", selected.State))
		return
	}
	request, err := a.PrepareStart(selected.TaskID)
	if err != nil {
		a.showFocusNotice(t("tui.start_failed", err.Error()))
		return
	}
	if request.State != "backlog" && request.State != "todo" {
		a.showFocusNotice(t("tui.start_invalid_state", request.State))
		return
	}
	if !backgroundStartLauncher(request.Launcher) {
		a.showFocusNotice(t("tui.start_use_cli", request.Launcher))
		return
	}
	a.StartConfirmation = &request
	a.resetMouseSelection()
}

func (a *App) handleStartConfirmation(key string) {
	request := *a.StartConfirmation
	a.StartConfirmation = nil
	if key != "y" {
		return
	}
	run := a.StartTask
	a.pendingWork = func() any {
		result, err := run(request)
		return startResult{result, err}
	}
	a.showFocusNotice(t("tui.start_starting", request.TaskID))
}

func (a *App) applyStartResult(result startResult) {
	a.refreshBoard()
	message := ""
	if result.err != nil {
		message = t("tui.start_failed", result.err.Error())
	} else {
		r := result.result
		address := r.Outcome.Tab + ":" + r.Outcome.Pane
		if r.Plan.Launcher != "herdr" {
			address = r.Plan.Session + ":" + r.Outcome.Window + ":" + r.Outcome.Pane
		}
		message = t("tui.start_success", r.TaskID, r.Agent, r.Plan.Launcher, address)
	}
	for _, warning := range result.result.Warnings {
		message += " " + warning
	}
	a.showFocusNotice(message)
}

func (a *App) renderStartConfirmation() (popupBox, string) {
	h, w := a.size()
	p := themePalette(a.Theme)
	request := a.StartConfirmation
	lines := []string{
		t("tui.start_confirm"),
		request.TaskID,
		t("tui.start_settings", request.Agent, request.Launcher),
	}
	if request.State == "backlog" {
		lines = append(lines, t("tui.start_backlog"))
	}
	lines = append(lines, t("tui.start_confirm_keys"))
	for i, line := range lines {
		lines[i] = printableText(ansi.Strip(line))
	}
	inner := max(1, min(w-8, max(40, blockWidth(strings.Join(lines, "\n")))))
	body := ansi.Wrap(strings.Join(lines, "\n"), inner, "")
	box := centerPopupMax(w, h, inner+4, blockHeight(body)+2, max(1, w-4))
	return box, popupFrame(p, box.Width-2).Render(padBlock(body, box.Width-4, box.Height-2, p))
}
