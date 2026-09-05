package tui

import (
	"github.com/dualface/kander/internal/config"
)

// uiPrefs 是 TUI 内部使用的界面偏好视图, 持久化统一交给 config.json.
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

// savePrefs 落盘界面偏好, 返回实际写入的取值, 便于调用方同步选项会话基线.
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

// saveColumns 记住用户希望同屏显示的栏目数.
func saveColumns(count int) (config.TUI, error) {
	prefs := loadPrefs()
	prefs.Columns = clampColumns(count)
	return savePrefs(prefs)
}
