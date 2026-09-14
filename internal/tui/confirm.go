package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/x/ansi"
)

// confirmPhase is the shared four-stage confirmation state machine.
type confirmPhase int

const (
	confirmLoading confirmPhase = iota
	confirmReady
	confirmRunning
	confirmFinished
)

// confirmAction is the shared y / n / Esc contract for one key.
type confirmAction int

const (
	confirmIgnore confirmAction = iota
	confirmWait
	confirmCancel
	confirmAccept
	confirmClose
)

// confirmKind tags one pendingWork payload so a single apply path can
// finish the matching dialog.
type confirmKind int

const (
	workStartPreview confirmKind = iota + 1
	workStartResult
	workBoardInitPreview
	workBoardInitResult
	workTakeoverPreview
	workTakeoverResult
)

// confirmWork is the shared preview/result envelope. Callers put their
// domain value in payload; sequence drops stale deliveries.
type confirmWork struct {
	kind     confirmKind
	sequence uint64
	id       string
	payload  any
	err      error
}

// confirmDialog is the shared confirmation dialog: phase machine, sequence
// guard, body scrolling, and unified rendering. Callers supply titles,
// paragraphs, optional preview/execute closures, and target validity.
type confirmDialog struct {
	sequence uint64
	phase    confirmPhase
	failed   bool
	message  string
	bodyView viewport.Model
}

func confirmKeyAction(phase confirmPhase, key string) confirmAction {
	switch phase {
	case confirmRunning:
		return confirmIgnore
	case confirmFinished:
		return confirmClose
	case confirmLoading:
		switch key {
		case "y", "Y":
			return confirmWait
		case "n", "N", "esc":
			return confirmCancel
		default:
			return confirmIgnore
		}
	default:
		switch key {
		case "y", "Y":
			return confirmAccept
		case "n", "N", "esc":
			return confirmCancel
		default:
			return confirmIgnore
		}
	}
}

func (d *confirmDialog) handleKey(key string) confirmAction {
	if d == nil {
		return confirmIgnore
	}
	return confirmKeyAction(d.phase, key)
}

func (d *confirmDialog) matches(sequence uint64, phase confirmPhase) bool {
	return d != nil && d.sequence == sequence && d.phase == phase
}

func (d *confirmDialog) scroll(delta int) {
	if d == nil || delta == 0 {
		return
	}
	if delta > 0 {
		d.bodyView.ScrollDown(delta)
	} else {
		d.bodyView.ScrollUp(-delta)
	}
}

func (d *confirmDialog) finish(message string, failed bool) {
	d.phase = confirmFinished
	d.message = message
	d.failed = failed
	d.bodyView.GotoTop()
}

func (d *confirmDialog) run() {
	d.phase = confirmRunning
	d.failed = false
	d.message = ""
}

func (d *confirmDialog) handleWheel(x, y, buttons int, loadingWheel func(int, int, int), valid func() bool) bool {
	delta := mouseWheelDelta(buttons)
	if d == nil || delta == 0 {
		return false
	}
	if d.phase == confirmLoading {
		if loadingWheel != nil {
			loadingWheel(x, y, buttons)
		}
		return valid != nil && !valid()
	}
	d.scroll(delta)
	return false
}

func confirmHint(phase confirmPhase) string {
	switch phase {
	case confirmLoading:
		return t("dialog.loading_keys")
	case confirmRunning:
		return t("dialog.running_keys")
	case confirmFinished:
		return t("dialog.result_keys")
	default:
		return t("dialog.keys")
	}
}

func confirmSharedTitle(phase confirmPhase, failed bool) string {
	switch phase {
	case confirmLoading:
		return t("dialog.loading")
	case confirmRunning:
		return t("dialog.title_starting")
	case confirmFinished:
		if failed {
			return t("dialog.title_failed")
		}
		return t("dialog.title_done")
	default:
		return ""
	}
}

func confirmSettings(agent, launcher string) string {
	if agent == "" && launcher == "" {
		placeholder := t("dialog.loading")
		return t("dialog.settings", placeholder, placeholder)
	}
	return t("dialog.settings", agent, launcher)
}

func (a *App) renderConfirm(paragraphs []string, hint, title string, view *viewport.Model) (popupBox, string) {
	h, w := a.size()
	return renderConfirmDialog(w, h, a.Theme, paragraphs, hint, title, view)
}

// renderConfirmDialog draws one confirmation body; view scrolls the paragraphs
// and belongs to the dialog that owns the body.
func renderConfirmDialog(width, height int, theme string, paragraphs []string, hint, title string, view *viewport.Model) (popupBox, string) {
	p := themePalette(theme)
	clean := func(s string) string { return printableText(ansi.Strip(s)) }
	for i := range paragraphs {
		paragraphs[i] = clean(paragraphs[i])
	}
	frame := popup{TightFit: true}
	inner := frame.inner(width, height, max(1, min(width-8, max(40, blockWidth(strings.Join(paragraphs, "\n")+"\n"+hint)))))
	// The hint rides inside the body rather than in the frame: it moves up against the paragraphs
	// when the dialog runs out of height, which the plain hint row cannot do.
	frame.Title = ansi.Wrap(clean(title), inner, "")
	hint = ansi.Wrap(clean(hint), inner, "")
	available := max(1, height-blockHeight(frame.Title)-3)
	body := fitConfirmDialog(paragraphs, hint, inner, available, p, view)
	box, _, out := frame.render(p, width, height, inner, body)
	return box, out
}

// fitConfirmDialog removes the footer gap before paragraph gaps or shrinking the body viewport.
// Wrap before measuring so narrow terminals retain complete horizontal content.
func fitConfirmDialog(paragraphs []string, hint string, width, available int, p palette, view *viewport.Model) string {
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

func (a *App) applyConfirmWork(work confirmWork) {
	switch work.kind {
	case workStartPreview:
		a.applyStartPreview(work)
	case workStartResult:
		a.applyStartResult(work)
	case workBoardInitPreview:
		a.applyBoardInitPreview(work)
	case workBoardInitResult:
		a.applyBoardInitResult(work)
	case workTakeoverPreview:
		a.applyTakeoverPreview(work)
	case workTakeoverResult:
		a.applyTakeoverResult(work)
	}
}
