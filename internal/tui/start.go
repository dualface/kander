package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/launch"
	"github.com/dualface/kander/internal/terminal"
)

type startRequest struct {
	launch.StartPreview
	root string
}

type startNotice struct{ full, compact string }

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

// backgroundStartLauncher reports whether the launcher starts the agent in a
// terminal container, leaving the board in control of this terminal.
func backgroundStartLauncher(launcher string) bool {
	return terminal.HasCapability(launcher, func(c terminal.Capabilities) bool { return c.Container })
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
		confirmDialog: confirmDialog{sequence: sequence, phase: confirmLoading},
		startRequest:  startRequest{StartPreview: launch.StartPreview{TaskID: id, State: selected.State}},
	}
	a.pendingWork = func() any {
		request, err := prepare(id)
		return confirmWork{kind: workStartPreview, sequence: sequence, id: id, payload: request, err: err}
	}
	a.resetMouseSelection()
}

func (a *App) applyStartPreview(work confirmWork) {
	dialog := a.StartConfirmation
	if dialog == nil || !dialog.matches(work.sequence, confirmLoading) || dialog.TaskID != work.id {
		return
	}
	selected := a.Model.SelectedTask()
	if selected == nil || selected.TaskID != work.id {
		a.StartConfirmation = nil
		return
	}
	request, _ := work.payload.(startRequest)
	message := ""
	switch {
	case work.err != nil:
		dialog.finish(t("tui.start_failed", work.err.Error()), true)
		if len(request.Warnings) > 0 {
			dialog.message = strings.Join(append([]string{dialog.message}, request.Warnings...), " ")
		}
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
	dialog.startRequest, dialog.phase = request, confirmReady
}

func (a *App) handleStartConfirmation(key string) {
	dialog := a.StartConfirmation
	switch dialog.handleKey(key) {
	case confirmWait:
		a.showFocusNotice(t("dialog.loading_keys"))
	case confirmCancel, confirmClose:
		a.StartConfirmation = nil
	case confirmAccept:
		run, request, sequence := a.StartTask, dialog.startRequest, dialog.sequence
		dialog.run()
		a.pendingWork = func() any {
			result, err := run(request)
			return confirmWork{kind: workStartResult, sequence: sequence, payload: result, err: err}
		}
	}
}

func (a *App) applyStartResult(work confirmWork) {
	result, _ := work.payload.(launch.StartResult)
	id := strings.TrimSpace(result.TaskID)
	if id == "" && a.StartConfirmation != nil {
		id = strings.TrimSpace(a.StartConfirmation.startRequest.TaskID)
	}
	if id == "" {
		a.invalidateSummaries()
	} else {
		a.invalidateSummaries(id)
	}
	a.requestBoardRefresh(true)
	message := ""
	compact := ""
	if work.err != nil {
		message = t("tui.start_failed", work.err.Error())
	} else {
		address := launch.OpaqueAddress(result.Plan, result.Outcome)
		message = t("tui.start_success", result.TaskID, result.Agent, result.Plan.Launcher, address)
		compact = t("tui.start_success_compact", result.Agent, result.Plan.Launcher, address)
	}
	for _, warning := range result.Warnings {
		message += " " + warning
		compact += " " + warning
	}
	if dialog := a.StartConfirmation; dialog != nil && dialog.matches(work.sequence, confirmRunning) {
		dialog.finish(message, work.err != nil)
	}
	a.showFocusNotice(message)
	if work.err == nil {
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
	a.cancelOwnedReads()
	if a.summaries != nil {
		a.summaries.Close()
		a.summaries = nil
	}
	a.Running = false
}

// confirmCapturesKeys reports whether a shared confirmation dialog owns input,
// so Ctrl+C is ignored instead of quitting the board.
func (a *App) confirmCapturesKeys() bool {
	return a.StartConfirmation != nil || a.BoardInit != nil || a.Takeover != nil || (a.Options != nil && a.Options.confirm != nil)
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

func (a *App) startTargetValid() bool {
	dialog := a.StartConfirmation
	if dialog == nil {
		return false
	}
	selected := a.Model.SelectedTask()
	return selected != nil && selected.TaskID == dialog.TaskID
}
