package tui

import (
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strings"
	"testing"

	glamstyles "github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/dualface/kander/internal/launch"
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
	for _, name := range namedThemeNames() {
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

func TestThemePaletteGoldenLightAndDark(t *testing.T) {
	wantLight := palette{
		Base:      "#16181d",
		Bg:        "#fafafa",
		Dim:       "#6b7280",
		Separator: "#828892",
		Accent:    "#9d2ec5",
		Bar:       "#1d4ed8",
		ChromeFg:  "#f7f7fb",
		ChromeBg:  "#6b21a8",
		PopupFg:   "#16181d",
		PopupEdge: "#9d2ec5",
		Warn:      "#c62828",
		OK:        "#2e7d32",
		Headings: map[string]lipgloss.Color{
			"backlog":  "#0e7490",
			"todo":     "#a16207",
			"working":  "#1d4ed8",
			"review":   "#9d2ec5",
			"done":     "#15803d",
			"archived": "#7e22ce",
			"trash":    "#b91c1c",
		},
	}
	wantDark := palette{
		Base:      "#e6e8eb",
		Bg:        "#16181d",
		Dim:       "#8b919a",
		Separator: "#6a7078",
		Accent:    "#d670d6",
		Bar:       "#6ea8fe",
		ChromeFg:  "#f7f7fb",
		ChromeBg:  "#6b21a8",
		PopupFg:   "#e6e8eb",
		PopupEdge: "#d670d6",
		Warn:      "#f07178",
		OK:        "#7fd17f",
		Headings: map[string]lipgloss.Color{
			"backlog":  "#4dd0e1",
			"todo":     "#e6c35c",
			"working":  "#6ea8fe",
			"review":   "#d670d6",
			"done":     "#7fd17f",
			"archived": "#c084d0",
			"trash":    "#f07178",
		},
	}
	assertPaletteEqual(t, "light", themePalette("light"), wantLight)
	assertPaletteEqual(t, "dark", themePalette("dark"), wantDark)
}

func assertPaletteEqual(t *testing.T, name string, got, want palette) {
	t.Helper()
	if got.Base != want.Base || got.Bg != want.Bg || got.Dim != want.Dim || got.Separator != want.Separator {
		t.Fatalf("%s canvas got %+v want %+v", name, got, want)
	}
	if got.Accent != want.Accent || got.Bar != want.Bar || got.ChromeFg != want.ChromeFg || got.ChromeBg != want.ChromeBg {
		t.Fatalf("%s chrome got %+v want %+v", name, got, want)
	}
	if got.PopupFg != want.PopupFg || got.PopupEdge != want.PopupEdge || got.Warn != want.Warn || got.OK != want.OK {
		t.Fatalf("%s popup got %+v want %+v", name, got, want)
	}
	for _, state := range allStates {
		if got.Headings[state] != want.Headings[state] {
			t.Fatalf("%s heading %s = %q want %q", name, state, got.Headings[state], want.Headings[state])
		}
	}
}

func themeBodyContrastMin(name string) float64 {
	if strings.HasSuffix(name, "-contrast") {
		return 7
	}
	return 4.5
}

func TestThemePaletteContrast(t *testing.T) {
	for _, name := range namedThemeNames() {
		p := themePalette(name)
		bg := string(p.Bg)
		minRatio := themeBodyContrastMin(name)
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
			if ratio < minRatio {
				t.Fatalf("%s %s on Bg = %.3f, want >= %.1f", name, item.label, ratio, minRatio)
			}
		}
		for _, state := range allStates {
			ratio := contrastRatio(string(p.Headings[state]), bg)
			if ratio < minRatio {
				t.Fatalf("%s heading %s on Bg = %.3f, want >= %.1f", name, state, ratio, minRatio)
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
		if chromeRatio < minRatio {
			t.Fatalf("%s ChromeFg on ChromeBg = %.3f, want >= %.1f", name, chromeRatio, minRatio)
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
	seen := map[string]string{}
	for _, name := range namedThemeNames() {
		p := themePalette(name)
		line := p.fillLine(2)
		if !strings.Contains(line, "48;2;") {
			t.Fatalf("%s canvas missing truecolor bg, got %q", name, line)
		}
		if prev, ok := seen[line]; ok {
			t.Fatalf("%s and %s painted the same canvas %q", name, prev, line)
		}
		seen[line] = name
	}
}

func TestMarkdownRenderFollowsColorProfile(t *testing.T) {
	doc := "body text for the theme canvas"
	cases := []struct {
		profile termenv.Profile
		theme   string
	}{
		{termenv.TrueColor, "light"},
		{termenv.ANSI256, "light"},
	}
	for _, tc := range cases {
		t.Run(tc.profile.Name(), func(t *testing.T) {
			previous := lipgloss.ColorProfile()
			lipgloss.SetColorProfile(tc.profile)
			defer lipgloss.SetColorProfile(previous)

			joined := strings.Join(renderMarkdown(doc, 40, tc.theme), "\n")
			wantBG := tc.profile.Color(string(themePalette(tc.theme).Bg)).Sequence(true)
			if wantBG == "" || !strings.Contains(joined, wantBG) {
				t.Fatalf("markdown missing profile bg %q: %q", wantBG, joined)
			}
			if tc.profile == termenv.ANSI256 && strings.Contains(joined, "48;2;250;250;250") {
				t.Fatalf("ANSI256 markdown still emitted truecolor canvas: %q", joined)
			}
		})
	}
}

func TestThemePaletteProfileDowngrade(t *testing.T) {
	tags := []string{"", "title", "separator", "heading-backlog", "popup-warn"}
	for _, name := range namedThemeNames() {
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

func trueColorSeq(hex string, foreground bool) string {
	prefix := "48;2"
	if foreground {
		prefix = "38;2"
	}
	return fmt.Sprintf("%s;%.0f;%.0f;%.0f", prefix, hexByte(hex[1], hex[2]), hexByte(hex[3], hex[4]), hexByte(hex[5], hex[6]))
}

func TestResolveThemeNamedAndAuto(t *testing.T) {
	originalDetector := detectDarkBackground
	t.Cleanup(func() { detectDarkBackground = originalDetector })
	detections := 0
	detectDarkBackground = func() bool {
		detections++
		return true
	}
	for _, name := range namedThemeNames() {
		if got := resolveTheme(name); got != name {
			t.Fatalf("named %s: got %q", name, got)
		}
	}
	if detections != 0 {
		t.Fatalf("named themes should not probe, detections=%d", detections)
	}
	if got := resolveTheme("auto"); got != "dark" || detections != 1 {
		t.Fatalf("auto dark: theme=%q detections=%d", got, detections)
	}
	detectDarkBackground = func() bool {
		detections++
		return false
	}
	if got := resolveTheme("auto"); got != "light" || detections != 2 {
		t.Fatalf("auto light: theme=%q detections=%d", got, detections)
	}
}

func TestThemeIsDarkClassifiesFamilies(t *testing.T) {
	for _, name := range []string{"light", "light-warm", "light-contrast"} {
		if themeIsDark(name) {
			t.Fatalf("%s should be light", name)
		}
	}
	for _, name := range []string{"dark", "dark-soft", "dark-contrast"} {
		if !themeIsDark(name) {
			t.Fatalf("%s should be dark", name)
		}
	}
}

func TestMarkdownCanvasStyleOwnBackgroundAndFamily(t *testing.T) {
	for _, name := range namedThemeNames() {
		got := markdownCanvasStyle(name)
		wantBG := string(themePalette(name).Bg)
		if got.Document.BackgroundColor == nil || *got.Document.BackgroundColor != wantBG {
			t.Fatalf("%s document bg=%v want %s", name, got.Document.BackgroundColor, wantBG)
		}
		key := glamstyles.LightStyle
		if themeIsDark(name) {
			key = glamstyles.DarkStyle
		}
		src := glamstyles.DefaultStyles[key]
		if src == nil {
			t.Fatalf("missing glamour style %s", key)
		}
		want := *src
		want.Document.BackgroundColor = &wantBG
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s base style is not %s", name, key)
		}
	}
}

// TestThemeSurfacesPaintOwnBackground checks that in-process TrueColor
// frames contain each theme's canvas sequence. It does not close the
// live-terminal walkthrough; that record is testdata/theme-live-walkthrough.md.
func TestThemeSurfacesPaintOwnBackground(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(previous)

	for _, name := range namedThemeNames() {
		app := newApp(true, 30, tuiPageContext(),
			func() (BoardPayload, error) {
				return BoardPayload{Tasks: []Task{{
					TaskID: "20260909-demo-task", Title: "Demo", State: "todo", Type: "Feature", Kind: "small",
				}}}, nil
			},
			func(id string) (Task, error) {
				return Task{TaskID: id, Title: "Demo", State: "todo", Document: "# Hi\nbody"}, nil
			},
			name, 40, nil, func(string) (bool, string) { return true, "" })
		app.Width, app.Height = 120, 32
		board, err := app.GetBoard()
		if err != nil {
			t.Fatal(err)
		}
		app.Model.SetBoard(board)

		bg := trueColorSeq(string(themePalette(name).Bg), false)
		surfaces := map[string]string{"board": app.renderBoardView()}
		task, err := app.GetTask("20260909-demo-task")
		if err != nil {
			t.Fatal(err)
		}
		app.Detail = &task
		surfaces["detail"] = app.renderDetailView()
		app.Detail = nil
		_, help := app.renderHelp()
		surfaces["help"] = help
		app.StartConfirmation = &startDialog{
			startRequest: startRequest{StartPreview: launch.StartPreview{
				TaskID: "20260909-demo-task", State: "todo", Agent: "cursor", Launcher: "herdr",
			}},
			phase: startReady,
		}
		_, start := app.renderStartConfirmation()
		surfaces["start"] = start
		app.StartConfirmation = nil
		for label, out := range surfaces {
			if !strings.Contains(out, bg) {
				t.Fatalf("%s %s missing canvas %s", name, label, bg)
			}
		}
	}
}
