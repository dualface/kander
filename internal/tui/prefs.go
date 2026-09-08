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

// savePrefs writes only the UI fields that differ from the unmerged scope
// config. Overlay-only TUI values must not be copied back into config.json.
func savePrefs(prefs uiPrefs) (config.TUI, error) {
	desired := prefsConfig(prefs)
	var written config.TUI
	_, err := config.Update(func(cfg *config.Config) error {
		next := cfg.TUI
		if desired.Columns != next.Columns {
			next.Columns = desired.Columns
		}
		if desired.MinColumnWidth != next.MinColumnWidth {
			next.MinColumnWidth = desired.MinColumnWidth
		}
		if desired.Theme != next.Theme {
			next.Theme = desired.Theme
		}
		if desired.Refresh != next.Refresh {
			next.Refresh = desired.Refresh
		}
		if desired.Single != next.Single {
			next.Single = desired.Single
		}
		cfg.TUI = next
		written = next
		return nil
	})
	return written, err
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
