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
func (p *optionsPanel) innerWidth() int {
	_, screenWidth := p.app.size()
	boxWidth := screenWidth - 8
	if boxWidth > popupMaxWidth {
		boxWidth = popupMaxWidth
	}
	if boxWidth < popupMinWidth {
		boxWidth = popupMinWidth
	}
	if boxWidth > screenWidth {
		boxWidth = screenWidth
	}
	inner := boxWidth - 4
	if inner < 1 {
		inner = 1
	}
	return inner
}

// startForm installs a new form: Init first draws the fields into the viewport, then the natural height is measured.
// Measuring too early finds an empty viewport, dropping the trailing blanks yields 1, and the following WithHeight(1) cuts the whole page away.
func (p *optionsPanel) startForm(form *huh.Form) tea.Cmd {
	p.form = form
	cmd := form.Init()
	p.measureForm()
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
	boxWidth := inner + 4
	// The title line, the separator and the top and bottom borders.
	chrome := 4
	// When the content does not fit, the options popup may reach the top and bottom edges of the terminal, so the rule modules do not
	// start scrolling merely because of the popup's outer margin.
	maxBody := screenHeight - chrome
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
	}

	box := centerOptionsPopup(screenWidth, screenHeight, boxWidth, bodyHeight+chrome)
	p.box = box
	// The body starts after the top border, the title line and the separator; on the left it gives up one column each to the border and the padding.
	p.bodyX, p.bodyY = box.X+2, box.Y+3
	p.bodyWidth, p.bodyHeight = inner, bodyHeight
	p.bodyLines = lines

	rule := styleFor("popup-edge", palette).Render(strings.Repeat("─", inner))
	content := strings.Join([]string{
		styleFor("popup-title", palette).Render(optionsHeader(title, inner)),
		rule,
		padBlock(strings.Join(lines, "\n"), inner, bodyHeight, palette),
	}, "\n")
	return box, withDefaultColors(popupFrame(palette, boxWidth-2).Render(content), palette.ink(palette.Base))
}

// centerOptionsPopup keeps the ordinary popup margins while the content fits; only a height shortage consumes them.
func centerOptionsPopup(screenWidth, screenHeight, wantWidth, wantHeight int) popupBox {
	box := centerPopup(screenWidth, screenHeight, wantWidth, wantHeight)
	height := wantHeight
	if height > screenHeight {
		height = screenHeight
	}
	if height > box.Height {
		box.Height = height
		box.Y = (screenHeight - height) / 2
	}
	return box
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
		hint := styleFor("popup-dim", palette).Render(t("tui.scroll_esc_back"))
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
	formHeight, footerGap := fitOptionsForm(p.formNatural, height)
	p.form.WithWidth(width)
	// Keep Huh's natural height while the content fits. Even when given the same height, WithHeight switches the
	// Group to a viewport layout, making a page that could be shown in full take part in scrolling.
	if formHeight < p.formNatural {
		p.form.WithHeight(formHeight)
	}
	title := t("tui.kander_options")
	switch {
	case p.confirming:
		title = t("tui.close_options")
	case p.current != "":
		title = sectionTitle(p.current)
	}
	// The unsaved marker stays in the title, so it remains visible inside a section too.
	if p.dirty {
		title += t("tui.unsaved")
	}
	hint := styleFor("popup-dim", palette).Render(p.hintLine())
	// An unconstrained Huh Group may carry the trailing blanks of an initialized viewport. Trim them before adding the hint,
	// otherwise those blanks become interior whitespace and the popup cannot hug its actual content.
	formView := strings.Join(trimTrailingBlank(strings.Split(p.form.View(), "\n")), "\n")
	return title, formView + footerGap + hint
}

// fitOptionsForm owns the vertical layout of every section: the blank line before the hint is preserved first,
// a shortage drops that blank line next, and only if it still does not fit is the Huh viewport shortened and scrolled.
func fitOptionsForm(natural, available int) (height int, footerGap string) {
	footerHeight := 2
	footerGap = "\n\n"
	if natural+footerHeight > available {
		footerHeight = 1
		footerGap = "\n"
	}
	height = natural
	if height > available-footerHeight {
		height = available - footerHeight
	}
	if height < 1 {
		height = 1
	}
	return height, footerGap
}

// hintLine is the key hint at the bottom of the popup, giving one accurate line for the current page.
func (p *optionsPanel) hintLine() string {
	switch {
	case p.current == sectionDoctor:
		return t("tui.choose_enter_confirm_esc_skip_installation")
	case p.confirming:
		return t("tui.move_enter_confirm_esc_keep_editing")
	case p.current == "":
		return t("tui.move_enter_open_esc_close")
	case p.current == sectionExecution || p.current == sectionReview:
		return t("tui.field_change_type_model_ids_enter_save_esc_back")
	}
	return t("tui.field_change_enter_save_esc_back")
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
	if delta := mouseWheelDelta(bstate); delta != 0 {
		if delta > 0 {
			return keyCmd(tea.KeyDown)
		}
		return keyCmd(tea.KeyUp)
	}
	if !mouseLeftClicked(bstate) {
		return nil
	}
	if x < p.bodyX || x >= p.bodyX+p.bodyWidth || y < p.bodyY || y >= p.bodyY+p.bodyHeight {
		return nil
	}
	target := y - p.bodyY
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
