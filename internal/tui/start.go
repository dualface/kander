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

type startNotice struct{ full, compact string }

type startResult struct {
	result   launch.StartResult
	err      error
	sequence uint64
}

func prepareTaskStart(id string) (startRequest, error) {
	root, err := board.BoardRoot()
	if err != nil {
		return startRequest{}, err
	}
	preview, err := launch.PreviewStart(root, id)
	return startRequest{StartPreview: preview, root: root}, err
}

func runTaskStart(request startRequest) (result launch.StartResult, err error) {
	var warnings board.WarningLog
	defer func() { result.Warnings = append(warnings.Messages(), result.Warnings...) }()
	if !backgroundStartLauncher(request.Launcher) {
		return launch.StartResult{}, fmt.Errorf("%s", t("tui.start_use_cli", request.Launcher))
	}
	if request.State == "backlog" {
		snapshot, err := board.ReadSnapshotWithWarnings(request.root, request.TaskID, &warnings)
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
	a.startSequence++
	sequence, id, prepare := a.startSequence, selected.TaskID, a.PrepareStart
	a.StartConfirmation = &startDialog{
		startRequest: startRequest{StartPreview: launch.StartPreview{TaskID: id, State: selected.State}},
		sequence:     sequence, phase: startLoading,
	}
	a.pendingWork = func() any {
		request, err := prepare(id)
		return startPreviewResult{request: request, err: err, taskID: id, sequence: sequence}
	}
	a.resetMouseSelection()
}

func (a *App) applyStartPreview(result startPreviewResult) {
	dialog := a.StartConfirmation
	if dialog == nil || dialog.phase != startLoading || dialog.sequence != result.sequence || dialog.TaskID != result.taskID {
		return
	}
	selected := a.Model.SelectedTask()
	if selected == nil || selected.TaskID != result.taskID {
		a.StartConfirmation = nil
		return
	}
	request, message := result.request, ""
	switch {
	case result.err != nil:
		dialog.phase = startFinished
		dialog.failed = true
		dialog.message = t("tui.start_failed", result.err.Error())
		if len(request.Warnings) > 0 {
			dialog.message = strings.Join(append([]string{dialog.message}, request.Warnings...), " ")
		}
		dialog.bodyView.GotoTop()
		return
	case request.State != "backlog" && request.State != "todo":
		message = t("tui.start_invalid_state", request.State)
	case !backgroundStartLauncher(request.Launcher):
		message = t("tui.start_use_cli", request.Launcher)
	}
	if message != "" {
		a.StartConfirmation = nil
		a.showFocusNotice(strings.Join(append([]string{message}, request.Warnings...), " "))
		return
	}
	dialog.startRequest, dialog.phase = request, startReady
}

func (a *App) handleStartConfirmation(key string) {
	dialog := a.StartConfirmation
	if dialog.phase == startRunning {
		return
	}
	if dialog.phase == startFinished || key != "y" {
		a.StartConfirmation = nil
		return
	}
	if dialog.phase == startLoading {
		a.showFocusNotice(t("tui.start_loading_keys"))
		return
	}
	run, request, sequence := a.StartTask, dialog.startRequest, dialog.sequence
	dialog.phase = startRunning
	a.pendingWork = func() any {
		result, err := run(request)
		return startResult{result: result, err: err, sequence: sequence}
	}
}

func (a *App) applyStartResult(result startResult) {
	a.refreshBoard()
	message := ""
	compact := ""
	if result.err != nil {
		message = t("tui.start_failed", result.err.Error())
	} else {
		r := result.result
		address := r.Outcome.Tab + ":" + r.Outcome.Pane
		if r.Plan.Launcher != "herdr" {
			address = r.Plan.Session + ":" + r.Outcome.Window + ":" + r.Outcome.Pane
		}
		message = t("tui.start_success", r.TaskID, r.Agent, r.Plan.Launcher, address)
		compact = t("tui.start_success_compact", r.Agent, r.Plan.Launcher, address)
	}
	for _, warning := range result.result.Warnings {
		message += " " + warning
		compact += " " + warning
	}
	if dialog := a.StartConfirmation; dialog != nil && dialog.phase == startRunning && dialog.sequence == result.sequence {
		dialog.phase, dialog.message, dialog.failed = startFinished, message, result.err != nil
		dialog.bodyView.GotoTop()
	}
	a.showFocusNotice(message)
	if result.err == nil {
		a.startNotice = &startNotice{a.CopyNotice, strings.ReplaceAll(printableText(ansi.Strip(compact)), "\n", " ")}
	}
}

func (a *App) renderStartPopup(lines []string) (popupBox, string) {
	h, w := a.size()
	p := themePalette(a.Theme)
	for i, line := range lines {
		lines[i] = printableText(ansi.Strip(line))
	}
	frame := popup{MaxWidth: max(1, w-4)}
	inner := frame.inner(w, h, max(1, min(w-8, max(40, blockWidth(strings.Join(lines, "\n"))))))
	box, _, out := frame.render(p, w, h, inner, ansi.Wrap(strings.Join(lines, "\n"), inner, ""))
	return box, out
}

func (a *App) requestQuit() {
	a.Running = false
}

func (a *App) activeStartNotice() bool {
	return a.startNotice != nil && a.CopyNotice == a.startNotice.full && a.Now().Before(a.CopyNoticeUntil)
}

func (a *App) displayNotice() string {
	if a.activeStartNotice() {
		_, w := a.size()
		if displayWidth(a.CopyNotice) > w-1 {
			return a.startNotice.compact
		}
	}
	return a.CopyNotice
}

func (a *App) startNoticeOverflows(width int) bool {
	return a.activeStartNotice() && displayWidth(a.startNotice.compact) > width-1
}
