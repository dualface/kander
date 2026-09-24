package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/cellbuf"
)

type optionsActionHit struct {
	x, width int
	key      tea.KeyType
}

// renderForm uses the same complete Huh content for scrolling and hit testing.
// Huh's own group viewport follows focus on every update, including cursor blinks.
func (p *optionsPanel) renderForm(width, available int) string {
	p.form.WithWidth(p.fieldWidth(width))
	p.formWidth = width
	if p.bind == nil {
		p.form.GetFocusedField().WithHeight(0)
	}
	content := p.formContent()
	p.formNatural = contentHeight(content)
	height, _ := fitOptionsForm(p.formNatural, available)
	p.formView.Width, p.formView.Height = width, height
	p.formView.SetContent(strings.Join(trimTrailingBlank(strings.Split(content, "\n")), "\n"))
	if p.followFocus {
		p.revealFormFocus()
		p.followFocus = false
	}
	p.formView.SetYOffset(p.formView.YOffset)
	return p.formView.View()
}

func (p *optionsPanel) currentBodyLines() []string {
	if p.formGroup == nil {
		return p.bodyLines
	}
	return trimTrailingBlank(strings.Split(p.formContent(), "\n"))
}

func (p *optionsPanel) revealFormFocus() {
	lo, hi, ok := focusRange(p.currentBodyLines())
	if !ok {
		return
	}
	if hi-lo >= p.formView.Height {
		hi = lo
	}
	if lo < p.formView.YOffset {
		p.formView.SetYOffset(lo)
	} else if hi >= p.formView.YOffset+p.formView.Height {
		p.formView.SetYOffset(hi - p.formView.Height + 1)
	}
}

func (p *optionsPanel) renderActions(width int) string {
	p.actionHits = nil
	var labels []string
	add := func(label string, key tea.KeyType) {
		x := displayWidth(strings.Join(labels, "  "))
		if len(labels) > 0 {
			x += 2
		}
		label = "[" + label + "]"
		if x+displayWidth(label) <= width {
			p.actionHits = append(p.actionHits, optionsActionHit{x, displayWidth(label), key})
			labels = append(labels, label)
		}
	}
	// Sections have no mouse actions: an outside click leaves them and Enter still saves.
	if p.confirming {
		add(t("tui.keep_editing"), tea.KeyEsc)
	}
	return styleFor("popup-title", themePalette(p.app.Theme)).Render(strings.Join(labels, "  "))
}

// optionsMouseActivate also accepts release events for the scope-tab adapter.
// HandleMouse validates that the release has not ended a drag first.
func optionsMouseActivate(bstate int) bool {
	return mouseLeftClicked(bstate) || mouseButton1Released(bstate)
}

func (p *optionsPanel) HandleMouse(x, y, bstate int) tea.Cmd {
	click := p.app.popupClick(x, y, bstate)
	if p.confirm != nil {
		p.confirm.handleWheel(x, y, bstate, nil, nil)
		if click {
			return p.clickConfirmation(x, y)
		}
		return nil
	}
	if click && !p.box.contains(x, y) {
		if p.current != "" && !p.confirming && p.report == nil && p.form != nil {
			return p.leaveSection()
		}
		return p.requestClose()
	}
	if click {
		if p.hitTab(x-p.headerX, y-p.headerY) != "" {
			return p.handleTabMouse(x, y, bstate)
		}
	}
	if p.report != nil {
		if delta := mouseWheelDelta(bstate); delta != 0 {
			p.report.view.SetYOffset(p.report.view.YOffset + delta*mouseScrollStep)
		} else if click {
			return p.updateReport(tea.KeyMsg{Type: tea.KeyEsc})
		}
		return nil
	}
	if p.form == nil {
		if click && p.loadErr != "" {
			p.close()
		}
		return nil
	}
	if delta := mouseWheelDelta(bstate); delta != 0 {
		p.followFocus = false
		p.formView.SetYOffset(p.formView.YOffset + delta*mouseScrollStep)
		return nil
	}
	if !click || x < p.bodyX || x >= p.bodyX+p.bodyWidth {
		return nil
	}
	if p.closeHintAt(x, y) {
		return p.requestClose()
	}
	row := y - p.bodyY - p.chromeLines
	if row == p.formView.Height+1 {
		for _, hit := range p.actionHits {
			if x-p.bodyX >= hit.x && x-p.bodyX < hit.x+hit.width {
				return p.Update(tea.KeyMsg{Type: hit.key})
			}
		}
	}
	if row < 0 || row >= p.formView.Height {
		return nil
	}
	row += p.formView.YOffset
	if p.bind == nil {
		return p.clickMenu(row)
	}
	return p.clickField(x-p.bodyX, row)
}

// Only visible close hints are clickable; menu values and clipped text are not.
func (p *optionsPanel) closeHintAt(x, y int) bool {
	if p.current != "" || p.confirming {
		return false
	}
	row := y - p.bodyY
	if row < 0 || row >= len(p.bodyLines) {
		return false
	}
	label := t("tui.options_close_hint")
	if row != len(p.bodyLines)-1 {
		start, _ := p.menuRows()
		formRow := row - p.chromeLines + p.formView.YOffset
		if formRow < 0 || formRow >= start {
			return false
		}
		_, suffix, ok := strings.Cut(p.menuDescription, "Esc")
		if !ok {
			return false
		}
		label = "Esc" + suffix
	}
	line := ansi.Strip(p.bodyLines[row])
	index := strings.Index(line, label)
	if index < 0 {
		return false
	}
	left := p.bodyX + displayWidth(line[:index])
	return x >= left && x < left+displayWidth(label)
}

// menuRows mirrors Huh's wrapping, including continuation rows of long labels.
func (p *optionsPanel) menuRows() (int, []int) {
	styles := p.formTheme.Focused
	width := p.fieldWidth(p.formWidth) - styles.Base.GetHorizontalFrameSize()
	row := blockHeight(cellbuf.Wrap(p.menuDescription, width, ",.-; "))
	heights := make([]int, len(p.menuOptions))
	for i, option := range p.menuOptions {
		heights[i] = blockHeight(cellbuf.Wrap(option.Key, width-displayWidth(styles.SelectSelector.String()), ",.-; "))
	}
	return row, heights
}

func (p *optionsPanel) clickMenu(row int) tea.Cmd {
	start, heights := p.menuRows()
	current, target := -1, -1
	value := p.form.GetFocusedField().GetValue()
	for i, option := range p.menuOptions {
		if option.Value == value {
			current = i
		}
		if row >= start && row < start+heights[i] {
			target = i
		}
		start += heights[i]
	}
	if target < 0 || current < 0 {
		return nil
	}
	var cmds []tea.Cmd
	for current != target {
		key := tea.KeyDown
		if current > target {
			key = tea.KeyUp
			current--
		} else {
			current++
		}
		cmds = append(cmds, p.updateForm(tea.KeyMsg{Type: key}))
	}
	// Dispatch only after selection has changed, without queued synthetic Enter.
	cmds = append(cmds, p.updateForm(tea.KeyMsg{Type: tea.KeyEnter}))
	return tea.Batch(cmds...)
}

func (p *optionsPanel) clickField(x, row int) tea.Cmd {
	start, current, target := 0, -1, -1
	position := 0
	var clicked huh.Field
	var line string
	fieldRow := 0
	for _, field := range p.bind.formFields {
		lines := strings.Split(field.View(), "\n")
		if !field.Skip() {
			if field == p.form.GetFocusedField() {
				current = position
			}
			if row >= start && row < start+len(lines) {
				target, clicked = position, field
				fieldRow = row - start
				line = ansi.Strip(lines[fieldRow])
			}
			position++
		}
		start += len(lines)
	}
	if clicked == nil || current < 0 {
		return nil
	}
	var cmds []tea.Cmd
	for current != target {
		if current < target {
			cmds = append(cmds, p.form.NextField())
			current++
		} else {
			cmds = append(cmds, p.form.PrevField())
			current--
		}
	}
	p.followFocus = true
	switch field := clicked.(type) {
	case *huh.Select[string], *huh.Select[int]:
		if key, ok := selectorClick(line, x); ok {
			cmds = append(cmds, p.updateForm(tea.KeyMsg{Type: key}))
		}
	case *huh.Confirm:
		if value, ok := confirmClick(field, fieldRow, x); ok && value != field.GetValue() {
			cmds = append(cmds, p.updateForm(tea.KeyMsg{Type: tea.KeyRight}))
		}
	}
	return tea.Batch(cmds...)
}

func selectorClick(line string, x int) (tea.KeyType, bool) {
	for _, arrow := range []struct {
		text string
		key  tea.KeyType
	}{{"←", tea.KeyLeft}, {"→", tea.KeyRight}} {
		if i := strings.Index(line, arrow.text); i >= 0 && x == displayWidth(line[:i]) {
			return arrow.key, true
		}
	}
	return 0, false
}

// Huh exposes localized button labels even when keyboard shortcuts are disabled.
// Search from the end of the field so inherited labels in the title cannot
// activate buttons, and wrapped borders do not make a button unclickable.
func confirmClick(field *huh.Confirm, row, x int) (bool, bool) {
	lines := strings.Split(ansi.Strip(field.View()), "\n")
	for _, binding := range field.KeyBinds() {
		help := binding.Help()
		if (help.Key != "y" && help.Key != "n") || help.Desc == "" {
			continue
		}
		for i := len(lines) - 1; i >= 0; i-- {
			index := strings.LastIndex(lines[i], help.Desc)
			if index < 0 {
				continue
			}
			start := displayWidth(lines[i][:index])
			if i == row && x >= max(0, start-2) && x < start+displayWidth(help.Desc)+2 {
				return help.Key == "y", true
			}
			break
		}
	}
	return false, false
}

// Huh includes padding in the field width, but Lip Gloss adds its border outside it.
func (p *optionsPanel) fieldWidth(width int) int {
	return max(1, width-p.formTheme.Focused.Base.GetHorizontalBorderSize())
}

func (p *optionsPanel) formContent() string {
	content := p.formGroup.Content()
	if footer := p.formGroup.Footer(); footer != "" {
		content += "\n\n" + footer
	}
	return content
}
