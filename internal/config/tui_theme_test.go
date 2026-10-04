package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestValidateTUIAcceptsNamedThemes(t *testing.T) {
	want := []string{"auto", "light", "light-warm", "light-contrast", "dark", "dark-soft", "dark-contrast", "tide", "dusk", "slate-dark", "slate-light", "matcha-zen", "bamboo-multiplex"}
	if strings.Join(TUIThemes, ",") != strings.Join(want, ",") {
		t.Fatalf("TUIThemes=%v", TUIThemes)
	}
	for _, theme := range TUIThemes {
		t.Run("accept "+theme, func(t *testing.T) {
			raw := minimalPayload(map[string]any{
				"tui": map[string]any{
					"columns": 3, "min_column_width": 40, "refresh": 30, "single": false, "theme": theme,
				},
			})
			data, err := json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ValidateJSON(data)
			if err != nil {
				t.Fatal(err)
			}
			if got.TUI.Theme != theme {
				t.Fatalf("theme=%q", got.TUI.Theme)
			}
		})
	}
	t.Run("unknown theme locates tui.theme", func(t *testing.T) {
		raw := minimalPayload(map[string]any{
			"tui": map[string]any{
				"columns": 3, "min_column_width": 40, "refresh": 30, "single": false, "theme": "blue",
			},
		})
		data, err := json.Marshal(raw)
		if err != nil {
			t.Fatal(err)
		}
		_, err = ValidateJSON(data)
		if !IsError(err) {
			t.Fatalf("expected config error, got %v", err)
		}
		if !strings.Contains(err.Error(), "tui.theme") {
			t.Fatalf("error %q should locate tui.theme", err)
		}
	})
}

func validateTUIPayload(t *testing.T, tui map[string]any) (*Config, error) {
	t.Helper()
	data, err := json.Marshal(minimalPayload(map[string]any{"tui": tui}))
	if err != nil {
		t.Fatal(err)
	}
	return ValidateJSON(data)
}

func TestValidateTUIAutoVariantThemes(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{"columns": 3, "min_column_width": 40, "refresh": 30, "single": false, "theme": "auto"}
	}
	t.Run("missing keys default to light and dark", func(t *testing.T) {
		got, err := validateTUIPayload(t, base())
		if err != nil {
			t.Fatal(err)
		}
		if got.TUI.ThemeLight != DefaultTUIThemeLight || got.TUI.ThemeDark != DefaultTUIThemeDark {
			t.Fatalf("variants=%q/%q", got.TUI.ThemeLight, got.TUI.ThemeDark)
		}
	})
	t.Run("named and cross-family values are kept", func(t *testing.T) {
		tui := base()
		tui["theme_light"] = "tide"
		tui["theme_dark"] = "slate-light"
		got, err := validateTUIPayload(t, tui)
		if err != nil {
			t.Fatal(err)
		}
		if got.TUI.ThemeLight != "tide" || got.TUI.ThemeDark != "slate-light" {
			t.Fatalf("variants=%q/%q", got.TUI.ThemeLight, got.TUI.ThemeDark)
		}
	})
	for _, tc := range []struct {
		key   string
		value any
	}{
		{"theme_light", "auto"},
		{"theme_dark", "auto"},
		{"theme_light", "blue"},
		{"theme_dark", 7},
	} {
		t.Run("reject "+tc.key, func(t *testing.T) {
			tui := base()
			tui[tc.key] = tc.value
			_, err := validateTUIPayload(t, tui)
			if !IsError(err) || !strings.Contains(err.Error(), "tui."+tc.key) {
				t.Fatalf("expected config error locating tui.%s, got %v", tc.key, err)
			}
		})
	}
}

func TestNamedTUIThemesExcludesAuto(t *testing.T) {
	named := NamedTUIThemes()
	if len(named) != len(TUIThemes)-1 || slices.Contains(named, "auto") {
		t.Fatalf("named=%v", named)
	}
	if !slices.Contains(TUIThemes, "auto") {
		t.Fatal("NamedTUIThemes mutated TUIThemes")
	}
}

func TestRepairResetsInvalidAutoVariantTheme(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvConfig, path)
	original := []byte(`{
		"schema_version": 1, "welcome_complete": true, "kanban_agent": "claude", "launcher": "foreground",
		"tui": {"columns":5,"min_column_width":40,"refresh":30,"single":false,"theme":"auto",
			"theme_light":"auto","theme_dark":"tide"}
	}`)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Repair(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TUI.ThemeLight != DefaultTUIThemeLight || cfg.TUI.ThemeDark != "tide" {
		t.Fatalf("variants=%q/%q", cfg.TUI.ThemeLight, cfg.TUI.ThemeDark)
	}
}

func TestApplyOverlayMergesOneAutoVariantTheme(t *testing.T) {
	scope := DefaultConfig()
	scope.WelcomeComplete = true
	scope.TUI.ThemeLight = "slate-light"
	merged, err := ApplyOverlay(scope, map[string]any{"tui": map[string]any{"theme_dark": "dusk"}})
	if err != nil {
		t.Fatal(err)
	}
	if merged.TUI.ThemeLight != "slate-light" || merged.TUI.ThemeDark != "dusk" {
		t.Fatalf("merged variants=%q/%q", merged.TUI.ThemeLight, merged.TUI.ThemeDark)
	}
	if scope.TUI.ThemeDark != DefaultTUIThemeDark {
		t.Fatalf("scope mutated: %q", scope.TUI.ThemeDark)
	}
}
