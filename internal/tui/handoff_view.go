package tui

import (
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/issue"
)

type handoffLayout struct {
	box        popupBox
	body       popupBox
	inner      int
	bodyHeight int
}

func (a *App) handoffFrame() popup {
	state := a.Handoff
	frame := popup{MaxWidth: issuesMaxWidth}
	if state == nil {
		return frame
	}
	switch {
	case state.editing:
		frame.Title = t("tui.handoff_editing", handoffFieldLabel(state.focus))
	case state.phase == handoffFinished:
		if state.failed {
			frame.Title = t("tui.handoff_title_failed")
		} else {
			frame.Title = t("tui.handoff_title_done")
		}
	default:
		frame.Title = t("tui.handoff_title", strconv.Itoa(state.number), issue.SanitizeRemoteText(state.issueTitle))
	}
	frame.Hint = handoffHint(state)
	return frame
}

func handoffHint(state *handoffState) string {
	if state.editing {
		return t("tui.handoff_editor_keys")
	}
	switch state.phase {
	case handoffLoading:
		return t("tui.handoff_loading_keys")
	case handoffEdit:
		return t("tui.handoff_edit_keys")
	case handoffReview:
		return t("tui.handoff_review_keys")
	case handoffSaving:
		return t("tui.handoff_saving")
	case handoffConfirm:
		if state.gateErr != nil || !state.previewReady {
			return t("tui.handoff_close_keys")
		}
		return t("tui.handoff_confirm_keys")
	case handoffRunning:
		return t("tui.handoff_starting")
	default:
		return t("tui.handoff_result_keys")
	}
}

func (a *App) handoffLayout() handoffLayout {
	h, w := a.size()
	frame := a.handoffFrame()
	wantedInner := clampInt(w-4, 20, issuesMaxWidth-4)
	inner := frame.inner(w, h, wantedInner)
	bodyHeight := h - frame.chrome() - 4
	if bodyHeight < 6 {
		bodyHeight = 6
	}
	box := centerPopup(w, h, inner+4, frame.chrome()+bodyHeight, frame.MaxWidth, frame.TightFit)
	inner = max(1, box.Width-4)
	bodyHeight = max(1, box.Height-frame.chrome())
	body := popupBox{X: box.X + 2, Y: box.Y + 1, Width: inner, Height: bodyHeight}
	if frame.Title != "" {
		body.Y += blockHeight(frame.Title) + 1
	}
	return handoffLayout{box: box, body: body, inner: inner, bodyHeight: bodyHeight}
}

func (a *App) renderHandoff() (popupBox, string) {
	p := themePalette(a.Theme)
	h, w := a.size()
	layout := a.handoffLayout()
	frame := a.handoffFrame()
	box, _, out := frame.render(p, w, h, layout.inner, a.handoffBody(layout, p))
	return box, out
}

func (a *App) handoffBody(layout handoffLayout, p palette) string {
	state := a.Handoff
	if state == nil {
		return ""
	}
	switch {
	case state.editing:
		return a.handoffEditorBody(layout, p)
	case state.phase == handoffEdit:
		return a.handoffFormBody(layout, p)
	case state.phase == handoffReview:
		return a.handoffReviewBody(layout, p)
	case state.phase == handoffConfirm:
		return a.handoffConfirmBody(layout, p)
	case state.phase == handoffFinished:
		return a.handoffTextBody(layout, p, state.message, state.failed)
	default:
		return a.handoffTextBody(layout, p, handoffStatusText(state), false)
	}
}

func handoffStatusText(state *handoffState) string {
	switch state.phase {
	case handoffLoading:
		return t("tui.handoff_loading_card")
	case handoffSaving:
		return t("tui.handoff_saving")
	case handoffRunning:
		return t("tui.handoff_starting")
	}
	return ""
}

// handoffFormBody renders the contract list. The notice row sits above the
// fields so a validation message never replaces the draft it is about.
func (a *App) handoffFormBody(layout handoffLayout, p palette) string {
	state := a.Handoff
	width := layout.inner
	lines := []string{}
	if state.notice != "" {
		lines = append(lines, styleFor("popup-warn", p).Render(clipText(state.notice, width)))
	} else {
		lines = append(lines, styleFor("popup-dim", p).Render(clipText(t("tui.handoff_undecided"), width)))
	}
	if state.language != "" {
		lines = append(lines, styleFor("popup-dim", p).Render(clipText(t("tui.handoff_language_fixed", state.language), width)))
	}
	lines = append(lines, "")
	focusStart, focusEnd := len(lines), len(lines)
	for id, block := range handoffFormBlocks(state, width) {
		field := handoffFieldID(id)
		if field == state.focus {
			focusStart = len(lines)
		}
		for i, line := range block {
			style := styleFor("popup", p)
			if field == state.focus {
				style = styleFor("popup-sel", p)
			} else if i > 0 {
				style = styleFor("popup-dim", p)
			}
			lines = append(lines, style.Render(padLine(line, width)))
		}
		if field == state.focus {
			focusEnd = len(lines)
		}
	}
	handoffEnsureVisible(state, len(lines), focusStart, focusEnd, layout.bodyHeight)
	return handoffSlice(lines, state.scroll, layout.bodyHeight, width, p)
}

func handoffFormBlocks(state *handoffState, width int) [][]string {
	blocks := make([][]string, 0, handoffFieldCount)
	for id, spec := range handoffFieldSpecs {
		field := handoffFieldID(id)
		value := state.value(field)
		label := handoffFieldLabel(field)
		if spec.selectValue() {
			marker := ""
			if field == handoffType || field == handoffSize {
				marker = "! "
			}
			blocks = append(blocks, []string{clipText(marker+label+": < "+value+" >", width)})
			continue
		}
		lines := []string{clipText(label+":", width)}
		preview := wrapText(value, max(1, width-4))
		for i := 0; i < len(preview) && i < 2; i++ {
			lines = append(lines, "    "+preview[i])
		}
		blocks = append(blocks, lines)
	}
	return blocks
}

// handoffReviewBody lists the four creation self-review items above the
// attestation and the conclusion the creator has to type.
func (a *App) handoffReviewBody(layout handoffLayout, p palette) string {
	state := a.Handoff
	width := layout.inner
	lines := []string{}
	for _, id := range []string{
		"tui.handoff_review_item_1", "tui.handoff_review_item_2",
		"tui.handoff_review_item_3", "tui.handoff_review_item_4",
	} {
		for _, line := range wrapText(t(id), width) {
			lines = append(lines, styleFor("popup", p).Render(padLine(line, width)))
		}
	}
	lines = append(lines, "")
	focusStart := len(lines)
	checkbox := "[ ]"
	if state.attest {
		checkbox = "[x]"
	}
	style := styleFor("popup", p)
	if state.focus == handoffAttest {
		style = styleFor("popup-sel", p)
	}
	lines = append(lines, style.Render(padLine(checkbox+" "+t("tui.handoff_attest"), width)))
	style = styleFor("popup", p)
	if state.focus == handoffConclusion {
		style = styleFor("popup-sel", p)
	}
	lines = append(lines, style.Render(padLine(t("tui.handoff_conclusion")+":", width)))
	conclusion := t("tui.handoff_conclusion_empty")
	conclusionStyle := styleFor("popup-dim", p)
	if strings.TrimSpace(state.conclusion) != "" {
		conclusion = state.conclusion
		conclusionStyle = styleFor("popup", p)
	}
	if state.focus == handoffConclusion {
		conclusionStyle = styleFor("popup-sel", p)
	}
	for _, line := range wrapText(conclusion, max(1, width-4)) {
		lines = append(lines, conclusionStyle.Render(padLine("    "+line, width)))
	}
	if state.notice != "" {
		lines = append(lines, styleFor("popup-warn", p).Render(clipText(state.notice, width)))
	}
	focusEnd := len(lines)
	handoffEnsureVisible(state, len(lines), focusStart, focusEnd, layout.bodyHeight)
	return handoffSlice(lines, state.scroll, layout.bodyHeight, width, p)
}

// handoffEditorBody is the focused field editor. The caret line is kept inside
// the visible window; the notice row appears under the text when a save failed
// and the editor stayed open.
func (a *App) handoffEditorBody(layout handoffLayout, p palette) string {
	state := a.Handoff
	width := layout.inner
	text := state.value(state.focus)
	lines := handoffWrapLines(text, width)
	caretLine, caretCol := handoffCaretPosition(text, state.caret, width)
	height := layout.bodyHeight
	if state.notice != "" {
		height--
	}
	if caretLine < state.scroll {
		state.scroll = caretLine
	}
	if caretLine >= state.scroll+height {
		state.scroll = caretLine - height + 1
	}
	if maxScroll := len(lines) - height; state.scroll > maxScroll {
		state.scroll = maxScroll
	}
	if state.scroll < 0 {
		state.scroll = 0
	}
	out := make([]string, 0, layout.bodyHeight)
	for row := state.scroll; row < len(lines) && len(out) < height; row++ {
		line := lines[row]
		if row == caretLine {
			line = handoffHighlightCaret(line, caretCol, p)
			out = append(out, padLineFill(line, width, p))
			continue
		}
		out = append(out, styleFor("popup", p).Render(padLine(line, width)))
	}
	if state.notice != "" {
		out = append(out, styleFor("popup-warn", p).Render(clipText(state.notice, width)))
	}
	return handoffSlice(out, 0, layout.bodyHeight, width, p)
}

func (a *App) handoffConfirmBody(layout handoffLayout, p palette) string {
	state := a.Handoff
	width := layout.inner
	repository := state.repository
	url, err := repository.IssueURL(state.number)
	if err != nil {
		url = ""
	}
	lines := []string{}
	add := func(label, value string, style string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		lines = append(lines, styleFor(style, p).Render(clipText(label+": "+value, width)))
	}
	add(t("tui.handoff_issue_label"), issue.SanitizeRemoteText(state.issueTitle), "popup")
	add(t("tui.handoff_source_url"), url, "popup-dim")
	add(t("tui.handoff_task"), state.taskID, "popup")
	add(t("tui.handoff_size_language"), state.size+"  "+state.language, "popup")
	if state.previewReady {
		add(t("tui.handoff_agent"), state.request.Agent, "popup")
		add(t("tui.handoff_launcher"), state.request.Launcher, "popup")
	} else {
		add(t("tui.handoff_agent"), t("tui.handoff_preview_loading"), "popup-dim")
	}
	add(t("tui.handoff_transition"), "backlog → todo → working", "popup")
	lines = append(lines, "")
	if state.gateErr != nil {
		for _, line := range wrapText(t("tui.handoff_gate_blocked", state.gateErr.Error()), width) {
			lines = append(lines, styleFor("popup-warn", p).Render(padLine(line, width)))
		}
		if board.RequiresCardReview(state.size, state.source) {
			for _, line := range wrapText(t("tui.handoff_gate_review_hint"), width) {
				lines = append(lines, styleFor("popup-dim", p).Render(padLine(line, width)))
			}
		}
	} else if state.previewReady {
		for _, line := range wrapText(t("tui.handoff_gate_ready"), width) {
			lines = append(lines, styleFor("popup-ok", p).Render(padLine(line, width)))
		}
		if !backgroundStartLauncher(state.request.Launcher) {
			for _, line := range wrapText(t("tui.start_use_cli", state.request.Launcher), width) {
				lines = append(lines, styleFor("popup-warn", p).Render(padLine(line, width)))
			}
		}
	}
	if state.notice != "" {
		for _, line := range wrapText(state.notice, width) {
			lines = append(lines, styleFor("popup-warn", p).Render(padLine(line, width)))
		}
	}
	return handoffSlice(lines, 0, layout.bodyHeight, width, p)
}

func (a *App) handoffTextBody(layout handoffLayout, p palette, message string, failed bool) string {
	width := layout.inner
	style := "popup"
	if failed {
		style = "popup-warn"
	}
	lines := []string{}
	for _, line := range wrapText(message, width) {
		lines = append(lines, styleFor(style, p).Render(padLine(line, width)))
	}
	if failed {
		hint := t("tui.handoff_retry_hint")
		if state := a.Handoff; state != nil && state.actualState != "" && state.actualState != "backlog" {
			hint = t("tui.handoff_retry_board")
		}
		lines = append(lines, styleFor("popup-dim", p).Render(clipText(hint, width)))
	}
	return handoffSlice(lines, 0, layout.bodyHeight, width, p)
}

// handoffEnsureVisible scrolls the list so the focused block is inside the window.
func handoffEnsureVisible(state *handoffState, total, focusStart, focusEnd, height int) {
	if height < 1 {
		return
	}
	if focusEnd-focusStart > height {
		focusEnd = focusStart + height
	}
	if focusStart < state.scroll {
		state.scroll = focusStart
	}
	if focusEnd > state.scroll+height {
		state.scroll = focusEnd - height
	}
	if maxScroll := total - height; state.scroll > maxScroll {
		state.scroll = maxScroll
	}
	if state.scroll < 0 {
		state.scroll = 0
	}
}

func handoffSlice(lines []string, start, height, width int, p palette) string {
	if start < 0 {
		start = 0
	}
	if start > len(lines) {
		start = len(lines)
	}
	end := start + height
	if end > len(lines) {
		end = len(lines)
	}
	out := append([]string{}, lines[start:end]...)
	for len(out) < height {
		out = append(out, p.fillLine(width))
	}
	return strings.Join(out, "\n")
}

func handoffWrapLines(text string, width int) []string {
	var out []string
	for _, source := range strings.Split(text, "\n") {
		out = append(out, wrapText(source, width)...)
	}
	if len(out) == 0 {
		out = append(out, "")
	}
	return out
}

// handoffCaretPosition maps one rune offset in the unwrapped text to the
// wrapped line and column that show it.
func handoffCaretPosition(text string, caret, width int) (int, int) {
	caret = clampInt(caret, 0, runeCount(text))
	remaining := caret
	lineIndex := 0
	last := 0
	for _, source := range strings.Split(text, "\n") {
		for _, segment := range wrapText(source, width) {
			count := runeCount(segment)
			if remaining <= count {
				return lineIndex, remaining
			}
			remaining -= count
			lineIndex++
		}
		if remaining > 0 {
			remaining--
		}
		last = lineIndex
	}
	return max(0, last-1), 0
}

func handoffHighlightCaret(line string, column int, p palette) string {
	runes := []rune(line)
	var b strings.Builder
	for i, r := range runes {
		if i == column {
			b.WriteString(styleFor("caret", p).Render(string(r)))
			continue
		}
		b.WriteRune(r)
	}
	if column >= len(runes) {
		b.WriteString(styleFor("caret", p).Render(" "))
	}
	return b.String()
}

// handleHandoffKey routes keys by phase. Loading can be cancelled; saving and
// running own the write and cannot be interrupted from the keyboard.
func (a *App) handleHandoffKey(key string) {
	state := a.Handoff
	if state == nil {
		return
	}
	if key == "?" {
		a.Help = true
		return
	}
	switch state.phase {
	case handoffLoading:
		if key == "esc" || key == "q" {
			a.closeHandoff()
		}
		return
	case handoffSaving, handoffRunning:
		return
	case handoffFinished:
		a.closeHandoff()
		return
	}
	if state.editing {
		a.handleHandoffEditorKey(key)
		return
	}
	if state.phase == handoffEdit {
		a.handleHandoffEditKey(key)
		return
	}
	if state.phase == handoffReview {
		a.handleHandoffReviewKey(key)
		return
	}
	a.handleHandoffConfirmKey(key)
}

func (a *App) handleHandoffEditKey(key string) {
	state := a.Handoff
	switch key {
	case "esc", "q":
		a.closeHandoff()
	case "up", "k", "shift-tab":
		a.handoffMoveFocus(-1)
	case "down", "j", "tab":
		a.handoffMoveFocus(1)
	case "left", "h":
		cycleHandoffValue(state, -1)
	case "right", "l":
		cycleHandoffValue(state, 1)
	case "enter":
		spec := handoffFieldSpecs[state.focus]
		if spec.selectValue() {
			cycleHandoffValue(state, 1)
			return
		}
		a.handoffStartEditing()
	case "ctrl-s":
		a.handoffValidateAndReview()
	}
}

func (a *App) handleHandoffReviewKey(key string) {
	state := a.Handoff
	switch key {
	case "esc":
		state.phase = handoffEdit
		state.focus = handoffDiscussion
		state.notice = ""
		state.scroll = 0
	case "q":
		a.closeHandoff()
	case "up", "k", "shift-tab":
		if state.focus == handoffConclusion {
			state.focus = handoffAttest
		} else {
			state.focus = handoffConclusion
		}
	case "down", "j", "tab":
		if state.focus == handoffAttest {
			state.focus = handoffConclusion
		} else {
			state.focus = handoffAttest
		}
	case " ":
		if state.focus == handoffAttest {
			state.attest = !state.attest
		}
	case "enter":
		if state.focus == handoffAttest {
			state.attest = !state.attest
			return
		}
		a.handoffStartEditing()
	case "ctrl-s":
		a.handoffSubmitReview()
	}
}

func (a *App) handleHandoffConfirmKey(key string) {
	switch key {
	case "esc", "q", "n":
		a.closeHandoff()
	case "y":
		a.handoffStartConfirmed()
	}
}

func (a *App) handleHandoffEditorKey(key string) {
	state := a.Handoff
	switch key {
	case "esc":
		state.editing = false
	case "tab":
		state.editing = false
		a.handoffMoveFocus(1)
	case "shift-tab":
		state.editing = false
		a.handoffMoveFocus(-1)
	case "ctrl-s":
		state.editing = false
		if state.phase == handoffReview {
			a.handoffSubmitReview()
			return
		}
		a.handoffValidateAndReview()
	case "enter":
		a.handoffInsert("\n")
	case "backspace":
		a.handoffBackspace()
	case "left":
		state.caret = clampInt(state.caret-1, 0, runeCount(state.value(state.focus)))
	case "right":
		state.caret = clampInt(state.caret+1, 0, runeCount(state.value(state.focus)))
	case "up":
		a.handoffMoveCaretLine(-1)
	case "down":
		a.handoffMoveCaretLine(1)
	case "pgup":
		a.handoffMoveCaretLine(-5)
	case "pgdn":
		a.handoffMoveCaretLine(5)
	case "home":
		state.caret = handoffLineStart(state.value(state.focus), state.caret)
	case "end":
		state.caret = handoffLineEnd(state.value(state.focus), state.caret)
	default:
		if isPrintableKey(key) {
			a.handoffInsert(key)
		}
	}
}

func (a *App) handoffStartEditing() {
	state := a.Handoff
	state.editing = true
	state.caret = runeCount(state.value(state.focus))
	state.scroll = 0
	state.notice = ""
}

func (a *App) handoffMoveFocus(delta int) {
	state := a.Handoff
	state.editing = false
	if state.phase == handoffReview {
		if state.focus == handoffAttest {
			state.focus = handoffConclusion
			return
		}
		state.focus = handoffAttest
		return
	}
	next := (int(state.focus) + delta) % int(handoffFieldCount)
	if next < 0 {
		next += int(handoffFieldCount)
	}
	state.focus = handoffFieldID(next)
}

func (a *App) handoffInsert(value string) {
	state := a.Handoff
	text := []rune(state.value(state.focus))
	caret := clampInt(state.caret, 0, len(text))
	insert := []rune(value)
	text = append(text[:caret], append(insert, text[caret:]...)...)
	state.setValue(state.focus, string(text))
	state.caret = caret + len(insert)
}

func (a *App) handoffBackspace() {
	state := a.Handoff
	text := []rune(state.value(state.focus))
	if state.caret <= 0 || state.caret > len(text) {
		return
	}
	text = append(text[:state.caret-1], text[state.caret:]...)
	state.setValue(state.focus, string(text))
	state.caret--
}

func (a *App) handoffMoveCaretLine(delta int) {
	state := a.Handoff
	text := state.value(state.focus)
	caret := clampInt(state.caret, 0, runeCount(text))
	if delta < 0 {
		start := handoffLineStart(text, caret)
		if start == 0 {
			state.caret = 0
			return
		}
		previousEnd := start - 1
		previousStart := handoffLineStart(text, previousEnd)
		column := caret - start
		state.caret = clampInt(previousStart+column, previousStart, previousEnd)
		return
	}
	end := handoffLineEnd(text, caret)
	if end >= runeCount(text) {
		state.caret = runeCount(text)
		return
	}
	nextStart := end + 1
	nextEnd := handoffLineEnd(text, nextStart)
	column := caret - handoffLineStart(text, caret)
	state.caret = clampInt(nextStart+column, nextStart, nextEnd)
}

func handoffLineStart(text string, caret int) int {
	runes := []rune(text)
	caret = clampInt(caret, 0, len(runes))
	for i := caret - 1; i >= 0; i-- {
		if runes[i] == '\n' {
			return i + 1
		}
	}
	return 0
}

func handoffLineEnd(text string, caret int) int {
	runes := []rune(text)
	caret = clampInt(caret, 0, len(runes))
	for i := caret; i < len(runes); i++ {
		if runes[i] == '\n' {
			return i
		}
	}
	return len(runes)
}

func (a *App) handleHandoffMouse(x, y, bstate int) {
	state := a.Handoff
	if state == nil {
		return
	}
	delta := mouseWheelDelta(bstate)
	if delta == 0 {
		return
	}
	if state.editing {
		state.scroll += delta * mouseScrollStep
	} else {
		state.scroll += delta * issuesItemLines
	}
	if state.scroll < 0 {
		state.scroll = 0
	}
}
