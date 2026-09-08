package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// bareSpaces matches padding that carries no style, so the terminal background shows through it.
var bareSpaces = regexp.MustCompile(`\x1b\[0m( +)`)

// The help overlay must fill its whole rectangle with the theme background, including the gaps that
// appear when blocks of different widths and heights are joined.
func TestHelpOverlayFillsBackground(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(profile)

	// The widest size keeps the two-column layout, the narrowest falls back to a single column.
	for _, size := range [][2]int{{200, 60}, {120, 40}, {60, 40}} {
		app := &App{Theme: "light", Columns: 3, MinColumnWidth: minColumnWidth}
		app.Width, app.Height = size[0], size[1]
		_, popup := app.renderHelp()
		for i, line := range strings.Split(popup, "\n") {
			if match := bareSpaces.FindStringSubmatch(line); match != nil {
				t.Errorf("%dx%d line %d has %d unpainted spaces: %q", size[0], size[1], i, len(match[1]), line)
			}
		}
	}
}
