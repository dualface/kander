package tui

import (
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

var hexColorPattern = regexp.MustCompile(`^#[0-9a-f]{6}$`)

func TestThemePaletteAnchorsAndHex(t *testing.T) {
	dark := themePalette("dark")
	light := themePalette("light")
	if dark.Bg != "#16181d" {
		t.Fatalf("dark Bg=%q", dark.Bg)
	}
	if light.Bg != "#fafafa" {
		t.Fatalf("light Bg=%q", light.Bg)
	}
	for _, name := range []string{"light", "dark"} {
		p := themePalette(name)
		assertHexColor(t, name+".Base", p.Base)
		assertHexColor(t, name+".Bg", p.Bg)
		assertHexColor(t, name+".Dim", p.Dim)
		assertHexColor(t, name+".Separator", p.Separator)
		assertHexColor(t, name+".Accent", p.Accent)
		assertHexColor(t, name+".Bar", p.Bar)
		assertHexColor(t, name+".ChromeFg", p.ChromeFg)
		assertHexColor(t, name+".ChromeBg", p.ChromeBg)
		assertHexColor(t, name+".PopupFg", p.PopupFg)
		assertHexColor(t, name+".PopupEdge", p.PopupEdge)
		assertHexColor(t, name+".Warn", p.Warn)
		assertHexColor(t, name+".OK", p.OK)
		if len(p.Headings) != len(allStates) {
			t.Fatalf("%s headings=%d want %d", name, len(p.Headings), len(allStates))
		}
		for _, state := range allStates {
			color, ok := p.Headings[state]
			if !ok {
				t.Fatalf("%s missing heading %s", name, state)
			}
			assertHexColor(t, name+".Headings."+state, color)
		}
	}
	if dark.Base == light.Base || dark.Dim == light.Dim || dark.Accent == light.Accent {
		t.Fatal("light and dark should not share one foreground set")
	}
}

func TestThemePaletteContrast(t *testing.T) {
	for _, name := range []string{"light", "dark"} {
		p := themePalette(name)
		bg := string(p.Bg)
		baseRatio := contrastRatio(string(p.Base), bg)
		body := []struct {
			label string
			color lipgloss.Color
		}{
			{"Base", p.Base},
			{"Accent", p.Accent},
			{"Bar", p.Bar},
			{"Warn", p.Warn},
			{"OK", p.OK},
			{"PopupFg", p.PopupFg},
			{"PopupEdge", p.PopupEdge},
		}
		for _, item := range body {
			ratio := contrastRatio(string(item.color), bg)
			if ratio < 4.5 {
				t.Fatalf("%s %s on Bg = %.3f, want >= 4.5", name, item.label, ratio)
			}
		}
		for _, state := range allStates {
			ratio := contrastRatio(string(p.Headings[state]), bg)
			if ratio < 4.5 {
				t.Fatalf("%s heading %s on Bg = %.3f, want >= 4.5", name, state, ratio)
			}
		}
		dimRatio := contrastRatio(string(p.Dim), bg)
		sepRatio := contrastRatio(string(p.Separator), bg)
		if dimRatio < 3 {
			t.Fatalf("%s Dim on Bg = %.3f, want >= 3", name, dimRatio)
		}
		if sepRatio < 3 {
			t.Fatalf("%s Separator on Bg = %.3f, want >= 3", name, sepRatio)
		}
		if dimRatio >= baseRatio {
			t.Fatalf("%s Dim contrast %.3f should be below Base %.3f", name, dimRatio, baseRatio)
		}
		if sepRatio >= baseRatio {
			t.Fatalf("%s Separator contrast %.3f should be below Base %.3f", name, sepRatio, baseRatio)
		}
		chromeRatio := contrastRatio(string(p.ChromeFg), string(p.ChromeBg))
		if chromeRatio < 4.5 {
			t.Fatalf("%s ChromeFg on ChromeBg = %.3f, want >= 4.5", name, chromeRatio)
		}
	}
}

func TestThemePaletteTrueColorSequences(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(previous)

	light := themePalette("light")
	dark := themePalette("dark")
	if !strings.Contains(light.fillLine(2), "48;2;250;250;250") {
		t.Fatalf("light canvas should paint #fafafa, got %q", light.fillLine(2))
	}
	if !strings.Contains(dark.fillLine(2), "48;2;22;24;29") {
		t.Fatalf("dark canvas should paint #16181d, got %q", dark.fillLine(2))
	}
	if !strings.Contains(light.ink(light.Base).Render("x"), "38;2;22;24;29") {
		t.Fatalf("light ink should paint #16181d, got %q", light.ink(light.Base).Render("x"))
	}
	if !strings.Contains(dark.ink(dark.Base).Render("x"), "38;2;230;232;235") {
		t.Fatalf("dark ink should paint #e6e8eb, got %q", dark.ink(dark.Base).Render("x"))
	}
}

func TestThemePaletteProfileDowngrade(t *testing.T) {
	tags := []string{"", "title", "separator", "heading-backlog", "popup-warn"}
	for _, name := range []string{"light", "dark"} {
		p := themePalette(name)
		for _, profile := range []termenv.Profile{termenv.ANSI256, termenv.ANSI} {
			t.Run(name+"/"+profile.Name(), func(t *testing.T) {
				previous := lipgloss.ColorProfile()
				lipgloss.SetColorProfile(profile)
				defer lipgloss.SetColorProfile(previous)

				for _, tag := range tags {
					_ = styleFor(tag, p).Render("x")
				}
				_ = p.fillLine(4)

				fg := profile.Color(string(p.Base))
				bg := profile.Color(string(p.Bg))
				if fg == nil || bg == nil {
					t.Fatal("resolved color is nil")
				}
				if fg.Sequence(false) == bg.Sequence(false) {
					t.Fatalf("foreground and background collapsed to %q", fg.Sequence(false))
				}
			})
		}
	}
}

func assertHexColor(t *testing.T, label string, color lipgloss.Color) {
	t.Helper()
	value := string(color)
	if !hexColorPattern.MatchString(value) {
		t.Fatalf("%s = %q, want #rrggbb", label, value)
	}
}

func contrastRatio(a, b string) float64 {
	l1, l2 := relativeLuminance(a), relativeLuminance(b)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

func relativeLuminance(hex string) float64 {
	if len(hex) != 7 || hex[0] != '#' {
		return 0
	}
	r := linearizeChannel(hexByte(hex[1], hex[2]))
	g := linearizeChannel(hexByte(hex[3], hex[4]))
	b := linearizeChannel(hexByte(hex[5], hex[6]))
	return 0.2126*r + 0.7152*g + 0.0722*b
}

func linearizeChannel(value float64) float64 {
	c := value / 255
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func hexByte(hi, lo byte) float64 {
	return float64(hexNibble(hi)<<4 | hexNibble(lo))
}

func hexNibble(b byte) int {
	switch {
	case b >= '0' && b <= '9':
		return int(b - '0')
	case b >= 'a' && b <= 'f':
		return int(b-'a') + 10
	case b >= 'A' && b <= 'F':
		return int(b-'A') + 10
	}
	return 0
}
