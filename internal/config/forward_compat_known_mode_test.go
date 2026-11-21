package config

import "testing"

// A reason that makes an agents entry unusable never hides an invalid value of a session mode
// this version knows: the allocated command and the discovery of the entry are still checked.
func TestUnusableReasonDoesNotHideAnInvalidKnownSessionMode(t *testing.T) {
	setupHome(t)
	for _, tc := range []struct {
		name  string
		entry map[string]any
	}{
		{"unknown dialect, allocated without a command", map[string]any{"path": "sh", "dialect": "futuredialect", "session": map[string]any{"mode": "allocated"}}},
		{"built-in-only key, allocated without a command", map[string]any{"path": "sh", "dialect": "claude", "install_rules": false, "session": map[string]any{"mode": "allocated"}}},
		{"unknown dialect, discovered with an invalid format", map[string]any{"path": "sh", "dialect": "futuredialect", "session": map[string]any{"mode": "discovered", "discovery": map[string]any{"args": []any{"list"}, "format": "xml"}}}},
		{"no dialect and no args, discovery on another mode", map[string]any{"path": "sh", "session": map[string]any{"mode": "generated", "discovery": map[string]any{"args": []any{"list"}, "format": "lines"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := scopeDocument(t)
			doc["agents"] = map[string]any{"custom": tc.entry}
			writeScope(t, doc)
			if _, err := Load(false); err == nil {
				t.Fatal("an invalid session setting was accepted beside an unusable reason")
			}
		})
	}
}
