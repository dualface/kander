package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var detectDarkBackground = lipgloss.HasDarkBackground

// palette is one resolved theme. Light and dark both fill the whole screen background
// instead of leaving it to the terminal, so a light theme is not reduced to black text in a dark terminal.
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
	// The separator is only a helper line between columns, so it takes a shade closer to the background than Dim (ANSI 8, mid grey):
	// 240 on a dark background and 250 on a light one, both clearly below body contrast yet still visible.
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

// paintScreen lays the content on a canvas of fixed width and height, with line ends and blank lines carrying the theme background too.
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

// styleFor maps a screenBuffer cell tag to a lipgloss style.
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

// headingStyle is the style of a column title. The selected column is inverted,
// so which column has focus is obvious at a glance even when no card is selected.
func headingStyle(p palette, state string, focused bool) lipgloss.Style {
	style := styleFor("heading-"+state, p)
	if focused {
		return style.Reverse(true)
	}
	return style
}

// headingRuleStyle is the rule below the title. The selected column uses its column color as a focus hint,
// while the others take the same low-contrast separator as the vertical dividers and do not compete with the content.
func headingRuleStyle(p palette, state string, focused bool) lipgloss.Style {
	if focused {
		return styleFor("heading-"+state, p)
	}
	return styleFor("separator", p)
}

// stateColor is the theme color of one column, falling back to the base foreground for an unknown column.
func stateColor(p palette, state string) lipgloss.Color {
	if color, ok := p.Headings[state]; ok {
		return color
	}
	return p.Base
}

// cardStyle is the style of one line of a task card. Line 0 is the title and takes the color of its column,
// staying in the same family as the column title; the remaining lines are the task ID and metadata and keep the base color.
// A selected card is inverted in its column color as one block, so the selection and its column match up at a glance.
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

// panelBorderStyle is the border of a column panel: the selected column outlines focus in its column color,
// while the others take the low-contrast separator and do not compete with the card content.
func panelBorderStyle(p palette, state string, focused bool) lipgloss.Style {
	if focused {
		return p.ink(stateColor(p, state))
	}
	return styleFor("separator", p)
}

// badgeStyle is the task count badge after a column title. It is an inverted little block rather than a bare number:
// the selected column uses its column color and joins the equally inverted title, while the others go dim and only hint.
func badgeStyle(p palette, state string, focused bool) lipgloss.Style {
	if focused {
		return p.ink(stateColor(p, state)).Reverse(true).Bold(true)
	}
	return p.ink(p.Dim).Reverse(true)
}

// resolveTheme resolves auto into light or dark; an explicit theme ignores the terminal probe.
// Bubble Tea already had Lip Gloss probe and cache the background type before startup, so the result is simply reused here.
func resolveTheme(name string) string {
	if name == "light" || name == "dark" {
		return name
	}
	if detectDarkBackground() {
		return "dark"
	}
	return "light"
}
