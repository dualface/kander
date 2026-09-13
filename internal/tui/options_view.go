package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/internal/version"
)

// innerWidth is the number of writable columns inside the popup border; rendering and measuring must use the same value.
// The panel asks for a four-column margin on each side and lets the popup clamp from there.
func (p *optionsPanel) innerWidth() int {
	screenHeight, screenWidth := p.app.size()
	return p.frame("").inner(screenWidth, screenHeight, screenWidth-12)
}

// frame is the popup shape of the settings panel. The title is filled in only when it is being rendered.
func (p *optionsPanel) frame(title string) popup {
	return popup{Title: title, TightFit: true}
}

// startForm installs a new form: Init first draws the fields into the viewport, then the natural height is measured.
// Measuring too early finds an empty viewport, dropping the trailing blanks yields 1, and the following WithHeight(1) cuts the whole page away.
// Note titles can leave View() empty until Init's follow-up update lands, so a short first measure schedules a retry.
func (p *optionsPanel) startForm(form *huh.Form) tea.Cmd {
	p.form = form
	cmd := form.Init()
	p.measureForm()
	p.pendingMeasure = p.formNatural <= 1
	return cmd
}

// measureForm records the natural height of the form. Huh's WithHeight pads the form up to the given height,
// so it has to be measured once before the height is set, for the popup to hug its content afterwards.
// NewGroup inflates the viewport with Charm's default style before the theme is applied, so View() carries trailing blanks;
// they are dropped while measuring, otherwise a field-heavy page such as review would keep a large empty tail.
func (p *optionsPanel) measureForm() {
	if p.form == nil {
		return
	}
	p.formWidth = p.innerWidth()
	p.syncFormTheme(themePalette(p.app.Theme))
	p.form.WithWidth(p.formWidth)
	p.formNatural = contentHeight(p.form.View())
}

func contentHeight(view string) int {
	n := len(trimTrailingBlank(strings.Split(view, "\n")))
	if n < 1 {
		return 1
	}
	return n
}

func optionsHeader(left string, width int) string {
	right := clipText(version.String(), width)
	rightWidth := displayWidth(right)
	if rightWidth >= width {
		return padLine(right, width)
	}
	left = clipText(left, width-rightWidth-1)
	gap := width - displayWidth(left) - rightWidth
	return left + strings.Repeat(" ", gap) + right
}

// view renders the popup and records its geometry for mouse hit testing.
// The body is rendered against the available space first, then the border is tightened to the body's actual height to avoid large blank areas.
func (p *optionsPanel) view() (popupBox, string) {
	palette := themePalette(p.app.Theme)
	screenHeight, screenWidth := p.app.size()
	inner := p.innerWidth()

	if p.showScopeTabs() {
		return p.viewTabFrame(palette, screenWidth, screenHeight, inner)
	}

	frame := p.frame("title")
	maxBody := screenHeight - frame.chrome()
	if maxBody < 3 {
		maxBody = 3
	}
	title, body := p.content(palette, inner, maxBody)
	lines := trimTrailingBlank(strings.Split(body, "\n"))
	if len(lines) > maxBody {
		lines = lines[:maxBody]
	}
	frame.Title = optionsHeader(title, inner)
	box, bodyBox, out := frame.render(palette, screenWidth, screenHeight, inner, strings.Join(lines, "\n"))
	p.box = box
	p.bodyX, p.bodyY = bodyBox.X, bodyBox.Y
	p.bodyWidth, p.bodyHeight = bodyBox.Width, bodyBox.Height
	p.bodyLines = lines
	p.headerX, p.headerY = bodyBox.X, box.Y+1
	p.tabHits = nil
	p.tabLabelRow = -1
	return box, out
}

// viewTabFrame draws Global/Project as the dialog's own header chrome:
//
//	╭──────────────────┬─────────┬─────╮
//	│ Global - Interface│ Project │ ver │
//	├──────────────────┴─────────┴─────┤
//	│ body...                          │
//	╰──────────────────────────────────╯
func (p *optionsPanel) viewTabFrame(palette palette, screenWidth, screenHeight, inner int) (popupBox, string) {
	const frameChrome = tabFrameHeaderRows + 1 // header + bottom border
	maxBody := screenHeight - frameChrome
	if maxBody < 3 {
		maxBody = 3
	}
	title, body := p.content(palette, inner, maxBody)
	lines := trimTrailingBlank(strings.Split(body, "\n"))
	if len(lines) > maxBody {
		lines = lines[:maxBody]
	}
	bodyHeight := len(lines)
	if bodyHeight < 1 {
		bodyHeight = 1
		lines = []string{""}
	}

	metrics := p.tabHeaderMetrics(inner, title)
	top, mid, join := p.renderTabFrameHeader(palette, metrics)
	edge := styleFor("popup-edge", palette)
	bottom := edge.Render(borderBottomLeft + strings.Repeat("─", inner) + borderBottomRight)

	rows := make([]string, 0, frameChrome+bodyHeight)
	rows = append(rows, top, mid, join)
	for _, line := range lines {
		rows = append(rows, edge.Render("│")+padLineFill(line, inner, palette)+edge.Render("│"))
	}
	rows = append(rows, bottom)

	totalHeight := len(rows)
	box := centerPopup(screenWidth, screenHeight, inner+2, totalHeight, popupMaxWidth, true)
	// centerPopup may widen to the popup minimum; rebuild the frame to that width.
	if resolved := max(1, box.Width-2); resolved != inner {
		inner = resolved
		metrics = p.tabHeaderMetrics(inner, title)
		top, mid, join = p.renderTabFrameHeader(palette, metrics)
		bottom = edge.Render(borderBottomLeft + strings.Repeat("─", inner) + borderBottomRight)
		rows = rows[:0]
		rows = append(rows, top, mid, join)
		for _, line := range lines {
			rows = append(rows, edge.Render("│")+padLineFill(line, inner, palette)+edge.Render("│"))
		}
		rows = append(rows, bottom)
		totalHeight = len(rows)
		box = centerPopup(screenWidth, screenHeight, inner+2, totalHeight, popupMaxWidth, true)
	}

	out := withDefaultColors(strings.Join(rows, "\n"), palette.ink(palette.Base))
	p.box = popupBox{X: box.X, Y: box.Y, Width: inner + 2, Height: totalHeight}
	p.headerX = p.box.X
	p.headerY = p.box.Y
	p.bodyX = p.box.X + 1
	p.bodyY = p.box.Y + tabFrameHeaderRows
	p.bodyWidth = inner
	p.bodyHeight = bodyHeight
	p.bodyLines = lines
	return p.box, out
}

func trimTrailingBlank(lines []string) []string {
	for len(lines) > 1 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func (p *optionsPanel) content(palette palette, width, height int) (string, string) {
	switch {
	case p.report != nil:
		count := p.report.build(palette, width)
		// One blank line is always kept between the report body and the action hint at the bottom.
		body := height - 2
		if count < body {
			body = count
		}
		if body < 1 {
			body = 1
		}
		p.report.view.Width, p.report.view.Height = width, body
		hintKey := "tui.scroll_esc_back"
		if p.flowScale != "" {
			hintKey = "tui.flow_tab_scroll_esc"
		}
		hint := styleFor("popup-dim", palette).Render(t(hintKey))
		return p.report.title, p.report.view.View() + "\n\n" + hint
	case p.loadErr != "":
		return t("tui.options_2"), styleFor("popup-warn", palette).Render(p.loadErr)
	case p.form == nil:
		line := p.spinner.View() + " " + t("tui.detecting_environment")
		if p.status != "" {
			line = p.spinner.View() + " " + p.status
		}
		return t("tui.options_2"), styleFor("popup-dim", palette).Render(line)
	}
	if p.formWidth != width || p.formNatural == 0 {
		p.measureForm()
	}
	p.syncFormTheme(palette)
	title := t("tui.kander_options")
	switch {
	case p.confirming:
		title = t("tui.close_options")
	case p.restoreConfirming:
		title = t("tui.restore_field_inherit")
	case p.current != "":
		title = sectionTitle(p.current)
	}
	// The unsaved marker stays in the title, so it remains visible inside a section too.
	if p.dirty {
		title += t("tui.unsaved")
	}
	notice, noticeLines := p.renderScopeChrome(palette, width)
	if !p.confirming && !p.restoreConfirming && notice == "" && p.overlayNotice != "" {
		indent := scopeChromeIndent
		innerWidth := width - indent
		if innerWidth < 1 {
			indent = 0
			innerWidth = width
		}
		pad := strings.Repeat(" ", indent)
		notice = pad + styleFor("popup-dim", palette).Render(clipText(p.overlayNotice, innerWidth)) + "\n\n"
		noticeLines = 2
	}
	p.chromeLines = noticeLines
	formHeight, footerGap := fitOptionsForm(p.formNatural, height-noticeLines)
	p.form.WithWidth(width)
	// Keep Huh's natural height while the content fits. Even when given the same height, WithHeight switches the
	// Group to a viewport layout, making a page that could be shown in full take part in scrolling.
	if formHeight < p.formNatural {
		p.form.WithHeight(formHeight)
	}
	hint := styleFor("popup-dim", palette).Render(p.hintLine(width))
	// An unconstrained Huh Group may carry the trailing blanks of an initialized viewport. Trim them before adding the hint,
	// otherwise those blanks become interior whitespace and the popup cannot hug its actual content.
	formView := strings.Join(trimTrailingBlank(strings.Split(p.form.View(), "\n")), "\n")
	return title, notice + formView + footerGap + hint
}

// fitOptionsForm owns the vertical layout of every section. One blank line is always kept between the form
// and the bottom hint; when space is short the Huh viewport is shortened and scrolled instead.
func fitOptionsForm(natural, available int) (height int, footerGap string) {
	const footerHeight = 2
	footerGap = "\n\n"
	height = natural
	if height > available-footerHeight {
		height = available - footerHeight
	}
	if height < 1 {
		height = 1
	}
	return height, footerGap
}

func (p *optionsPanel) pageHint() string {
	switch {
	case p.current == sectionDoctor:
		return t("tui.choose_enter_confirm_esc_skip_installation")
	case p.confirming, p.restoreConfirming:
		return t("tui.move_enter_confirm_esc_keep_editing")
	case p.current == "":
		return t("tui.move_enter_open_esc_close")
	case p.current == sectionExecution || p.current == sectionReview:
		return t("tui.field_change_type_model_ids_enter_save_esc_back")
	}
	return t("tui.field_change_enter_save_esc_back")
}

// hintLine is the key hint at the bottom of the popup, giving one accurate line for the current page.
// When Global/Project can be switched, that hint is preserved if the line must be clipped.
func (p *optionsPanel) hintLine(width int) string {
	page := p.pageHint()
	scope := p.scopeTabHint()
	if scope == "" {
		return clipText(page, width)
	}
	const sep = " · "
	joined := page + sep + scope
	if width <= 0 || displayWidth(joined) <= width {
		return joined
	}
	budget := width - displayWidth(scope) - displayWidth(sep)
	if budget < 8 {
		return clipText(scope, width)
	}
	return clipText(page, budget) + sep + scope
}

func sectionTitle(section string) string {
	switch section {
	case sectionDoctor:
		return t("menu.install_herdr")
	case sectionInterface:
		return t("tui.interface")
	case sectionExecution:
		return t("tui.execution_and_models")
	case sectionReview:
		return t("tui.review_and_models")
	case sectionReviewStages:
		return t("tui.review_stages")
	case sectionRules:
		return t("rules.modules")
	}
	return t("tui.options_2")
}

// padBlock pads the content into a rectangle of fixed width and height, so the popup border does not jitter.
func padBlock(text string, width, height int, p palette) string {
	lines := strings.Split(text, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, line := range lines {
		lines[i] = padLineFill(line, width, p)
	}
	for len(lines) < height {
		lines = append(lines, p.fillLine(width))
	}
	return strings.Join(lines, "\n")
}

// Focus markers: the prefix of a selected item is set by this package in huhTheme, while the left border of a focused field is drawn by Huh.
const (
	focusMarker = "▸"
	focusBorder = "┃"
)

// focusRange finds the line range currently holding focus in the rendered output.
// With a selected-item marker it is exact to that line, otherwise it falls back to the whole left border segment of the focused field.
func focusRange(lines []string) (lo, hi int, ok bool) {
	lo, hi = -1, -1
	for i, line := range lines {
		text := ansi.Strip(line)
		if strings.Contains(text, focusMarker) {
			return i, i, true
		}
		if strings.Contains(text, focusBorder) {
			if lo < 0 {
				lo = i
			}
			hi = i
		}
	}
	return lo, hi, lo >= 0
}

// keyCmd wraps one key press as a command handed back to Bubble Tea, which delivers it into Update asynchronously.
// The commands returned by the form must not be called in place: the cursor blink chain of a text input contains a tea.Tick,
// and running it synchronously really does sleep for over half a second, which feels like a hitch on every key press.
func keyCmd(key tea.KeyType) tea.Cmd {
	return func() tea.Msg { return tea.KeyMsg{Type: key} }
}

// repeatCmd hands the same command to Bubble Tea n times.
func repeatCmd(n int, cmd tea.Cmd) tea.Cmd {
	if n <= 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, n)
	for i := 0; i < n; i++ {
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// bodyLines returns the rendered lines of the current form body, used to convert mouse coordinates into field positions.
func (p *optionsPanel) currentBodyLines() []string {
	if p.form == nil {
		return p.bodyLines
	}
	return trimTrailingBlank(strings.Split(p.form.View(), "\n"))
}

// optionsMouseActivate is true for a left click or a same-position release.
// mapButtons never sets mouseBtn1Clicked; a real single click arrives as press then release.
func optionsMouseActivate(bstate int) bool {
	return mouseLeftClicked(bstate) || mouseButton1Released(bstate)
}

// HandleMouse gives the popup click-to-focus, double-click confirmation and wheel scrolling.
// Every focus move is turned into a command handed back to Bubble Tea rather than driving the form synchronously here.
func (p *optionsPanel) HandleMouse(x, y, bstate int) tea.Cmd {
	if p.report != nil {
		delta := mouseWheelDelta(bstate)
		if delta > 0 {
			p.report.view.ScrollDown(delta * mouseScrollStep)
		} else if delta < 0 {
			p.report.view.ScrollUp(-delta * mouseScrollStep)
		}
		return nil
	}
	if p.form == nil {
		return nil
	}
	if cmd := p.handleTabMouse(x, y, bstate); cmd != nil {
		return cmd
	}
	if delta := mouseWheelDelta(bstate); delta != 0 {
		if delta > 0 {
			return keyCmd(tea.KeyDown)
		}
		return keyCmd(tea.KeyUp)
	}
	if !optionsMouseActivate(bstate) {
		return nil
	}
	if x < p.bodyX || x >= p.bodyX+p.bodyWidth || y < p.bodyY || y >= p.bodyY+p.bodyHeight {
		return nil
	}
	target := y - p.bodyY - p.chromeLines
	lines := p.currentBodyLines()
	if target < 0 || target >= len(lines) {
		return nil
	}
	move, already := p.focusMove(lines, target)
	if already || mouseLeftDoubleClicked(bstate) {
		// Clicking an already focused line, or double-clicking, counts as confirmation.
		return tea.Batch(move, keyCmd(tea.KeyEnter))
	}
	return move
}

// focusMove computes the commands needed to move focus to the target line and reports whether the target already has focus.
// Single-choice pages (the root menu, the close confirmation) move by candidate line; settings pages convert using the actual rendered height
// of each field, independently of whether blank lines sit between them.
func (p *optionsPanel) focusMove(lines []string, target int) (tea.Cmd, bool) {
	for i, line := range lines {
		if strings.Contains(ansi.Strip(line), focusMarker) {
			delta := target - i
			switch {
			case delta == 0:
				return nil, true
			case delta > 0:
				return repeatCmd(delta, keyCmd(tea.KeyDown)), false
			default:
				return repeatCmd(-delta, keyCmd(tea.KeyUp)), false
			}
		}
	}
	if p.bind == nil || len(p.bind.formFields) == 0 {
		return nil, false
	}
	row, focusable := 0, 0
	wanted, current := -1, -1
	for _, field := range p.bind.formFields {
		view := field.View()
		height := lipgloss.Height(view)
		if !field.Skip() {
			if target >= row && target < row+height {
				wanted = focusable
			}
			if strings.Contains(ansi.Strip(view), focusBorder) {
				current = focusable
			}
			focusable++
		}
		row += height
	}
	if wanted < 0 || current < 0 {
		return nil, false
	}
	delta := wanted - current
	switch {
	case delta == 0:
		return nil, true
	case delta > 0:
		return repeatCmd(delta, huh.NextField), false
	default:
		return repeatCmd(-delta, huh.PrevField), false
	}
}
