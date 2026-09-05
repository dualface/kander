package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var detectDarkBackground = lipgloss.HasDarkBackground

// palette 是一套解析后的主题配色. 浅色/深色会铺满整屏背景,
// 不再把底色交给终端, 避免浅色主题在深色终端里只剩黑字。
type palette struct {
	Base      lipgloss.Color
	Bg        lipgloss.Color
	Dim       lipgloss.Color
	Separator lipgloss.Color
	Accent    lipgloss.Color
	Bar       lipgloss.Color
	ChromeFg  lipgloss.Color
	ChromeBg  lipgloss.Color
	PopupFg   lipgloss.Color
	PopupEdge lipgloss.Color
	Warn      lipgloss.Color
	OK        lipgloss.Color
	Headings  map[string]lipgloss.Color
}

var headingColors = map[string]lipgloss.Color{
	"backlog":  lipgloss.Color("6"),
	"todo":     lipgloss.Color("3"),
	"working":  lipgloss.Color("4"),
	"review":   lipgloss.Color("5"),
	"done":     lipgloss.Color("2"),
	"archived": lipgloss.Color("5"),
	"trash":    lipgloss.Color("1"),
}

func themePalette(name string) palette {
	base := lipgloss.Color("15")
	bg := lipgloss.Color("0")
	dim := lipgloss.Color("8")
	// 分隔线只是分栏用的辅助线, 取比 Dim (ANSI 8, 中灰) 更贴近背景的一档:
	// 深色底用 240, 浅色底用 250, 两边都明显低于正文对比度但仍看得见.
	separator := lipgloss.Color("240")
	if resolveTheme(name) == "light" {
		base = lipgloss.Color("0")
		bg = lipgloss.Color("15")
		separator = lipgloss.Color("250")
	}
	return palette{
		Base:      base,
		Bg:        bg,
		Dim:       dim,
		Separator: separator,
		Accent:    lipgloss.Color("13"),
		Bar:       lipgloss.Color("4"),
		ChromeFg:  lipgloss.Color("15"),
		ChromeBg:  lipgloss.Color("5"),
		PopupFg:   base,
		PopupEdge: lipgloss.Color("13"),
		Warn:      lipgloss.Color("1"),
		OK:        lipgloss.Color("2"),
		Headings:  headingColors,
	}
}

func (p palette) ink(color lipgloss.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(color).Background(p.Bg)
}

func (p palette) fillLine(width int) string {
	if width < 1 {
		return ""
	}
	return p.ink(p.Base).Render(strings.Repeat(" ", width))
}

func (p palette) fillColumn(width, height int) string {
	if height < 1 {
		return ""
	}
	line := p.fillLine(width)
	lines := make([]string, height)
	for i := range lines {
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

func (p palette) paint(style lipgloss.Style) lipgloss.Style {
	return style.Background(p.Bg)
}

// paintScreen 把内容铺到固定宽高的画布上, 行尾和空行也带主题背景.
func paintScreen(content string, width, height int, p palette) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	lines := strings.Split(content, "\n")
	out := make([]string, height)
	for i := 0; i < height; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		out[i] = padLineFill(line, width, p)
	}
	return strings.Join(out, "\n")
}

// styleFor 把 screenBuffer 的单元格标签映射为 lipgloss 样式.
func styleFor(tag string, p palette) lipgloss.Style {
	style := p.ink(p.Base)
	switch {
	case tag == "title" || tag == "footer":
		return lipgloss.NewStyle().Foreground(p.ChromeFg).Background(p.ChromeBg).Bold(true)
	case tag == "search":
		return p.ink(p.Accent).Bold(true)
	case tag == "selected" || tag == "select":
		return p.ink(p.Base).Reverse(true).Bold(true)
	case tag == "bar":
		return p.ink(p.Bar).Bold(true)
	case tag == "match" || tag == "caret":
		return p.ink(p.Base).Reverse(true)
	case tag == "dim":
		return p.ink(p.Dim)
	case tag == "separator":
		return p.ink(p.Separator)
	case tag == "bold":
		return style.Bold(true)
	case tag == "popup-title":
		return p.ink(p.PopupEdge).Bold(true)
	case tag == "popup-edge":
		return p.ink(p.PopupEdge)
	case tag == "popup-sel":
		return p.ink(p.PopupFg).Reverse(true).Bold(true)
	case tag == "popup-dim":
		return p.ink(p.Dim)
	case tag == "popup-group":
		return p.ink(p.Accent).Bold(true)
	case tag == "popup-warn":
		return p.ink(p.Warn).Bold(true)
	case tag == "popup-ok":
		return p.ink(p.OK)
	case tag == "popup":
		return p.ink(p.PopupFg)
	case strings.HasPrefix(tag, "heading-"):
		state := strings.TrimPrefix(tag, "heading-")
		if color, ok := p.Headings[state]; ok {
			return p.ink(color).Bold(true)
		}
		return style.Bold(true)
	}
	return style
}

// headingStyle 是栏目标题的样式. 当前选中的栏目反色显示,
// 让「焦点在哪一栏」在没有选中卡片时也一眼可见.
func headingStyle(p palette, state string, focused bool) lipgloss.Style {
	style := styleFor("heading-"+state, p)
	if focused {
		return style.Reverse(true)
	}
	return style
}

// headingRuleStyle 是标题下方的横线. 选中栏用栏目色作焦点提示,
// 其余栏与竖分隔线同样走低对比度的 separator, 不跟内容抢注意力.
func headingRuleStyle(p palette, state string, focused bool) lipgloss.Style {
	if focused {
		return styleFor("heading-"+state, p)
	}
	return styleFor("separator", p)
}

// stateColor 是某个栏目的主题色, 未知栏目退回基础前景色.
func stateColor(p palette, state string) lipgloss.Color {
	if color, ok := p.Headings[state]; ok {
		return color
	}
	return p.Base
}

// cardStyle 是任务卡某一行的样式. 第 0 行是标题, 用所在栏目的颜色,
// 与栏目标题保持同一色系; 其余行是任务 ID 与元信息, 保持基础色.
// 选中的卡整张按栏目色反色, 让选中项与所属栏目一眼对得上.
func cardStyle(p palette, state string, line int, selected bool) lipgloss.Style {
	color := stateColor(p, state)
	if selected {
		style := p.ink(color).Reverse(true)
		if line == 0 {
			return style.Bold(true)
		}
		return style
	}
	if line == 0 {
		return p.ink(color)
	}
	return p.ink(p.Base)
}

// panelBorderStyle 是栏目面板的边框: 选中栏用栏目色勾出焦点,
// 其余栏走低对比度的 separator, 不与卡片内容抢注意力.
func panelBorderStyle(p palette, state string, focused bool) lipgloss.Style {
	if focused {
		return p.ink(stateColor(p, state))
	}
	return styleFor("separator", p)
}

// badgeStyle 是栏目标题后那颗任务数徽标. 做成反色小块而不是裸数字:
// 选中栏用栏目色, 与同样反色的标题连成一块; 其余栏用暗色, 只做提示不抢眼.
func badgeStyle(p palette, state string, focused bool) lipgloss.Style {
	if focused {
		return p.ink(stateColor(p, state)).Reverse(true).Bold(true)
	}
	return p.ink(p.Dim).Reverse(true)
}

// resolveTheme 把 auto 解析为 light 或 dark; 显式主题忽略终端探测结果.
// Bubble Tea 在启动前已让 Lip Gloss 探测并缓存背景类型, 这里直接复用结果.
func resolveTheme(name string) string {
	if name == "light" || name == "dark" {
		return name
	}
	if detectDarkBackground() {
		return "dark"
	}
	return "light"
}
