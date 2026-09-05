package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	popupMaxWidth   = 78
	popupMinWidth   = 34
	popupSideMargin = 2
)

// popupBox 是弹窗在屏幕上的几何位置, 供渲染与鼠标命中共用.
type popupBox struct {
	X, Y          int
	Width, Height int
}

// centerPopup 按内容需要的尺寸在屏幕上居中摆放弹窗.
func centerPopup(screenWidth, screenHeight, wantWidth, wantHeight int) popupBox {
	return centerPopupMax(screenWidth, screenHeight, wantWidth, wantHeight, popupMaxWidth)
}

// centerPopupMax 与 centerPopup 相同, 但可以放宽最大宽度;
// 按键说明这类以表格排布的内容需要比设置面板更宽.
func centerPopupMax(screenWidth, screenHeight, wantWidth, wantHeight, maxWidth int) popupBox {
	width := wantWidth
	if width > maxWidth {
		width = maxWidth
	}
	if width > screenWidth-popupSideMargin*2 {
		width = screenWidth - popupSideMargin*2
	}
	if width < popupMinWidth {
		width = popupMinWidth
	}
	if width > screenWidth {
		width = screenWidth
	}
	height := wantHeight
	if height > screenHeight-2 {
		height = screenHeight - 2
	}
	if height < 5 {
		height = 5
	}
	if height > screenHeight {
		height = screenHeight
	}
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return popupBox{X: x, Y: y, Width: width, Height: height}
}

// popupFrame 是弹窗外框: 圆角边框加左右各一列内边距.
// Lip Gloss 的 Width 含内边距, 因此这里传的是边框内的总宽度.
func popupFrame(p palette, width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.PopupEdge).
		BorderBackground(p.Bg).
		Foreground(p.PopupFg).
		Background(p.Bg).
		Padding(0, 1).
		Width(width)
}

// withDefaultColors 给组件输出中的样式重置补回弹窗默认颜色.
// Lip Gloss 的外层 Background 不会穿过嵌套的 SGR reset: Huh 拼接的
// 候选行补白、箭头间距和按钮边距会因此露出终端底色. 在外框合成后处理,
// 保留组件明确指定的颜色与强调样式, 只改变 reset 后的默认画布.
func withDefaultColors(text string, colors lipgloss.Style) string {
	// 通过渲染器取得前缀, 遵循终端颜色能力和 NO_COLOR, 不硬编码色彩模式.
	prefix, _, _ := strings.Cut(colors.Render(" "), " ")
	if prefix == "" {
		return text
	}
	reset := "\x1b[0m"
	text = strings.NewReplacer(
		reset, reset+prefix,
		ansi.ResetStyle, ansi.ResetStyle+prefix,
	).Replace(text)
	return prefix + text + reset
}

// overlay 把 top 逐行叠加到 base 的 (x, y) 位置.
// 两侧都可能带 ANSI 样式, 因此按显示列而不是字节切片.
func overlay(base, top string, x, y int, p palette) string {
	if top == "" {
		return base
	}
	baseLines := strings.Split(base, "\n")
	topLines := strings.Split(top, "\n")
	for i, line := range topLines {
		row := y + i
		if row < 0 || row >= len(baseLines) {
			continue
		}
		width := ansi.StringWidth(line)
		original := baseLines[row]
		originalWidth := ansi.StringWidth(original)
		left := ""
		if x > 0 {
			left = ansi.Cut(original, 0, x)
			if pad := x - ansi.StringWidth(left); pad > 0 {
				left += p.fillLine(pad)
			}
		}
		right := ""
		if x+width < originalWidth {
			right = ansi.Cut(original, x+width, originalWidth)
		}
		baseLines[row] = left + line + right
	}
	return strings.Join(baseLines, "\n")
}

// padLine 把一行补齐到给定显示宽度; 超宽时按显示列截断.
func padLine(text string, width int) string {
	if width <= 0 {
		return ""
	}
	current := ansi.StringWidth(text)
	if current > width {
		return ansi.Truncate(text, width, "")
	}
	return text + strings.Repeat(" ", width-current)
}

// padLineFill 与 padLine 相同, 但补齐的空格带主题背景, 避免行尾漏出终端底色.
func padLineFill(text string, width int, p palette) string {
	if width <= 0 {
		return ""
	}
	current := ansi.StringWidth(text)
	if current > width {
		return ansi.Truncate(text, width, "")
	}
	if pad := width - current; pad > 0 {
		return text + p.fillLine(pad)
	}
	return text
}
