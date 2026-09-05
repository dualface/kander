package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// boardLayout 是一次渲染中各栏目的几何, 鼠标命中直接复用.
type boardLayout struct {
	State        string
	X            int
	Width        int
	HasSeparator bool
}

// 栏目面板的圆角边框字符.
const (
	borderTopLeft     = "╭"
	borderTopRight    = "╮"
	borderBottomLeft  = "╰"
	borderBottomRight = "╯"
	borderHorizontal  = "─"
	borderVertical    = "│"
)

// panelChrome 是一个栏目面板左右各占的列数: 边框 2 列, 内边距 2 列.
const panelChrome = 4

// panelDetailChrome 与 panelChrome 相同, 详情面板铺满整屏宽度时同样要扣掉它.
const panelDetailChrome = 4

// renderBoardView 用 Lip Gloss 组装看板: 顶栏, 留白, 若干栏目面板, 底部状态栏.
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
	bodyHeight := h - bodyTop - 2
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	layout := a.visibleColumnLayout()
	columnHeight := bodyHeight + 2

	blocks := make([]string, 0, len(layout)*2)
	for i, col := range layout {
		blocks = append(blocks, a.renderColumnPanel(p, col, bodyHeight,
			i == 0, i == len(layout)-1,
			a.Model.ColumnOffset > 0,
			a.Model.ColumnOffset+len(layout) < len(a.Model.States())))
		if col.HasSeparator {
			blocks = append(blocks, p.fillColumn(1, columnHeight))
		}
	}
	board := lipgloss.JoinHorizontal(lipgloss.Top, blocks...)
	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		// 顶栏与栏目面板之间留一行空白, 免得挤在一起.
		p.fillLine(w),
		padBlock(board, w, columnHeight, p),
		a.renderStatusBar(p, w, len(layout)),
	)
}

// renderHeader 是顶部单行: 左边是标题与搜索, 右边是栏目数与更新时间.
func (a *App) renderHeader(p palette, w int) string {
	// 搜索框用文字标签而不是放大镜 emoji: emoji 的显示宽度在各终端不一致,
	// 会把按列计算的整行布局撑歪.
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

// compactStamp 只保留时间部分, 顶栏不需要重复今天的日期.
func compactStamp(value string) string {
	if value == "" {
		return "-"
	}
	if _, clock, ok := strings.Cut(value, " "); ok {
		return clock
	}
	return value
}

// renderStatusBar 是底部状态栏: 左边是分段信息, 右边是最常用的两个按键.
// 完整键位表交给 ? 帮助浮层, 底栏不再塞一长串被截断的提示.
func (a *App) renderStatusBar(p palette, w, visible int) string {
	if notice := a.transientNotice(); notice != "" {
		return styleFor("footer", p).Render(padLine(" "+clipText(notice, max(0, w-1)), w))
	}
	segments := []string{
		"KANDER",
		itoa(visible) + "/" + itoa(len(a.Model.States())) + " " + a.Context.ColumnUnit,
		itoa(a.visibleTaskCount()) + " " + a.Context.CardUnit,
	}
	left := " " + strings.Join(segments, "  "+a.Glyphs["vbar"]+"  ")
	right := a.Context.StatusHelp + " "
	gap := w - displayWidth(left) - displayWidth(right)
	if gap < 1 {
		return styleFor("footer", p).Render(padLine(clipText(left, w), w))
	}
	return styleFor("footer", p).Render(padLine(left+strings.Repeat(" ", gap)+right, w))
}

// transientNotice 是复制结果与错误这类临时消息, 出现时占满整条状态栏.
func (a *App) transientNotice() string {
	if a.Now().Before(a.CopyNoticeUntil) && a.CopyNotice != "" {
		return a.CopyNotice
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

// renderColumnPanel 把一个栏目画成带标题的圆角面板.
func (a *App) renderColumnPanel(p palette, col boardLayout, bodyHeight int, first, last, moreLeft, moreRight bool) string {
	width := col.Width
	focused := col.State == a.Model.CurrentState()
	tasks, scroll, capacity := columnTaskWindow(a.Model, col.State, bodyHeight)

	label := a.Context.stateLabel(col.State)
	single := a.Model.Single || (first && last && len(a.Model.States()) > 1)
	showLeft := (single || first) && moreLeft
	showRight := (single || last) && moreRight
	if single && len(a.Model.States()) > 1 {
		showLeft, showRight = true, true
	}

	lines := []string{a.panelTop(p, col.State, label, itoa(len(tasks)), width, focused, showLeft, showRight)}
	contentWidth := width - panelChrome
	if contentWidth < 1 {
		contentWidth = 1
	}
	body := make([]string, 0, bodyHeight)
	if len(tasks) == 0 {
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
	for i := 0; i < bodyHeight; i++ {
		content := ""
		if i < len(body) {
			content = body[i]
		}
		lines = append(lines, a.panelRow(p, col.State, content, width, focused))
	}
	lines = append(lines, a.panelBottom(p, col.State, width, focused))
	return strings.Join(lines, "\n")
}

// renderCard 画一张任务卡. 卡片左右各留一列内边距, 且这两列一起参与着色,
// 所以选中的卡是一整块铺满面板内宽的色块, 不需要再额外画一根竖条.
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

// panelTop 画面板上边框, 标题与数量徽标嵌在边框里: ╭─ 待办池 3 ────╮
// 左右还有更多栏目时, 用边框两端的箭头提示, 不占标题的位置.
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
	// 有徽标时标题不留右侧空格, 由徽标自己带上左右各一格, 做成完整的小块.
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

// panelRow 把一行内容裹进面板的左右边框.
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
