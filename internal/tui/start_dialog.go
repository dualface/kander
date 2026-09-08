package tui

import (
	"strings"

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
	message  string
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
		paragraphs = append(paragraphs, dialog.message)
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
	return a.renderStartDialog(paragraphs, hint)
}

func (a *App) renderStartDialog(paragraphs []string, hint string) (popupBox, string) {
	h, w := a.size()
	p := themePalette(a.Theme)
	title := t("tui.start_confirm")
	clean := func(s string) string { return printableText(ansi.Strip(s)) }
	for i := range paragraphs {
		paragraphs[i] = clean(paragraphs[i])
	}
	inner := max(1, min(w-8, max(40, blockWidth(strings.Join(paragraphs, "\n")+"\n"+hint))))
	inner = max(1, centerOptionsPopup(w, h, inner+4, 5).Width-4)
	title = ansi.Wrap(clean(title), inner, "")
	hint = ansi.Wrap(clean(hint), inner, "")
	available := max(1, h-blockHeight(title)-3)
	body := fitStartDialog(paragraphs, hint, inner, available, p)
	box := centerOptionsPopup(w, h, inner+4, blockHeight(title)+blockHeight(body)+3)
	content := styleFor("popup-title", p).Render(title) + "\n" +
		styleFor("popup-edge", p).Render(strings.Repeat("─", inner)) + "\n" +
		padBlock(body, inner, blockHeight(body), p)
	return box, withDefaultColors(popupFrame(p, inner+2).Render(content), p.ink(p.Base))
}

// fitStartDialog removes the footer gap before paragraph gaps or body rows.
// Wrap before measuring so narrow terminals retain complete horizontal content.
func fitStartDialog(paragraphs []string, hint string, width, available int, p palette) string {
	body := ansi.Wrap(strings.Join(paragraphs, "\n\n"), width, "")
	gap := "\n\n"
	if blockHeight(body)+blockHeight(hint)+1 > available {
		gap = "\n"
	}
	if blockHeight(body)+blockHeight(hint) > available {
		body = ansi.Wrap(strings.Join(paragraphs, "\n"), width, "")
	}
	lines := strings.Split(body, "\n")
	limit := max(0, available-blockHeight(hint))
	if len(lines) > limit {
		lines = lines[:limit]
	}
	styledHint := styleFor("popup-dim", p).Render(hint)
	if len(lines) == 0 {
		return styledHint
	}
	return strings.Join(lines, "\n") + gap + styledHint
}
