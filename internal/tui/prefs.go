package tui

import (
	"github.com/dualface/kander/internal/config"
)

// uiPrefs is the UI preference view used inside the TUI; persistence goes through config.json alone.
type uiPrefs struct {
	Compact        bool
	Columns        int
	MinColumnWidth int
	Theme          string
	ThemeLight     string
	ThemeDark      string
	Refresh        int
	Single         bool
}

func defaultPrefs() uiPrefs {
	return prefsFromConfig(config.DefaultTUI())
}

func loadPrefs() uiPrefs {
	cfg, err := config.Load(true)
	if err != nil {
		return defaultPrefs()
	}
	return prefsFromConfig(cfg.TUI)
}

// savePrefs writes the caller-provided UI preferences into the scope
// config.json as a whole TUI section. Callers must pass unmerged scope
// values; this function does not filter overlay-only fields.
func savePrefs(prefs uiPrefs) (config.TUI, error) {
	value := prefsConfig(prefs)
	_, err := config.Update(func(cfg *config.Config) error {
		cfg.TUI = value
		return nil
	})
	return value, err
}

func prefsFromConfig(value config.TUI) uiPrefs {
	return uiPrefs{
		Compact:        value.Compact,
		Columns:        value.Columns,
		MinColumnWidth: value.MinColumnWidth,
		Theme:          value.Theme,
		ThemeLight:     value.ThemeLight,
		ThemeDark:      value.ThemeDark,
		Refresh:        value.Refresh,
		Single:         value.Single,
	}
}

func prefsConfig(prefs uiPrefs) config.TUI {
	theme := prefs.Theme
	if !containsString(themes, theme) {
		theme = "auto"
	}
	return config.TUI{
		Compact:        prefs.Compact,
		Columns:        clampColumns(prefs.Columns),
		MinColumnWidth: clampMinColumnWidth(prefs.MinColumnWidth),
		Refresh:        clampRefresh(prefs.Refresh),
		Single:         prefs.Single,
		Theme:          theme,
		ThemeLight:     namedThemeOr(prefs.ThemeLight, config.DefaultTUIThemeLight),
		ThemeDark:      namedThemeOr(prefs.ThemeDark, config.DefaultTUIThemeDark),
	}
}

// saveColumns remembers how many columns the user wants on screen.
// Only Columns is written so overlay-only TUI keys stay out of the scope file.
func saveColumns(count int) (config.TUI, error) {
	var written config.TUI
	_, err := config.Update(func(cfg *config.Config) error {
		cfg.TUI.Columns = clampColumns(count)
		written = cfg.TUI
		return nil
	})
	return written, err
}

// namedThemeOr keeps a concrete theme name and replaces anything else, such as
// "auto" or an unknown name, with the fallback.
func namedThemeOr(name, fallback string) string {
	if _, ok := themeDefByName(name); ok {
		return name
	}
	return fallback
}
