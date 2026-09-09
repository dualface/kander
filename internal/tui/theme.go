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

func themePalette(name string) palette {
	// Hex values keep light and dark on their designed canvases. Indexed 0–15
	// would follow the terminal palette and invert Solarized-style schemes.
	// Each theme carries its own foregrounds: the dark set is bright-on-dark,
	// the light set is dark-on-light, so they no longer share one ANSI family.
	if resolveTheme(name) == "light" {
		return palette{
			Base:      lipgloss.Color("#16181d"),
			Bg:        lipgloss.Color("#fafafa"),
			Dim:       lipgloss.Color("#6b7280"),
			Separator: lipgloss.Color("#828892"),
			Accent:    lipgloss.Color("#9d2ec5"),
			Bar:       lipgloss.Color("#1d4ed8"),
			ChromeFg:  lipgloss.Color("#f7f7fb"),
			ChromeBg:  lipgloss.Color("#6b21a8"),
			PopupFg:   lipgloss.Color("#16181d"),
			PopupEdge: lipgloss.Color("#9d2ec5"),
			Warn:      lipgloss.Color("#c62828"),
			OK:        lipgloss.Color("#2e7d32"),
			Headings: map[string]lipgloss.Color{
				"backlog":  lipgloss.Color("#0e7490"),
				"todo":     lipgloss.Color("#a16207"),
				"working":  lipgloss.Color("#1d4ed8"),
				"review":   lipgloss.Color("#9d2ec5"),
				"done":     lipgloss.Color("#15803d"),
				"archived": lipgloss.Color("#7e22ce"),
				"trash":    lipgloss.Color("#b91c1c"),
			},
		}
	}
	return palette{
		Base:      lipgloss.Color("#e6e8eb"),
		Bg:        lipgloss.Color("#16181d"),
		Dim:       lipgloss.Color("#8b919a"),
		Separator: lipgloss.Color("#6a7078"),
		Accent:    lipgloss.Color("#d670d6"),
		Bar:       lipgloss.Color("#6ea8fe"),
		ChromeFg:  lipgloss.Color("#f7f7fb"),
		ChromeBg:  lipgloss.Color("#6b21a8"),
		PopupFg:   lipgloss.Color("#e6e8eb"),
		PopupEdge: lipgloss.Color("#d670d6"),
		Warn:      lipgloss.Color("#f07178"),
		OK:        lipgloss.Color("#7fd17f"),
		Headings: map[string]lipgloss.Color{
			"backlog":  lipgloss.Color("#4dd0e1"),
			"todo":     lipgloss.Color("#e6c35c"),
			"working":  lipgloss.Color("#6ea8fe"),
			"review":   lipgloss.Color("#d670d6"),
			"done":     lipgloss.Color("#7fd17f"),
			"archived": lipgloss.Color("#c084d0"),
			"trash":    lipgloss.Color("#f07178"),
		},
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
