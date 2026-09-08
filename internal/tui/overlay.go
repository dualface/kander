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

// popupBox is the geometry of a popup on screen, shared by rendering and mouse hit testing.
type popupBox struct {
	X, Y          int
	Width, Height int
}

// centerPopup centers a popup on screen at the size its content needs.
// maxWidth of 0 takes the ordinary limit; table-shaped content such as the key reference asks for more.
// tightFit keeps the ordinary vertical margin while the content fits and gives it up only on a height shortage.
func centerPopup(screenWidth, screenHeight, wantWidth, wantHeight, maxWidth int, tightFit bool) popupBox {
	if maxWidth <= 0 {
		maxWidth = popupMaxWidth
	}
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
	if tightFit {
		if want := min(wantHeight, screenHeight); want > height {
			height = want
		}
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

// popup is the shared shape of every overlay: a centered rounded frame holding an optional title
// with a rule under it, the body, and an optional hint line under a blank row. It owns the geometry
// and the painting so a single overlay cannot forget a step — notably withDefaultColors, without
// which the terminal background shows through every space the body leaves unstyled.
type popup struct {
	// Title and Hint are plain text; the popup styles them. Both may be empty and Title may wrap.
	Title    string
	Hint     string
	MaxWidth int
	TightFit bool
}

// chrome is the number of rows the frame takes besides the body.
func (c popup) chrome() int {
	rows := 2
	if c.Title != "" {
		rows += blockHeight(c.Title) + 1
	}
	if c.Hint != "" {
		rows += blockHeight(c.Hint) + 1
	}
	return rows
}

// inner resolves the writable width for a body that would like wantInner columns.
// It is separate from render because a body has to be wrapped and measured before it can be framed.
func (c popup) inner(screenWidth, screenHeight, wantInner int) int {
	box := centerPopup(screenWidth, screenHeight, wantInner+4, 0, c.MaxWidth, c.TightFit)
	return max(1, box.Width-4)
}

// render frames an already styled body. It returns the popup's geometry and the rectangle the body
// occupies on screen, which mouse hit testing needs; both are in absolute screen coordinates.
func (c popup) render(p palette, screenWidth, screenHeight, inner int, body string) (popupBox, popupBox, string) {
	chrome := c.chrome()
	box := centerPopup(screenWidth, screenHeight, inner+4, chrome+blockHeight(body), c.MaxWidth, c.TightFit)
	inner = max(1, box.Width-4)
	bodyHeight := max(1, box.Height-chrome)

	rows := make([]string, 0, 5)
	if c.Title != "" {
		rows = append(rows,
			padBlock(styleFor("popup-title", p).Render(c.Title), inner, blockHeight(c.Title), p),
			styleFor("popup-edge", p).Render(strings.Repeat("─", inner)))
	}
	rows = append(rows, padBlock(body, inner, bodyHeight, p))
	if c.Hint != "" {
		rows = append(rows, p.fillLine(inner),
			padBlock(styleFor("popup-dim", p).Render(c.Hint), inner, blockHeight(c.Hint), p))
	}
	// The body starts after the top border and the title block; on the left it gives up one column
	// each to the border and the frame padding.
	bodyBox := popupBox{X: box.X + 2, Y: box.Y + 1, Width: inner, Height: bodyHeight}
	if c.Title != "" {
		bodyBox.Y += blockHeight(c.Title) + 1
	}
	out := withDefaultColors(popupFrame(p, box.Width-2).Render(strings.Join(rows, "\n")), p.ink(p.Base))
	return box, bodyBox, out
}

// popupFrame is the outer frame of a popup: a rounded border plus one column of padding on each side.
// Lip Gloss counts padding inside Width, so what is passed here is the total width inside the border.
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

// withDefaultColors restores the popup's default colors after style resets in component output.
// An outer Lip Gloss Background does not survive a nested SGR reset: the candidate-line padding, arrow spacing
// and button margins assembled by Huh would let the terminal background show through. It runs after the frame is
// composed, keeping the colors and emphasis the components asked for and only changing the default canvas after a reset.
func withDefaultColors(text string, colors lipgloss.Style) string {
	// Take the prefix from the renderer, honoring terminal color capabilities and NO_COLOR instead of hardcoding a color profile.
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

// overlay composites top onto base line by line at (x, y).
// Both sides may carry ANSI styling, so slicing goes by display column rather than by byte.
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

// padLine pads one line to the given display width; anything wider is truncated by display column.
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

// padLineFill is padLine with the padding spaces carrying the theme background, so no terminal background leaks at the end of a line.
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
