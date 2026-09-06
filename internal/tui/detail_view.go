package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/glamour"
	glamansi "github.com/charmbracelet/glamour/ansi"
	glamstyles "github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// detailRender caches one Markdown render. A task card body is Markdown rendered by Glamour;
// selection and cursor math are all done on the plain text with ANSI stripped.
type detailRender struct {
	key    string
	styled []string
	plain  []string
}

func (a *App) detailRenderWidth() int {
	_, w := a.size()
	width := w - 1
	if width < 20 {
		width = 20
	}
	return width
}

// renderedDetail returns the styled body lines, re-rendering when needed.
func (a *App) renderedDetail() []string {
	doc := ""
	if a.Detail != nil {
		doc = a.Detail.Document
	}
	width := a.detailRenderWidth()
	theme := resolveTheme(a.Theme)
	key := theme + "\x00" + itoa(width) + "\x00" + doc
	if a.detailCache.key == key {
		return a.detailCache.styled
	}
	styled := renderMarkdown(doc, width, theme)
	plain := make([]string, len(styled))
	for i, line := range styled {
		plain[i] = ansi.Strip(line)
	}
	a.detailCache = detailRender{key: key, styled: styled, plain: plain}
	return styled
}

// detailLines are the plain-text body lines used by search, the cursor and selection copying.
func (a *App) detailLines() []string {
	a.renderedDetail()
	return a.detailCache.plain
}

func renderMarkdown(doc string, width int, theme string) []string {
	if strings.TrimSpace(doc) == "" {
		return []string{""}
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(markdownCanvasStyle(theme)),
		glamour.WithWordWrap(width),
		glamour.WithEmoji(),
	)
	if err != nil {
		return wrapText(doc, width)
	}
	out, err := renderer.Render(doc)
	if err != nil {
		return wrapText(doc, width)
	}
	lines := strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
	for len(lines) > 1 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

// markdownCanvasStyle adds the same document background as the board to the standard Glamour light/dark styles,
// so the detail body does not punch a patch of the raw terminal color through the screen background.
func markdownCanvasStyle(theme string) glamansi.StyleConfig {
	name := resolveTheme(theme)
	src, ok := glamstyles.DefaultStyles[name]
	if !ok || src == nil {
		src = glamstyles.DefaultStyles[glamstyles.DarkStyle]
	}
	style := *src
	bg := string(themePalette(name).Bg)
	style.Document.BackgroundColor = &bg
	return style
}

// detailBody assembles the visible body: lines hit by search, selection or the cursor switch to plain text plus styling,
// while the remaining lines keep Glamour's original colors.
func (a *App) detailBody(p palette) string {
	styled := a.renderedDetail()
	plain := a.detailLines()
	selectStyle := styleFor("select", p)
	matchStyle := styleFor("match", p)
	caretStyle := styleFor("caret", p)
	out := make([]string, len(styled))
	for i := range styled {
		line := styled[i]
		text := plain[i]
		var spans [][2]int
		switch {
		case a.detailSelectionActive() && a.DetailAnchor != nil:
			spans = selectionSpansForLine(i, text, *a.DetailAnchor, a.DetailCursor, a.DetailSelectMode == "line", false)
		case a.MouseSelecting && a.MouseAnchor != nil && a.MouseCursor != nil && a.MouseAnchor.Kind == "detail":
			spans = selectionSpansForLine(i, text, [2]int{a.MouseAnchor.Line, a.MouseAnchor.Col}, [2]int{a.MouseCursor.Line, a.MouseCursor.Col}, false, true)
		}
		if len(spans) > 0 {
			line = renderSpans(text, spans, selectStyle)
		} else if a.DetailQuery != "" && len(matchSpans(text, a.DetailQuery)) > 0 {
			line = matchStyle.Render(text)
		}
		if !a.DetailSearching && i == a.DetailCursor[0] {
			line = renderCaret(text, a.DetailCursor[1], caretStyle, line, len(spans) > 0)
		}
		out[i] = line
	}
	return strings.Join(out, "\n")
}

func renderSpans(text string, spans [][2]int, style lipgloss.Style) string {
	if len(spans) == 0 {
		return text
	}
	span := spans[0]
	n := runeCount(text)
	start, end := span[0], span[1]
	if start < 0 {
		start = 0
	}
	if end > n {
		end = n
	}
	if start >= end {
		return text
	}
	return sliceRunes(text, 0, start) + style.Render(sliceRunes(text, start, end)) + sliceRunes(text, end, n)
}

// renderCaret draws a block cursor on a plain-text line. It is not added on top of an already inverted selection.
func renderCaret(text string, col int, style lipgloss.Style, fallback string, selected bool) string {
	if selected {
		return fallback
	}
	n := runeCount(text)
	if col < 0 {
		col = 0
	}
	if col > n {
		col = n
	}
	if col == n {
		return text + style.Render(" ")
	}
	return sliceRunes(text, 0, col) + style.Render(sliceRunes(text, col, col+1)) + sliceRunes(text, col+1, n)
}

// renderDetailView is the whole task card detail screen: header, blank line, one rounded panel styled like a board column
// (title embedded in the top border, holding the metadata, a separator and the Glamour body), and the bottom status bar.
func (a *App) renderDetailView() string {
	h, w := a.size()
	p := themePalette(a.Theme)
	if h < minBoardHeight || w < 20 {
		return padBlock(styleFor("bold", p).Render(a.Context.TooSmall), w, h, p)
	}
	task := Task{}
	if a.Detail != nil {
		task = *a.Detail
	}
	title := task.Title
	if title == "" {
		title = task.TaskID
	}
	state := task.State
	inner := w - 2
	if inner < 1 {
		inner = 1
	}
	contentWidth := w - panelDetailChrome
	if contentWidth < 1 {
		contentWidth = 1
	}

	meta := joinNonEmpty(task.TaskID, a.Context.stateLabel(task.State), a.Context.sizeLabel(task.Kind), task.Type, orUnassigned(task.Assignee, a.Context.Unassigned))
	metaRow := a.panelRow(p, state, " "+styleFor("dim", p).Render(padLine(clipText(meta, contentWidth), contentWidth))+" ", w, true)

	var ruleRow string
	if a.DetailSearching {
		prefix := a.Context.Search + ": "
		ruleRow = a.panelRow(p, state, " "+styleFor("search", p).Render(padLine(prefix+a.DetailQuery, contentWidth))+" ", w, true)
		a.CursorY, a.CursorX = detailRuleRow, 2+displayWidth(prefix+a.DetailQuery)
	} else {
		ruleRow = a.panelRow(p, state, styleFor("separator", p).Render(strings.Repeat("─", inner)), w, true)
	}

	bodyHeight := a.detailBodyHeight()
	a.detailView.Width, a.detailView.Height = contentWidth, bodyHeight
	a.detailView.SetContent(a.detailBody(p))
	a.detailView.SetYOffset(a.DetailScroll)
	bodyLines := strings.Split(padBlock(a.detailView.View(), contentWidth, bodyHeight, p), "\n")
	rows := make([]string, 0, bodyHeight+3)
	rows = append(rows,
		a.panelTop(p, state, clipText(title, max(1, contentWidth-6)), "", w, true, false, false),
		metaRow,
		ruleRow,
	)
	for _, line := range bodyLines {
		rows = append(rows, a.panelRow(p, state, " "+line+" ", w, true))
	}
	rows = append(rows, a.panelBottom(p, state, w, true))

	return lipgloss.JoinVertical(lipgloss.Left,
		a.renderHeader(p, w),
		p.fillLine(w),
		strings.Join(rows, "\n"),
		a.renderDetailStatusBar(p, w, task),
	)
}

// renderDetailStatusBar is the status bar of the detail screen: the task identity on the left, the back hint on the right.
func (a *App) renderDetailStatusBar(p palette, w int, task Task) string {
	if notice := a.transientNotice(); notice != "" {
		return styleFor("footer", p).Render(padLine(" "+clipText(notice, max(0, w-1)), w))
	}
	segments := []string{"KANDER"}
	if task.TaskID != "" {
		segments = append(segments, task.TaskID)
	}
	if label := a.Context.stateLabel(task.State); task.State != "" {
		segments = append(segments, label)
	}
	left := " " + strings.Join(segments, "  "+a.Glyphs["vbar"]+"  ")
	right := a.Context.DetailStatusHelp + " "
	gap := w - displayWidth(left) - displayWidth(right)
	if gap < 1 {
		return styleFor("footer", p).Render(padLine(clipText(left, w), w))
	}
	return styleFor("footer", p).Render(padLine(left+strings.Repeat(" ", gap)+right, w))
}

func newDetailViewport() viewport.Model {
	return viewport.New(1, 1)
}
