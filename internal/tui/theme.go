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

// themeDef is one named theme. Adding a theme means adding a table row;
// themePalette and the light/dark classifiers only look this table up.
type themeDef struct {
	name    string
	dark    bool
	palette palette
}

var themeTable = []themeDef{
	{name: "light", dark: false, palette: palette{
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
	}},
	{name: "light-warm", dark: false, palette: palette{
		Base:      lipgloss.Color("#3d3428"),
		Bg:        lipgloss.Color("#f3ead8"),
		Dim:       lipgloss.Color("#7a6d5a"),
		Separator: lipgloss.Color("#8a7d6a"),
		Accent:    lipgloss.Color("#8b3d1f"),
		Bar:       lipgloss.Color("#3a5278"),
		ChromeFg:  lipgloss.Color("#f7f3ea"),
		ChromeBg:  lipgloss.Color("#6b4423"),
		PopupFg:   lipgloss.Color("#3d3428"),
		PopupEdge: lipgloss.Color("#8b3d1f"),
		Warn:      lipgloss.Color("#9b2c2c"),
		OK:        lipgloss.Color("#3d6b38"),
		Headings: map[string]lipgloss.Color{
			"backlog":  lipgloss.Color("#0e5f73"),
			"todo":     lipgloss.Color("#8a5500"),
			"working":  lipgloss.Color("#3a5278"),
			"review":   lipgloss.Color("#7a3d8a"),
			"done":     lipgloss.Color("#2f6b38"),
			"archived": lipgloss.Color("#6b3480"),
			"trash":    lipgloss.Color("#9b2c2c"),
		},
	}},
	{name: "light-contrast", dark: false, palette: palette{
		Base:      lipgloss.Color("#000000"),
		Bg:        lipgloss.Color("#ffffff"),
		Dim:       lipgloss.Color("#5c5c5c"),
		Separator: lipgloss.Color("#6e6e6e"),
		Accent:    lipgloss.Color("#5a007a"),
		Bar:       lipgloss.Color("#003399"),
		ChromeFg:  lipgloss.Color("#ffffff"),
		ChromeBg:  lipgloss.Color("#3d0066"),
		PopupFg:   lipgloss.Color("#000000"),
		PopupEdge: lipgloss.Color("#5a007a"),
		Warn:      lipgloss.Color("#8b0000"),
		OK:        lipgloss.Color("#004d00"),
		Headings: map[string]lipgloss.Color{
			"backlog":  lipgloss.Color("#005266"),
			"todo":     lipgloss.Color("#6b4500"),
			"working":  lipgloss.Color("#003399"),
			"review":   lipgloss.Color("#5a007a"),
			"done":     lipgloss.Color("#004d00"),
			"archived": lipgloss.Color("#4a0066"),
			"trash":    lipgloss.Color("#8b0000"),
		},
	}},
	{name: "dark", dark: true, palette: palette{
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
	}},
	{name: "dark-soft", dark: true, palette: palette{
		Base:      lipgloss.Color("#c5ccd8"),
		Bg:        lipgloss.Color("#1c2230"),
		Dim:       lipgloss.Color("#8a929e"),
		Separator: lipgloss.Color("#6e7684"),
		Accent:    lipgloss.Color("#c090c8"),
		Bar:       lipgloss.Color("#7aa3e0"),
		ChromeFg:  lipgloss.Color("#e8eef6"),
		ChromeBg:  lipgloss.Color("#4a3a6a"),
		PopupFg:   lipgloss.Color("#c5ccd8"),
		PopupEdge: lipgloss.Color("#c090c8"),
		Warn:      lipgloss.Color("#e08080"),
		OK:        lipgloss.Color("#80c080"),
		Headings: map[string]lipgloss.Color{
			"backlog":  lipgloss.Color("#6ec8d4"),
			"todo":     lipgloss.Color("#d4b86a"),
			"working":  lipgloss.Color("#7aa3e0"),
			"review":   lipgloss.Color("#c090c8"),
			"done":     lipgloss.Color("#80c080"),
			"archived": lipgloss.Color("#b088c0"),
			"trash":    lipgloss.Color("#e08080"),
		},
	}},
	{name: "dark-contrast", dark: true, palette: palette{
		Base:      lipgloss.Color("#ffffff"),
		Bg:        lipgloss.Color("#000000"),
		Dim:       lipgloss.Color("#a3a3a3"),
		Separator: lipgloss.Color("#8a8a8a"),
		Accent:    lipgloss.Color("#f0a0ff"),
		Bar:       lipgloss.Color("#8cb4ff"),
		ChromeFg:  lipgloss.Color("#ffffff"),
		ChromeBg:  lipgloss.Color("#4a0080"),
		PopupFg:   lipgloss.Color("#ffffff"),
		PopupEdge: lipgloss.Color("#f0a0ff"),
		Warn:      lipgloss.Color("#ff8080"),
		OK:        lipgloss.Color("#66e066"),
		Headings: map[string]lipgloss.Color{
			"backlog":  lipgloss.Color("#5ce1f0"),
			"todo":     lipgloss.Color("#ffd24d"),
			"working":  lipgloss.Color("#8cb4ff"),
			"review":   lipgloss.Color("#f0a0ff"),
			"done":     lipgloss.Color("#66e066"),
			"archived": lipgloss.Color("#e0a0ff"),
			"trash":    lipgloss.Color("#ff8080"),
		},
	}},
}

func themeDefByName(name string) (themeDef, bool) {
	for _, def := range themeTable {
		if def.name == name {
			return def, true
		}
	}
	return themeDef{}, false
}

func namedThemeNames() []string {
	out := make([]string, len(themeTable))
	for i, def := range themeTable {
		out[i] = def.name
	}
	return out
}

func themePalette(name string) palette {
	def, _ := themeDefByName(resolveTheme(name))
	return def.palette
}

// themeIsDark reports the light/dark family of a theme from the table.
// auto and unknown names go through resolveTheme first.
func themeIsDark(name string) bool {
	def, _ := themeDefByName(resolveTheme(name))
	return def.dark
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

// resolveTheme only normalizes auto: a named theme is returned as itself,
// and auto (or any name not in the table) uses the cached terminal probe.
func resolveTheme(name string) string {
	if _, ok := themeDefByName(name); ok {
		return name
	}
	if detectDarkBackground() {
		return "dark"
	}
	return "light"
}
