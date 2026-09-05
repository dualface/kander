package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/internal/version"
)

// innerWidth 是弹窗边框内可写的列数, 渲染与测量必须用同一个值.
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

// startForm 装上新表单: 先 Init 把字段画进视口, 再量自然高度.
// 量早了视口还是空的, 去掉尾随空行会得到 1, 随后 WithHeight(1) 把整页裁掉.
func (p *optionsPanel) startForm(form *huh.Form) tea.Cmd {
	p.form = form
	cmd := form.Init()
	p.measureForm()
	return cmd
}

// measureForm 记下表单的自然高度. Huh 的 WithHeight 会把表单补齐到指定高度,
// 所以必须在设置高度之前量一次, 之后弹窗才能贴着内容收紧.
// NewGroup 会在套主题前按 Charm 默认样式把视口撑高, View() 因此带着底部空白;
// 量高度时去掉这些尾随空行, 否则审核这种字段多的页会留下一大截留白.
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

// view 渲染弹窗, 并记录几何供鼠标命中测试.
// 先按可用空间渲染正文, 再按正文实际高度收紧边框, 避免大片空白.
func (p *optionsPanel) view() (popupBox, string) {
	palette := themePalette(p.app.Theme)
	screenHeight, screenWidth := p.app.size()

	inner := p.innerWidth()
	boxWidth := inner + 4
	// 标题行, 分隔线, 上下边框.
	chrome := 4
	// 内容放不下时允许选项弹窗贴到终端上下边缘，避免规则模块仅因
	// 弹窗外留白而提前进入滚动。
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
	// 正文从上边框, 标题行与分隔线之后开始; 左侧让出边框与内边距各一列.
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

// centerOptionsPopup 在内容能放下时沿用普通弹窗边距；只有高度受限时才占用边距。
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
		// 报告正文与底部操作提示之间固定留一个空行.
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
	// 内容能放下时保留 Huh 的自然高度。即使传入相同高度，WithHeight 也会
	// 把 Group 切换成 viewport 布局，导致本可完整显示的页面参与上下滚动。
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
	// 未保存标记常驻标题, 进了分区也看得见.
	if p.dirty {
		title += t("tui.unsaved")
	}
	hint := styleFor("popup-dim", palette).Render(p.hintLine())
	// 未约束的 Huh Group 可能带初始化视口的尾随空行。先裁掉再加提示，
	// 否则空行会变成正文内部空白，弹窗不能按实际内容收紧。
	formView := strings.Join(trimTrailingBlank(strings.Split(p.form.View(), "\n")), "\n")
	return title, formView + footerGap + hint
}

// fitOptionsForm 统一管理所有分区的垂直布局：优先保留提示前空行，
// 空间不足时先压掉空行，仍放不下才缩短 Huh 视口并滚动。
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

// hintLine 是弹窗底部的按键提示, 按当前页面给出准确的一行.
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

// padBlock 把内容补齐成固定宽高的矩形, 保证弹窗边框不抖动.
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

// 焦点标记: 选中项前缀由本包在 huhTheme 里指定, 焦点字段的左边框由 Huh 画出.
const (
	focusMarker = "▸"
	focusBorder = "┃"
)

// focusRange 从渲染结果里找出当前焦点所在的行区间.
// 有选中项标记时精确到那一行, 否则退回焦点字段的整段左边框.
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

// keyCmd 把一次按键包装成命令交回 Bubble Tea, 由它异步送进 Update.
// 不能就地调用表单返回的命令: 文本输入框的光标闪烁链里有一个 tea.Tick,
// 同步执行会真的 sleep 半秒以上, 表现就是按一下卡一下.
func keyCmd(key tea.KeyType) tea.Cmd {
	return func() tea.Msg { return tea.KeyMsg{Type: key} }
}

// repeatCmd 把同一个命令重复 n 次交给 Bubble Tea.
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

// bodyLines 取当前表单正文的渲染行, 用于把鼠标坐标换算成字段位置.
func (p *optionsPanel) currentBodyLines() []string {
	if p.form == nil {
		return p.bodyLines
	}
	return trimTrailingBlank(strings.Split(p.form.View(), "\n"))
}

// HandleMouse 让弹窗支持点击聚焦, 双击确认与滚轮滚动.
// 所有的焦点移动都折算成命令交回 Bubble Tea, 不在这里同步驱动表单.
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
		// 点已经聚焦的行, 或双击, 等同确认.
		return tea.Batch(move, keyCmd(tea.KeyEnter))
	}
	return move
}

// focusMove 计算把焦点移到目标行所需的命令, 并报告目标是否已经处于焦点.
// 单选页 (根菜单, 关闭确认) 按候选行移动; 设置页按各字段实际渲染出的高度换算,
// 不依赖字段之间有没有空行.
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
