package config

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// serverUnknownCases adds one unknown sub-key to each v2-only section.
var serverUnknownCases = []struct {
	path  []string
	value any
}{
	{[]string{"server", "token"}, "secret-from-a-newer-version"},
	{[]string{"project", "extra"}, map[string]any{"a": 1}},
}

func documentWithServerUnknowns(t *testing.T, withKnown bool) map[string]any {
	t.Helper()
	doc := scopeDocument(t)
	if withKnown {
		doc["server"] = map[string]any{"url": "http://127.0.0.1:7979"}
		doc["project"] = map[string]any{"name": "demo"}
	}
	for _, c := range serverUnknownCases {
		OverlaySet(doc, c.value, c.path...)
	}
	return doc
}

func TestServerAndProjectUnknownKeysAreIgnoredAndListed(t *testing.T) {
	for _, withKnown := range []bool{true, false} {
		setupHome(t)
		writeScope(t, documentWithServerUnknowns(t, withKnown))
		cfg, err := Load(false)
		if err != nil {
			t.Fatalf("unknown server/project keys must not fail the load: %v", err)
		}
		if want := []string{"project.extra", "server.token"}; !slices.Equal(ignoredPaths(cfg), want) {
			t.Fatalf("withKnown=%v ignored=%v want %v", withKnown, ignoredPaths(cfg), want)
		}
		if withKnown && (cfg.Server.URL != "http://127.0.0.1:7979" || cfg.Project.Name != "demo") {
			t.Fatalf("known server/project values stopped applying: %+v %+v", cfg.Server, cfg.Project)
		}
	}
}

func TestSaveKeepsServerAndProjectUnknownKeys(t *testing.T) {
	// withKnown=false covers a section that holds only unknown keys: the encoding of the
	// empty known part omits the section, so the kept keys must bring it back.
	for _, withKnown := range []bool{true, false} {
		for name, save := range map[string]func(*Config) error{
			"Save": func(cfg *Config) error { _, err := Save(cfg); return err },
			"SaveIfUnchanged": func(cfg *Config) error {
				_, err := SaveIfUnchanged(cfg, Clone(cfg))
				return err
			},
			"Update": func(*Config) error {
				_, err := Update(func(c *Config) error { c.TUI.Columns = 3; return nil })
				return err
			},
			"Repair": func(*Config) error { _, _, err := Repair(nil); return err },
		} {
			t.Run(name, func(t *testing.T) {
				setupHome(t)
				path := writeScope(t, documentWithServerUnknowns(t, withKnown))
				cfg, err := Load(false)
				if err != nil {
					t.Fatal(err)
				}
				if err := save(cfg); err != nil {
					t.Fatal(err)
				}
				saved := readScope(t, path)
				for _, c := range serverUnknownCases {
					got, ok := jsonPath(saved, c.path...)
					if !ok {
						t.Fatalf("withKnown=%v: %s lost after save", withKnown, strings.Join(c.path, "."))
					}
					want, _ := json.Marshal(c.value)
					have, _ := json.Marshal(got)
					if string(want) != string(have) {
						t.Fatalf("%s changed: %s != %s", strings.Join(c.path, "."), have, want)
					}
				}
				// Repair is excluded: it already drops the known server and project values
				// of a valid file, with or without unknown keys (not part of this change).
				if withKnown && name != "Repair" {
					if v, _ := jsonPath(saved, "server", "url"); v != "http://127.0.0.1:7979" {
						t.Fatalf("server.url changed on save: %v", v)
					}
				}
				if _, err := Load(false); err != nil {
					t.Fatalf("saved file does not load: %v", err)
				}
			})
		}
	}
}

func TestOverlayServerAndProjectUnknownKeysMergeAndSurviveASave(t *testing.T) {
	setupHome(t)
	root := t.TempDir()
	main := initGitRepo(t, filepath.Join(root, "repo"))
	t.Chdir(main)
	scopePath := filepath.Join(root, "config.json")
	t.Setenv(EnvConfig, scopePath)
	writeJSONFile(t, scopePath, scopeDocument(t))
	overlayPath := filepath.Join(main, OverlayFilename)
	writeJSONFile(t, overlayPath, map[string]any{
		"server":  map[string]any{"url": "http://127.0.0.1:7979", "token": "t"},
		"project": map[string]any{"name": "demo", "extra": true},
	})
	cfg, err := Load(false)
	if err != nil {
		t.Fatalf("unknown overlay server/project keys must not fail the merge: %v", err)
	}
	if cfg.Server.URL != "http://127.0.0.1:7979" || cfg.Project.Name != "demo" {
		t.Fatalf("known overlay values stopped applying: %+v %+v", cfg.Server, cfg.Project)
	}
	sources := map[string]string{}
	for _, item := range cfg.Ignored {
		sources[item.PathString()] = item.Source
	}
	for _, p := range []string{"server.token", "project.extra"} {
		if sources[p] != overlayPath {
			t.Fatalf("%s must name the overlay as its source: %v", p, sources)
		}
	}
	// An overlay edit keeps the unknown keys.
	baseline, err := ReadOverlayFile(overlayPath)
	if err != nil {
		t.Fatal(err)
	}
	edited := CloneOverlay(baseline)
	OverlaySet(edited, "http://127.0.0.1:8080", "server", "url")
	if _, err := SaveOverlayIfUnchanged(overlayPath, edited, baseline); err != nil {
		t.Fatal(err)
	}
	saved := readScope(t, overlayPath)
	for _, p := range [][]string{{"server", "token"}, {"project", "extra"}, {"project", "name"}} {
		if _, ok := jsonPath(saved, p...); !ok {
			t.Fatalf("overlay save lost %v", p)
		}
	}
	if v, _ := jsonPath(saved, "server", "url"); v != "http://127.0.0.1:8080" {
		t.Fatalf("overlay edit not written: %v", v)
	}
}

func TestServerAndProjectKnownValuesStayChecked(t *testing.T) {
	setupHome(t)
	doc := documentWithServerUnknowns(t, false)
	OverlaySet(doc, "ftp://example.test", "server", "url")
	writeScope(t, doc)
	if _, err := Load(false); err == nil {
		t.Fatal("an invalid server.url was accepted beside an unknown key")
	}
	doc = documentWithServerUnknowns(t, false)
	OverlaySet(doc, "1bad name", "project", "name")
	writeScope(t, doc)
	if _, err := Load(false); err == nil {
		t.Fatal("an invalid project.name was accepted beside an unknown key")
	}
}
