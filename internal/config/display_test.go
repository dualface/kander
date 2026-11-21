package config

import (
	"fmt"
	"path/filepath"
	"testing"
)

// displayShapes are the kinds of content of a newer version a configuration can hold: every
// unknown key the loader reports, plus the roles that name an unknown agent and the sections
// that hold nothing else.
func displayShapes() []struct {
	name   string
	mutate func(doc map[string]any)
} {
	type shape = struct {
		name   string
		mutate func(doc map[string]any)
	}
	var shapes []shape
	for _, c := range unknownCases {
		shapes = append(shapes, shape{fmt.Sprint(c.path), func(doc map[string]any) {
			if c.path[0] == "agents" {
				doc["agents"] = map[string]any{"custom": customAgent()}
			}
			OverlaySet(doc, c.value, c.path...)
		}})
	}
	return append(shapes,
		shape{"unknown kanban_agent", func(doc map[string]any) { doc["kanban_agent"] = "futureagent" }},
		shape{"unknown kanban_agents.small", func(doc map[string]any) {
			doc["kanban_agents"] = map[string]any{"large": "codex", "small": "futureagent"}
		}},
		shape{"unknown chat_agent", func(doc map[string]any) { doc["chat_agent"] = "futureagent" }},
		shape{"legacy PM reviewer", func(doc map[string]any) { doc["reviewers"] = map[string]any{"PM": "futureagent", "QA": "codex"} }},
		shape{"legacy PM reviewer only", func(doc map[string]any) { doc["reviewers"] = map[string]any{"PM": "futureagent"} }},
		shape{"scaled reviewer", func(doc map[string]any) {
			doc["reviewers"] = map[string]any{"large": map[string]any{"PMQA": "futureagent"}}
		}},
		shape{"reviewers of a newer version only", func(doc map[string]any) {
			doc["reviewers"] = map[string]any{"large": map[string]any{"FutureRole": "codex"}}
		}},
		shape{"tui of unknown keys only", func(doc map[string]any) { doc["tui"] = map[string]any{"future": 1} }},
		shape{"issue of unknown keys only", func(doc map[string]any) { doc["issue"] = map[string]any{"future": 1} }},
		shape{"git of unknown keys only", func(doc map[string]any) { doc["git"] = map[string]any{"future": 1} }},
		shape{"unusable built-in codex", func(doc map[string]any) {
			doc["agents"] = map[string]any{"codex": map[string]any{
				"args":    map[string]any{"start": []any{}, "resume": []any{}},
				"session": map[string]any{"mode": "hook:future-hook"},
			}}
		}},
		shape{"models of an unknown agent only", func(doc map[string]any) {
			doc["models"] = map[string]any{"kanban": map[string]any{"newagent": map[string]any{"large_model": "m"}}}
		}},
	)
}

// Whatever two kinds of newer content a scope file holds together, the display is built from
// the cleaned layers: it keeps no ignored item and no role that names an unknown agent. A
// display that fell back to the loaded configuration would show them.
func TestDisplayConfigCleansEveryPairOfShapes(t *testing.T) {
	shapes := displayShapes()
	for i := range shapes {
		for j := i; j < len(shapes); j++ {
			t.Run(shapes[i].name+" + "+shapes[j].name, func(t *testing.T) {
				setupHome(t)
				doc := scopeDocument(t)
				shapes[i].mutate(doc)
				shapes[j].mutate(doc)
				writeScope(t, doc)
				cfg, err := Load(false)
				if err != nil {
					t.Skipf("the combination does not load: %v", err)
				}
				display, err := DisplayConfig(cfg)
				if err != nil {
					t.Fatal(err)
				}
				if len(display.Ignored) != 0 || len(UnknownAgentRefs(display)) != 0 {
					t.Fatalf("display keeps ignored=%v refs=%v", ignoredPaths(display), UnknownAgentRefs(display))
				}
			})
		}
	}
}

// The same holds when one kind sits in the scope file and the other in the project overlay.
func TestDisplayConfigCleansScopeAndOverlayShapes(t *testing.T) {
	shapes := displayShapes()
	loaded := 0
	for i := range shapes {
		for j := range shapes {
			t.Run(shapes[i].name+" in scope, "+shapes[j].name+" in overlay", func(t *testing.T) {
				setupHome(t)
				doc := scopeDocument(t)
				shapes[i].mutate(doc)
				writeScope(t, doc)
				overlay := map[string]any{}
				shapes[j].mutate(overlay)
				project := t.TempDir()
				t.Chdir(project)
				writeJSONFile(t, filepath.Join(project, OverlayFilename), overlay)
				cfg, err := Load(false)
				if err != nil {
					t.Skipf("the combination does not load: %v", err)
				}
				loaded++
				display, err := DisplayConfig(cfg)
				if err != nil {
					t.Fatal(err)
				}
				if len(display.Ignored) != 0 || len(UnknownAgentRefs(display)) != 0 {
					t.Fatalf("display keeps ignored=%v refs=%v", ignoredPaths(display), UnknownAgentRefs(display))
				}
			})
		}
	}
	if loaded < len(shapes) {
		t.Fatalf("only %d combinations loaded", loaded)
	}
}
