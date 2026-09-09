package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/x/ansi"
)

type startPhase int

const (
	startLoading startPhase = iota
	startReady
	startRunning
	startFinished
)

type startDialog struct {
	startRequest
	sequence uint64
	phase    startPhase
	failed   bool
	message  string
	bodyView viewport.Model
}

func startDialogTitle(dialog *startDialog) string {
	switch dialog.phase {
	case startLoading:
		return t("tui.start_loading")
	case startRunning:
		return t("tui.start_title_starting")
	case startFinished:
		if dialog.failed {
			return t("tui.start_title_failed")
		}
		return t("tui.start_title_started")
	default:
		return t("tui.start_confirm")
	}
}

type startPreviewResult struct {
	request  startRequest
	err      error
	taskID   string
	sequence uint64
}

func (a *App) renderStartConfirmation() (popupBox, string) {
	dialog := a.StartConfirmation
	paragraphs := []string{dialog.TaskID + "\n" + t("tui.start_state", dialog.State)}
	hint := t("tui.start_confirm_keys")
	switch dialog.phase {
	case startLoading:
		placeholder := t("tui.start_loading")
		paragraphs = append(paragraphs, t("tui.start_settings", placeholder, placeholder))
		hint = t("tui.start_loading_keys")
	case startFinished:
		paragraphs = []string{dialog.message}
		hint = t("tui.start_result_keys")
	default:
		paragraphs = append(paragraphs, t("tui.start_settings", dialog.Agent, dialog.Launcher))
		if dialog.State == "backlog" {
			paragraphs = append(paragraphs, t("tui.start_backlog"))
		}
		paragraphs = append(paragraphs, dialog.Warnings...)
		if dialog.phase == startRunning {
			hint = t("tui.start_starting", dialog.TaskID)
		}
	}
	return a.renderStartDialog(paragraphs, hint, startDialogTitle(dialog))
}

func (a *App) renderStartDialog(paragraphs []string, hint, title string) (popupBox, string) {
	h, w := a.size()
	p := themePalette(a.Theme)
	clean := func(s string) string { return printableText(ansi.Strip(s)) }
	for i := range paragraphs {
		paragraphs[i] = clean(paragraphs[i])
	}
	frame := popup{TightFit: true}
	inner := frame.inner(w, h, max(1, min(w-8, max(40, blockWidth(strings.Join(paragraphs, "\n")+"\n"+hint)))))
	// The hint rides inside the body rather than in the frame: it moves up against the paragraphs
	// when the dialog runs out of height, which the plain hint row cannot do.
	frame.Title = ansi.Wrap(clean(title), inner, "")
	hint = ansi.Wrap(clean(hint), inner, "")
	available := max(1, h-blockHeight(frame.Title)-3)
	body := fitStartDialog(paragraphs, hint, inner, available, p, &a.StartConfirmation.bodyView)
	box, _, out := frame.render(p, w, h, inner, body)
	return box, out
}

// fitStartDialog removes the footer gap before paragraph gaps or shrinking the body viewport.
// Wrap before measuring so narrow terminals retain complete horizontal content.
func fitStartDialog(paragraphs []string, hint string, width, available int, p palette, view *viewport.Model) string {
	body := ansi.Wrap(strings.Join(paragraphs, "\n\n"), width, "")
	gap := "\n\n"
	if blockHeight(body)+blockHeight(hint)+1 > available {
		gap = "\n"
	}
	if blockHeight(body)+blockHeight(hint) > available {
		body = ansi.Wrap(strings.Join(paragraphs, "\n"), width, "")
	}
	view.Width, view.Height = width, max(0, min(blockHeight(body), available-blockHeight(hint)))
	view.SetContent(body)
	view.SetYOffset(view.YOffset)
	styledHint := styleFor("popup-dim", p).Render(hint)
	if view.Height == 0 {
		return styledHint
	}
	return view.View() + gap + styledHint
}

func (a *App) handleStartMouse(x, y, buttons int) {
	dialog := a.StartConfirmation
	delta := mouseWheelDelta(buttons)
	if delta == 0 {
		return
	}
	if dialog.phase == startLoading {
		a.handleBoardMouse(x, y, buttons)
		selected := a.Model.SelectedTask()
		if selected == nil || selected.TaskID != dialog.TaskID {
			a.StartConfirmation = nil
		}
		return
	}
	if delta > 0 {
		dialog.bodyView.ScrollDown(delta)
	} else {
		dialog.bodyView.ScrollUp(-delta)
	}
}
