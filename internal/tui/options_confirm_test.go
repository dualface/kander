package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestInlineConfirmKeepsOneSpaceBeforeButtons(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(profile)

	p := themePalette("light")
	theme := huhThemeForTest(p)
	marker := "(inherited On)"
	title := "Show only the current column:  " + marker
	v := true
	f := inlineConfirm().Title(title).Value(&v).WithTheme(theme).WithWidth(100)
	_ = f.Init()
	plain := ansi.Strip(f.View())
	line := strings.Split(plain, "\n")[0]
	idx := strings.Index(line, marker)
	if idx < 0 {
		t.Fatalf("marker missing: %q", line)
	}
	after := line[idx+len(marker):]
	n := 0
	for _, r := range after {
		if r == ' ' {
			n++
			continue
		}
		break
	}
	if n != 1 {
		t.Fatalf("spaces after inherited marker=%d, want 1; line=%q", n, line)
	}
}

func huhThemeForTest(p palette) *huh.Theme {
	theme := huh.ThemeBase()
	applyHuhPalette(theme, p)
	return theme
}
