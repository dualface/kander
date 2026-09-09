package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateTUIAcceptsNamedThemes(t *testing.T) {
	want := []string{"auto", "light", "light-warm", "light-contrast", "dark", "dark-soft", "dark-contrast"}
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
