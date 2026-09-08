package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// helpEntry is one line of the help overlay: the key on the left, its description on the right.
type helpEntry struct {
	Keys string
	Desc string
}

// helpGroup is one group of key descriptions.
type helpGroup struct {
	Title   string
	Entries []helpEntry
}

func boardHelpGroups() []helpGroup {
	return []helpGroup{
		{
			Title: t("tui.board"),
			Entries: []helpEntry{
				{"←→ hl", t("tui.switch_column")},
				{"↑↓ jk", t("tui.switch_task")},
				{"PgUp PgDn", t("tui.page")},
				{"Enter", t("tui.task_detail")},
				{"/", t("tui.search_2")},
				{"y", t("tui.copy_task_id")},
				{"g", t("tui.focus_agent_window")},
				{"s", t("tui.start_task")},
				{"- =", t("tui.columns_on_screen")},
				{"a", t("tui.archived_columns")},
				{"t", t("tui.cycle_theme")},
				{"o", t("tui.options")},
				{"r", t("tui.refresh_now")},
				{"? q", t("tui.help_quit")},
			},
		},
		{
			Title: t("tui.task_detail_2"),
			Entries: []helpEntry{
				{"hjkl ←→↑↓", t("tui.move_cursor")},
				{"Ctrl-d Ctrl-u", t("tui.half_page")},
				{"Ctrl-f Ctrl-b", t("tui.full_page")},
				{"gg G", t("tui.top_bottom")},
				{"/ n N", t("tui.search_and_jump")},
				{"v V", t("tui.char_line_select")},
				{"y", t("tui.copy_selection")},
				{"q Esc", t("tui.back_to_board")},
			},
		},
		{
			Title: t("tui.mouse"),
			Entries: []helpEntry{
				{t("tui.click"), t("tui.focus_column_or_task")},
				{t("tui.double_click"), t("tui.open_task_detail")},
				{t("tui.drag"), t("tui.copy_text")},
				{t("tui.wheel"), t("tui.cards_document")},
			},
		},
	}
}

// renderHelp draws the help overlay. The keys are laid out in two columns: the board on the left, the detail view and the mouse on the right;
// on a narrow terminal it falls back to a single vertical column so nothing is truncated by the popup width.
func (a *App) renderHelp() (popupBox, string) {
	h, w := a.size()
	p := themePalette(a.Theme)
	groups := boardHelpGroups()

	left := renderHelpGroup(p, groups[0])
	right := renderHelpGroup(p, groups[1])
	if len(groups) > 2 {
		right = joinBlocksVertical(p, right, renderHelpGroup(p, groups[2]))
	}
	height := blockHeight(left)
	if got := blockHeight(right); got > height {
		height = got
	}
	// Both sides are padded to their own rectangle first: lipgloss would otherwise pad the shorter
	// lines and the missing rows with bare spaces, which leaves the terminal background showing through.
	left = padBlock(left, blockWidth(left), height, p)
	right = padBlock(right, blockWidth(right), height, p)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, p.fillColumn(3, height), right)

	available := w - 8
	if available > 108 {
		available = 108
	}
	if blockWidth(body) > available-4 {
		blocks := make([]string, 0, len(groups))
		for _, group := range groups {
			blocks = append(blocks, renderHelpGroup(p, group))
		}
		body = joinBlocksVertical(p, blocks...)
	}

	frame := popup{Title: t("tui.key_bindings"), Hint: t("tui.press_any_key_to_close"), MaxWidth: available}
	inner := blockWidth(body)
	if width := displayWidth(frame.Hint); width > inner {
		inner = width
	}
	if width := displayWidth(frame.Title); width > inner {
		inner = width
	}
	box, _, out := frame.render(p, w, h, inner, body)
	return box, out
}

// joinBlocksVertical stacks the blocks with one blank line between them, every line padded to the
// widest one so the filler carries the theme background instead of lipgloss' bare spaces.
func joinBlocksVertical(p palette, blocks ...string) string {
	width := 0
	for _, block := range blocks {
		if got := blockWidth(block); got > width {
			width = got
		}
	}
	lines := make([]string, 0, len(blocks))
	for i, block := range blocks {
		if i > 0 {
			lines = append(lines, p.fillLine(width))
		}
		lines = append(lines, padBlock(block, width, blockHeight(block), p))
	}
	return strings.Join(lines, "\n")
}

func blockWidth(block string) int {
	width := 0
	for _, line := range strings.Split(block, "\n") {
		if got := ansi.StringWidth(line); got > width {
			width = got
		}
	}
	return width
}

func blockHeight(block string) int {
	return len(strings.Split(block, "\n"))
}

func renderHelpGroup(p palette, group helpGroup) string {
	keyWidth := 0
	for _, entry := range group.Entries {
		if width := displayWidth(entry.Keys); width > keyWidth {
			keyWidth = width
		}
	}
	lines := []string{styleFor("popup-group", p).Render(group.Title)}
	for _, entry := range group.Entries {
		lines = append(lines,
			styleFor("popup-title", p).Render(padLine(entry.Keys, keyWidth))+
				p.fillLine(2)+styleFor("popup", p).Render(entry.Desc))
	}
	return strings.Join(lines, "\n")
}
