package tui

import (
	"github.com/dualface/kander/internal/config"
)

// uiPrefs is the UI preference view used inside the TUI; persistence goes through config.json alone.
type uiPrefs struct {
	Columns        int
	MinColumnWidth int
	Theme          string
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

// savePrefs writes the UI preferences to disk and returns the values actually written, so the caller can sync the options session baseline.
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
		Columns:        value.Columns,
		MinColumnWidth: value.MinColumnWidth,
		Theme:          value.Theme,
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
		Columns:        clampColumns(prefs.Columns),
		MinColumnWidth: clampMinColumnWidth(prefs.MinColumnWidth),
		Refresh:        clampRefresh(prefs.Refresh),
		Single:         prefs.Single,
		Theme:          theme,
	}
}

// saveColumns remembers how many columns the user wants on screen.
func saveColumns(count int) (config.TUI, error) {
	prefs := loadPrefs()
	prefs.Columns = clampColumns(count)
	return savePrefs(prefs)
}
