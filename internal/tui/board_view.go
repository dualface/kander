package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// boardLayout is the geometry of one state panel in a render. Rendering, paging,
// and mouse hit testing all consume this same two-dimensional layout.
type boardLayout struct {
	State        string
	X, Y         int
	Width        int
	Height       int
	BodyHeight   int
	VisualColumn int
	HasSeparator bool
	SkipTop      bool
	FirstVisual  bool
	LastVisual   bool
}

// Rounded border characters of a column panel.
const (
	borderTopLeft     = "╭"
	borderTopRight    = "╮"
	borderBottomLeft  = "╰"
	borderBottomRight = "╯"
	borderHorizontal  = "─"
	borderVertical    = "│"
)

// panelChrome is the total number of columns a column panel takes on both sides: 1 border and 1 padding column per side.
const panelChrome = 4

// panelDetailChrome equals panelChrome; the detail panel has to subtract it too when it spans the full screen width.
const panelDetailChrome = 4

func (a *App) columnStripVisible() bool {
	h, w := a.size()
	return w > 0 && w <= columnStripMaxWidth && h >= minBoardHeight
}

func (a *App) boardBodyHeight() int {
	h, _ := a.size()
	n := h - bodyTop - 2
	if n < 1 {
		return 1
	}
	return n
}

// renderBoardView assembles the board with Lip Gloss: header, blank line, the column panels, and the bottom status bar.
func (a *App) renderBoardView() string {
	h, w := a.size()
	p := themePalette(a.Theme)
	header := a.renderHeader(p, w)
	if h < minBoardHeight || w < 1 {
		body := styleFor("bold", p).Render(padLine(a.Context.TooSmall, w))
		return lipgloss.JoinVertical(lipgloss.Left,
			header,
			padBlock(body, w, h-2, p),
			styleFor("footer", p).Render(padLine(" "+a.Context.QuitHelp, w)),
		)
	}
	bodyHeight := a.boardBodyHeight()
	layout := a.visibleColumnLayout()
	tabs := a.columnStripVisible()
	columnHeight := bodyHeight + 1
	if !tabs {
		columnHeight++
	}

	blocks := make([]string, 0, len(layout)*2)
	for start := 0; start < len(layout); {
		end := start + 1
		for end < len(layout) && layout[end].VisualColumn == layout[start].VisualColumn {
			end++
		}
		panels := make([]string, 0, end-start)
		for _, panel := range layout[start:end] {
			panels = append(panels, a.renderColumnPanel(p, panel, len(layout)))
		}
		blocks = append(blocks, lipgloss.JoinVertical(lipgloss.Left, panels...))
		if layout[start].HasSeparator {
			blocks = append(blocks, p.fillColumn(1, columnHeight))
		}
		start = end
	}
	board := lipgloss.JoinHorizontal(lipgloss.Top, blocks...)
	parts := []string{
		header,
		// Leave one blank line between the header and the column panels so they are not cramped together.
		p.fillLine(w),
	}
	if tabs {
		parts = append(parts, a.renderColumnTabs(p, w))
	}
	parts = append(parts, padBlock(board, w, columnHeight, p), a.renderStatusBar(p, w, len(layout)))
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderHeader is the single top line: title and search on the left, column count and update time on the right.
func (a *App) renderHeader(p palette, w int) string {
	// The search box uses a text label rather than a magnifier emoji: emoji display widths differ across terminals
	// and would skew the whole line, which is laid out by columns.
	left := " " + a.Context.Title + "   " + a.Context.Search + ": " + a.Model.Query
	if a.Searching {
		left += "▏"
	}
	stamp := compactStamp(a.Model.GeneratedAt)
	right := a.Context.Columns + " " + itoa(a.Columns) + "   " + a.Context.Updated + " " + stamp + " "
	leftWidth := displayWidth(left)
	rightWidth := displayWidth(right)
	if leftWidth+rightWidth > w {
		right = clipText(right, max(0, w-leftWidth))
		rightWidth = displayWidth(right)
	}
	gap := w - leftWidth - rightWidth
	if gap < 0 {
		gap = 0
	}
	return styleFor("title", p).Render(padLine(clipText(left, w)+strings.Repeat(" ", gap)+right, w))
}

// compactStamp keeps only the time part; the header need not repeat today's date.
func compactStamp(value string) string {
	if value == "" {
		return "-"
	}
	if _, clock, ok := strings.Cut(value, " "); ok {
		return clock
	}
	return value
}

// renderStatusBar is the bottom status bar: segmented information on the left, the two most used keys on the right.
// The full key table belongs to the ? help overlay, so the status bar no longer carries a long truncated hint.
func (a *App) renderStatusBar(p palette, w, visible int) string {
	a.statusHits = nil
	if notice := a.transientNotice(); notice != "" {
		return styleFor("footer", p).Render(padLine(" "+clipText(notice, max(0, w-1)), w))
	}
	segments := []string{
		"KANDER",
		itoa(visible) + "/" + itoa(len(a.Model.States())) + " " + a.Context.ColumnUnit,
		itoa(a.visibleTaskCount()) + " " + a.Context.CardUnit,
	}
	left := " " + strings.Join(segments, "  "+a.Glyphs["vbar"]+"  ")
	options, help := a.Context.StatusOptions, a.Context.StatusHelp
	right := options + " | " + help + " "
	gap := w - displayWidth(left) - displayWidth(right)
	if gap < 1 {
		left = clipText(left, max(0, w-displayWidth(right)-1))
		gap = max(0, w-displayWidth(left)-displayWidth(right))
	}
	if displayWidth(right) > w {
		return styleFor("footer", p).Render(padLine(clipText(left, w), w))
	}
	x := displayWidth(left) + gap
	a.statusHits = []statusActionHit{
		{x: x, width: displayWidth(options), options: true},
		{x: x + displayWidth(options) + 3, width: displayWidth(help)},
	}
	return styleFor("footer", p).Render(padLine(left+strings.Repeat(" ", gap)+right, w))
}

// transientNotice covers the whole status bar while showing temporary messages such as copy results and errors.
func (a *App) transientNotice() string {
	if a.Now().Before(a.CopyNoticeUntil) && a.CopyNotice != "" {
		return a.displayNotice()
	}
	if status := a.statusError(); status != "" {
		return a.Context.Error + ": " + status
	}
	if a.Searching {
		return a.Context.SearchHelp
	}
	return ""
}

func (a *App) visibleTaskCount() int {
	total := 0
	for _, state := range a.Model.States() {
		total += len(a.Model.TasksFor(state))
	}
	return total
}

// renderColumnPanel draws one column as a rounded panel with a title.
func (a *App) renderColumnPanel(p palette, col boardLayout, visibleStates int) string {
	width := col.Width
	focused := col.State == a.Model.CurrentState()
	tasks, scroll, capacity := columnTaskWindow(a.Model, col.State, col.BodyHeight)

	label := a.Context.stateLabel(col.State)
	single := a.Model.Single || (col.FirstVisual && col.LastVisual && len(a.Model.States()) > 1)
	moreLeft := a.Model.ColumnOffset > 0
	moreRight := a.Model.ColumnOffset+visibleStates < len(a.Model.States())
	showLeft := (single || col.FirstVisual) && moreLeft
	showRight := (single || col.LastVisual) && moreRight
	if single && len(a.Model.States()) > 1 {
		showLeft, showRight = true, true
	}

	var lines []string
	if !col.SkipTop {
		lines = append(lines, a.panelTop(p, col.State, label, itoa(len(tasks)), width, focused, showLeft, showRight))
	}
	contentWidth := width - panelChrome
	if contentWidth < 1 {
		contentWidth = 1
	}
	body := make([]string, 0, col.BodyHeight)
	if len(tasks) == 0 {
		if col.BodyHeight > 1 {
			body = append(body, "")
		}
		body = append(body, styleFor("dim", p).Render(centerText(a.Context.Empty, width-2)))
	} else {
		end := scroll + capacity
		if end > len(tasks) {
			end = len(tasks)
		}
		for _, task := range tasks[scroll:end] {
			body = append(body, a.renderCard(p, col.State, task, contentWidth, focused)...)
			body = append(body, "")
		}
	}
	for i := 0; i < col.BodyHeight; i++ {
		content := ""
		if i < len(body) {
			content = body[i]
		}
		lines = append(lines, a.panelRow(p, col.State, content, width, focused))
	}
	lines = append(lines, a.panelBottom(p, col.State, width, focused))
	return strings.Join(lines, "\n")
}

type columnTabCell struct {
	state    string
	x, width int
	text     string
	selected bool
}

func columnTabSegment(label string, selected bool, index int, prevSelected bool, count int) string {
	text := label
	// Selected tabs always keep the count (including zero). Idle tabs only
	// show it when the column has cards, so empty states stay compact.
	if selected || count > 0 {
		text = label + " " + itoa(count)
	}
	if selected {
		return borderVertical + text + borderVertical
	}
	if index > 0 && !prevSelected {
		return borderVertical + text
	}
	return text
}

func measureTabLabels(states, labels []string, current string, counts []int) int {
	width := 0
	prevSelected := false
	for i, state := range states {
		selected := state == current
		width += displayWidth(columnTabSegment(labels[i], selected, i, prevSelected, counts[i]))
		prevSelected = selected
	}
	return width
}

func longestIdleLabel(states, labels []string, current string) int {
	best, bestWidth := -1, 0
	for i, state := range states {
		if state == current {
			continue
		}
		// CJK single-rune names still have display width 2, but trimLastRune
		// cannot shorten them. Skip any label that would not actually shrink.
		if displayWidth(trimLastRune(labels[i])) >= displayWidth(labels[i]) {
			continue
		}
		if w := displayWidth(labels[i]); w > bestWidth {
			best, bestWidth = i, w
		}
	}
	return best
}

func trimLastRune(value string) string {
	runes := []rune(value)
	if len(runes) <= 1 {
		return value
	}
	return string(runes[:len(runes)-1])
}

func (a *App) columnTabCells(width int) []columnTabCell {
	if width < 1 {
		return nil
	}
	states := a.Model.States()
	if len(states) == 0 {
		return nil
	}
	current := a.Model.CurrentState()
	labels := make([]string, len(states))
	counts := make([]int, len(states))
	for i, state := range states {
		labels[i] = a.Context.stateLabel(state)
		counts[i] = len(a.Model.TasksFor(state))
	}
	for measureTabLabels(states, labels, current, counts) > width {
		idle := longestIdleLabel(states, labels, current)
		if idle < 0 {
			break
		}
		labels[idle] = trimLastRune(labels[idle])
	}
	cells := make([]columnTabCell, 0, len(states))
	x := 0
	prevSelected := false
	for i, state := range states {
		selected := state == current
		text := columnTabSegment(labels[i], selected, i, prevSelected, counts[i])
		w := displayWidth(text)
		if x+w > width {
			remain := width - x
			if remain < 1 {
				break
			}
			text = clipText(text, remain)
			w = displayWidth(text)
			if w < 1 {
				break
			}
		}
		cells = append(cells, columnTabCell{state: state, x: x, width: w, text: text, selected: selected})
		x += w
		prevSelected = selected
		if x >= width {
			break
		}
	}
	return cells
}

func (a *App) renderColumnTabs(p palette, width int) string {
	cells := a.columnTabCells(width)
	parts := make([]string, 0, len(cells))
	for _, cell := range cells {
		if cell.selected {
			parts = append(parts, columnStripStyle(p, cell.state, true).Render(cell.text))
			continue
		}
		parts = append(parts, styleFor("dim", p).Render(cell.text))
	}
	if len(parts) == 0 {
		return p.fillLine(width)
	}
	return padLineFill(strings.Join(parts, ""), width, p)
}

// renderCard draws one task card. The card keeps one column of padding on each side and both take part in the coloring,
// so a selected card is one solid block spanning the panel's inner width, with no extra vertical bar needed.
func (a *App) renderCard(p palette, state string, task Task, contentWidth int, focused bool) []string {
	selected := focused && task.TaskID == a.Model.SelectedIDs[state]
	card := a.boardCardLines(task, contentWidth)
	out := make([]string, 0, len(card))
	for offset, text := range card {
		style := cardStyle(p, state, offset, selected)
		var spans [][2]int
		if a.MouseSelecting && a.MouseAnchor != nil && a.MouseCursor != nil &&
			a.MouseAnchor.Kind == "board" && a.MouseAnchor.TaskID == task.TaskID {
			spans = selectionSpansForLine(offset, text, [2]int{a.MouseAnchor.Line, a.MouseAnchor.Col}, [2]int{a.MouseCursor.Line, a.MouseCursor.Col}, false, true)
		}
		if len(spans) > 0 {
			style = styleFor("select", p)
		}
		out = append(out, style.Render(" "+padLine(clipText(text, contentWidth), contentWidth)+" "))
	}
	return out
}

// panelTop draws the panel top border with the title and the count badge embedded in it: ╭─ Todo 3 ────╮
// When more columns exist to either side, arrows at the ends of the border hint at them without taking the title's place.
func (a *App) panelTop(p palette, state, label, badge string, width int, focused, moreLeft, moreRight bool) string {
	border := panelBorderStyle(p, state, focused)
	if width < 6 {
		return border.Render(strings.Repeat(borderHorizontal, max(0, width)))
	}
	head := borderTopLeft + borderHorizontal
	if moreLeft {
		head = borderTopLeft + a.Glyphs["left"]
	}
	tail := borderHorizontal + borderTopRight
	if moreRight {
		tail = a.Glyphs["right"] + borderTopRight
	}
	// With a badge the title keeps no trailing space; the badge brings one space on each side itself, forming a complete little block.
	title := " " + label + " "
	chip := ""
	if badge != "" {
		title = " " + label
		chip = " " + badge + " "
	}
	used := displayWidth(title) + displayWidth(chip)
	fill := width - displayWidth(head) - displayWidth(tail) - used
	if fill < 0 {
		title = " " + clipText(label, max(1, width-8))
		if chip == "" {
			title += " "
		}
		used = displayWidth(title) + displayWidth(chip)
		fill = max(0, width-displayWidth(head)-displayWidth(tail)-used)
	}
	rendered := headingStyle(p, state, focused).Render(title)
	if chip != "" {
		rendered += badgeStyle(p, state, focused).Render(chip)
	}
	return border.Render(head) + rendered +
		border.Render(strings.Repeat(borderHorizontal, fill)) +
		border.Render(tail)
}

func (a *App) panelBottom(p palette, state string, width int, focused bool) string {
	border := panelBorderStyle(p, state, focused)
	if width < 2 {
		return border.Render(strings.Repeat(borderHorizontal, max(0, width)))
	}
	return border.Render(borderBottomLeft + strings.Repeat(borderHorizontal, width-2) + borderBottomRight)
}

// panelRow wraps one line of content in the left and right panel borders.
func (a *App) panelRow(p palette, state, content string, width int, focused bool) string {
	border := panelBorderStyle(p, state, focused)
	inner := width - 2
	if inner < 0 {
		inner = 0
	}
	return border.Render(borderVertical) + padLineFill(content, inner, p) + border.Render(borderVertical)
}

func centerText(text string, width int) string {
	pad := (width - displayWidth(text)) / 2
	if pad < 0 {
		pad = 0
	}
	return padLine(strings.Repeat(" ", pad)+text, width)
}
