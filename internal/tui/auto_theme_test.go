package tui

import (
	"slices"
	"testing"
	"time"

	"github.com/dualface/kander/internal/config"
)

// stubStartupBackground pins the one-time startup probe and clears any runtime
// classification for the duration of a test.
func stubStartupBackground(t *testing.T, dark bool) {
	t.Helper()
	resetAutoBackground()
	t.Cleanup(resetAutoBackground)
	original := detectDarkBackground
	t.Cleanup(func() { detectDarkBackground = original })
	detectDarkBackground = func() bool { return dark }
}

func TestResolveThemeWithUsesConfiguredVariants(t *testing.T) {
	stubStartupBackground(t, false)
	variants := autoThemes{Light: "slate-light", Dark: "tide"}
	if got := resolveThemeWith("auto", variants); got != "slate-light" {
		t.Fatalf("light startup background resolved %q", got)
	}
	autoBackground.Store(2<<1 | 1)
	if got := resolveThemeWith("auto", variants); got != "tide" {
		t.Fatalf("runtime dark background resolved %q", got)
	}
	autoBackground.Store(3 << 1)
	if got := resolveThemeWith("auto", variants); got != "slate-light" {
		t.Fatalf("runtime light background resolved %q", got)
	}
	if got := resolveThemeWith("dusk", variants); got != "dusk" {
		t.Fatalf("named theme resolved %q", got)
	}
	if got := resolveThemeWith("auto", autoThemes{Light: "auto", Dark: "tide"}); got != config.DefaultTUIThemeLight {
		t.Fatalf("invalid light variant resolved %q", got)
	}
}

func TestProbeTickSwitchesToConfiguredDarkVariant(t *testing.T) {
	stubStartupBackground(t, false)
	app := newApp(false, 30, tuiPageContext(), nil, nil, "auto", 3, nil, nil)
	app.AutoThemes = autoThemes{Light: "light-warm", Dark: "dusk"}
	app.resolvedTheme = app.themeName()
	if app.resolvedTheme != "light-warm" {
		t.Fatalf("startup resolved %q", app.resolvedTheme)
	}
	bp, qr, _ := testProbe(t, time.Millisecond, 5*time.Second)
	app.probe = bp
	app.probeTick()
	if got := drainQueries(qr); got != backgroundOSCQuery {
		t.Fatalf("auto theme must probe, wrote %q", got)
	}
	bp.capture(true, true)
	app.probeTick()
	if app.resolvedTheme != "dusk" {
		t.Fatalf("resolved theme must switch to the dark variant, got %q", app.resolvedTheme)
	}
}

func TestAutoThemeOptionsListOneFamilyAndKeepCrossFamilyValue(t *testing.T) {
	_, panel := openPanel(t)
	values := func(current string, dark bool) []string {
		var out []string
		for _, option := range autoThemeOptions(panel, current, dark) {
			out = append(out, option.Value)
		}
		return out
	}
	light := values("light", false)
	if !slices.Equal(light, familyThemeNames(false)) || slices.Contains(light, "auto") {
		t.Fatalf("light options=%v", light)
	}
	for _, name := range light {
		if themeIsDark(name) {
			t.Fatalf("light options include dark theme %q", name)
		}
	}
	dark := values("dark", true)
	if !slices.Equal(dark, familyThemeNames(true)) {
		t.Fatalf("dark options=%v", dark)
	}
	if crossed := values("tide", false); crossed[len(crossed)-1] != "tide" || len(crossed) != len(light)+1 {
		t.Fatalf("cross-family value not kept: %v", crossed)
	}
}

func TestOptionsAutoThemeEditPreviewsOnlyEffectiveFamily(t *testing.T) {
	stubStartupBackground(t, true)
	app, panel := openPanel(t)
	app.Theme = "auto"
	pumpPanel(panel, panel.dispatch(sectionInterface))
	panel.bind.theme = "auto"
	panel.bind.themeDark = "tide"
	panel.bind.applyInterface(panel)
	if app.AutoThemes.Dark != "tide" || app.themeName() != "tide" {
		t.Fatalf("dark variant edit did not preview: %+v -> %q", app.AutoThemes, app.themeName())
	}
	if panel.rebuildFocus != interfaceFocusKey("theme_dark") {
		t.Fatalf("rebuildFocus=%q", panel.rebuildFocus)
	}
	pumpPanel(panel, panel.rebuildSection())
	panel.bind.themeLight = "slate-light"
	panel.bind.applyInterface(panel)
	if app.AutoThemes.Light != "slate-light" || app.themeName() != "tide" {
		t.Fatalf("light variant edit on a dark background changed the display: %q", app.themeName())
	}
	if err := panel.persistNow(); err != nil {
		t.Fatal(err)
	}
	if prefs := loadPrefs(); prefs.ThemeLight != "slate-light" || prefs.ThemeDark != "tide" {
		t.Fatalf("saved variants=%q/%q", prefs.ThemeLight, prefs.ThemeDark)
	}
}

func TestOptionsAutoThemeKeyboardEditsLightVariant(t *testing.T) {
	stubStartupBackground(t, false)
	app, panel := openPanel(t)
	pumpPanel(panel, panel.dispatch(sectionInterface))
	for range 3 {
		drivePanel(panel, keyMsg("down"))
	}
	drivePanel(panel, keyMsg("right"))
	want := familyThemeNames(false)[1]
	if panel.bind.themeLight != want || app.AutoThemes.Light != want || panel.session.Config.TUI.ThemeLight != want {
		t.Fatalf("binding=%q app=%q session=%q want %q",
			panel.bind.themeLight, app.AutoThemes.Light, panel.session.Config.TUI.ThemeLight, want)
	}
}

func TestOptionsKeepUneditedCrossFamilyVariant(t *testing.T) {
	stored := config.DefaultConfig()
	stored.WelcomeComplete = true
	stored.TUI.ThemeLight = "tide"
	_, panel := openPanel(t, stored)
	pumpPanel(panel, panel.dispatch(sectionInterface))
	if panel.bind.themeLight != "tide" {
		t.Fatalf("binding=%q", panel.bind.themeLight)
	}
	panel.bind.refresh = 60
	panel.bind.applyInterface(panel)
	if err := panel.persistNow(); err != nil {
		t.Fatal(err)
	}
	if prefs := loadPrefs(); prefs.ThemeLight != "tide" || prefs.Refresh != 60 {
		t.Fatalf("saved prefs=%+v", prefs)
	}
}

func TestPrefsConfigResetsInvalidAutoVariants(t *testing.T) {
	got := prefsConfig(uiPrefs{Columns: 3, MinColumnWidth: 40, Theme: "auto", Refresh: 30, ThemeLight: "auto", ThemeDark: "slate-light"})
	if got.ThemeLight != config.DefaultTUIThemeLight || got.ThemeDark != "slate-light" {
		t.Fatalf("variants=%q/%q", got.ThemeLight, got.ThemeDark)
	}
}
