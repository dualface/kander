package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// displayShape is one kind of content of a newer version. inject adds it to a layer (the full
// scope document or the sparse project overlay); twin adds what the configuration without that
// content still holds, for the shapes that sit inside an entry the twin keeps.
type displayShape struct {
	name      string
	scopeOnly bool
	inject    func(layer map[string]any)
	twin      func(layer map[string]any)
}

func unknownAt(value any, path ...string) displayShape {
	return displayShape{name: fmt.Sprint(path), inject: func(layer map[string]any) { OverlaySet(layer, value, path...) }}
}

// displayShapes covers the kinds of newer content a layer can hold: a top-level key, an unknown
// key inside every known section, an unknown key under server and project, a models entry for an
// unknown agent, an agents entry this version cannot use, and the roles that name an unknown
// agent.
func displayShapes() []displayShape {
	unusableCodex := map[string]any{
		"args":    map[string]any{"start": []any{}, "resume": []any{}},
		"session": map[string]any{"mode": "hook:future-hook"},
	}
	customWith := func(path ...string) displayShape {
		return displayShape{
			name:      fmt.Sprint(path),
			scopeOnly: true,
			inject: func(layer map[string]any) {
				layer["agents"] = map[string]any{"custom": customAgent()}
				OverlaySet(layer, "x", path...)
			},
			twin: func(layer map[string]any) { layer["agents"] = map[string]any{"custom": customAgent()} },
		}
	}
	return []displayShape{
		// A top-level key.
		unknownAt(map[string]any{"enabled": true}, "future_top"),
		// An unknown key inside a known section.
		unknownAt("x", "tui", "future"),
		unknownAt(true, "rules", "future_module"),
		unknownAt("x", "git", "future"),
		unknownAt("x", "issue", "future"),
		unknownAt("codex", "kanban_agents", "future_scale"),
		unknownAt("codex", "reviewers", "large", "FutureRole"),
		unknownAt(map[string]any{"PMQA": "codex"}, "reviewers", "future_scale"),
		unknownAt("auto", "review_stages", "small", "FutureRole"),
		unknownAt(map[string]any{"a": "b"}, "models", "future_section"),
		unknownAt("x", "models", "kanban", "codex", "future_field"),
		unknownAt("x", "models", "review", "codex", "future_field"),
		unknownAt("x", "models", "chat", "codex", "future_field"),
		unknownAt("x", "models", "review_roles", "PMQA", "future_field"),
		customWith("agents", "custom", "future_key"),
		customWith("agents", "custom", "session", "future_session_key"),
		// An unknown key under server and project.
		unknownAt("t", "server", "token"),
		unknownAt(true, "project", "extra"),
		// A models entry for an unknown agent.
		unknownAt(map[string]any{"large_model": "m"}, "models", "kanban", "future_agent"),
		unknownAt(map[string]any{"model": "m"}, "models", "review", "future_agent"),
		unknownAt(map[string]any{"model": "m"}, "models", "chat", "future_agent"),
		// An agents entry this version cannot use.
		unknownAt(map[string]any{"path": "sh", "dialect": "futuredialect"}, "agents", "badagent"),
		unknownAt(unusableCodex, "agents", "codex"),
		// A role that names an unknown agent.
		unknownAt("futureagent", "kanban_agent"),
		unknownAt("futureagent", "kanban_agents", "small"),
		unknownAt("futureagent", "chat_agent"),
		unknownAt(map[string]any{"PM": "futureagent", "QA": "codex"}, "reviewers"),
		unknownAt(map[string]any{"PM": "futureagent"}, "reviewers"),
		unknownAt(map[string]any{"large": map[string]any{"PMQA": "futureagent"}}, "reviewers"),
		// A section that holds nothing else.
		unknownAt(map[string]any{"future": 1}, "tui"),
		unknownAt(map[string]any{"large": map[string]any{"FutureRole": "codex"}}, "reviewers"),
	}
}

// placed is a shape in one layer.
type placed struct {
	shape   displayShape
	overlay bool
}

func (p placed) String() string {
	if p.overlay {
		return p.shape.name + " (overlay)"
	}
	return p.shape.name + " (scope)"
}

func placements() []placed {
	var out []placed
	for _, shape := range displayShapes() {
		out = append(out, placed{shape: shape})
		if !shape.scopeOnly {
			out = append(out, placed{shape: shape, overlay: true})
		}
	}
	return out
}

// documentsOf builds the scope document and the overlay (nil when empty) that hold the given
// shapes. The twin documents hold what the same configuration has without that content.
func documentsOf(t *testing.T, items []placed, twin bool) (scope, overlay map[string]any) {
	t.Helper()
	scope, overlay = scopeDocument(t), map[string]any{}
	for _, item := range items {
		layer := scope
		if item.overlay {
			layer = overlay
		}
		if twin {
			if item.shape.twin != nil {
				item.shape.twin(layer)
			}
			continue
		}
		item.shape.inject(layer)
	}
	if len(overlay) == 0 {
		overlay = nil
	}
	return scope, overlay
}

func loadDocuments(t *testing.T, scope, overlay map[string]any) (*Config, error) {
	t.Helper()
	writeJSONFile(t, os.Getenv(EnvConfig), scope)
	overlayPath := filepath.Join(t.TempDir(), OverlayFilename)
	t.Chdir(filepath.Dir(overlayPath))
	if overlay != nil {
		writeJSONFile(t, overlayPath, overlay)
	}
	return Load(false)
}

// Whatever two kinds of newer content the layers hold together, in the scope file, in the project
// overlay, or one in each, the display equals the configuration the same layers give without
// that content, and it keeps no ignored item: the display is the loader's own merge and
// validation over the cleaned layers.
func TestDisplayConfigEqualsTheConfigurationWithoutTheIgnoredContent(t *testing.T) {
	all := placements()
	for i := range all {
		for j := i; j < len(all); j++ {
			items := []placed{all[i], all[j]}
			t.Run(items[0].String()+" + "+items[1].String(), func(t *testing.T) {
				setupHome(t)
				writeScope(t, scopeDocument(t))
				scope, overlay := documentsOf(t, items, false)
				dirty, err := loadDocuments(t, scope, overlay)
				if err != nil {
					t.Skipf("the combination does not load: %v", err)
				}
				display, err := DisplayConfig(dirty)
				if err != nil {
					t.Fatal(err)
				}
				if len(display.Ignored) != 0 {
					t.Fatalf("display keeps %v", ignoredPaths(display))
				}
				scope, overlay = documentsOf(t, items, true)
				twin, err := loadDocuments(t, scope, overlay)
				if err != nil {
					t.Fatalf("the twin does not load: %v", err)
				}
				if got, want := marshalConfig(t, display), marshalConfig(t, twin); got != want {
					t.Fatalf("display differs from the configuration without the content:\n%s\n%s", got, want)
				}
			})
		}
	}
}

func marshalConfig(t *testing.T, cfg *Config) string {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
