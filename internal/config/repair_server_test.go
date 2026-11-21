package config

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
)

const (
	bindingURL     = "http://127.0.0.1:7979"
	bindingProject = "demo"
)

// bindingDocument is a valid, welcome-complete scope document that needs no repair.
func bindingDocument(t *testing.T) map[string]any {
	t.Helper()
	doc := scopeDocument(t)
	doc["server"] = map[string]any{"url": bindingURL}
	doc["project"] = map[string]any{"name": bindingProject}
	return doc
}

func assertBinding(t *testing.T, doc map[string]any, wantURL, wantProject string) {
	t.Helper()
	url, urlOK := jsonPath(doc, "server", "url")
	if wantURL == "" && urlOK || wantURL != "" && url != wantURL {
		t.Fatalf("server.url=%v (present=%v), want %q", url, urlOK, wantURL)
	}
	name, nameOK := jsonPath(doc, "project", "name")
	if wantProject == "" && nameOK || wantProject != "" && name != wantProject {
		t.Fatalf("project.name=%v (present=%v), want %q", name, nameOK, wantProject)
	}
}

func TestRepairKeepsValidBindingWithoutRewriting(t *testing.T) {
	setupHome(t)
	path := writeScope(t, bindingDocument(t))
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, result, err := Repair(nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || result.BackupPath != "" {
		t.Fatalf("a valid file must not be rewritten: %+v", result)
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(before, after) {
		t.Fatalf("file bytes changed: %v", err)
	}
	if cfg.Server.URL != bindingURL || cfg.Project.Name != bindingProject {
		t.Fatalf("returned config lost the binding: %+v %+v", cfg.Server, cfg.Project)
	}
}

func TestRepairKeepsBindingWhenAnotherValueNeedsRepair(t *testing.T) {
	setupHome(t)
	doc := bindingDocument(t)
	OverlaySet(doc, 999, "tui", "columns")
	path := writeScope(t, doc)
	cfg, result, err := Repair(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.BackupPath == "" {
		t.Fatalf("the invalid column count must be repaired with a backup: %+v", result)
	}
	if cfg.Server.URL != bindingURL || cfg.Project.Name != bindingProject {
		t.Fatalf("returned config lost the binding: %+v %+v", cfg.Server, cfg.Project)
	}
	assertBinding(t, readScope(t, path), bindingURL, bindingProject)
	loaded, err := Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Server.URL != bindingURL || loaded.Project.Name != bindingProject {
		t.Fatalf("loaded config lost the binding: %+v %+v", loaded.Server, loaded.Project)
	}
}

func TestRepairDropsOnlyTheInvalidBindingValue(t *testing.T) {
	cases := []struct {
		name                 string
		url, project         string
		wantURL, wantProject string
	}{
		{"invalid project name", bindingURL, "1bad", bindingURL, ""},
		{"invalid server url", "ftp://example.invalid", bindingProject, "", bindingProject},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupHome(t)
			doc := scopeDocument(t)
			doc["server"] = map[string]any{"url": c.url}
			doc["project"] = map[string]any{"name": c.project}
			path := writeScope(t, doc)
			cfg, result, err := Repair(nil)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Changed {
				t.Fatalf("the invalid value must be repaired: %+v", result)
			}
			if cfg.Server.URL != c.wantURL || cfg.Project.Name != c.wantProject {
				t.Fatalf("returned config: %+v %+v", cfg.Server, cfg.Project)
			}
			assertBinding(t, readScope(t, path), c.wantURL, c.wantProject)
			if _, err := Load(false); err != nil {
				t.Fatalf("repaired file does not load: %v", err)
			}
		})
	}
}

// omittedSectionSamples gives one valid, non-zero value for every top-level Config
// field whose JSON tag omits a zero value. A new such field fails the guard test
// below until it gets a sample here.
var omittedSectionSamples = map[string]any{
	"server":  map[string]any{"url": bindingURL},
	"project": map[string]any{"name": bindingProject},
}

// TestRepairKeepsEveryOmittableSection guards the encoded-defaults recovery of
// repairValues: a section that the default encoding omits is never restored by it
// unless Repair handles that section explicitly.
func TestRepairKeepsEveryOmittableSection(t *testing.T) {
	typ := reflect.TypeOf(Config{})
	checked := 0
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		// jsonName falls back to the Go field name, as encoding/json does for a tag like ",omitzero".
		name, ok := jsonName(field)
		_, options, _ := strings.Cut(field.Tag.Get("json"), ",")
		if !ok || (!strings.Contains(options, "omitzero") && !strings.Contains(options, "omitempty")) {
			continue
		}
		// agents has its own recovery path in repairValues and its own tests.
		if name == "agents" {
			continue
		}
		sample, ok := omittedSectionSamples[name]
		if !ok {
			t.Fatalf("Config.%s is omitted when empty: add a valid sample to omittedSectionSamples and keep Repair restoring it", field.Name)
		}
		checked++
		t.Run(name, func(t *testing.T) {
			setupHome(t)
			doc := scopeDocument(t)
			doc[name] = sample
			path := writeScope(t, doc)
			if _, _, err := Repair(nil); err != nil {
				t.Fatal(err)
			}
			got, ok := jsonPath(readScope(t, path), name)
			if !ok || !reflect.DeepEqual(got, sample) {
				t.Fatalf("%s=%v after Repair, want %v", name, got, sample)
			}
		})
	}
	if checked == 0 {
		t.Fatal("no omittable section found: the reflection rule no longer matches the Config tags")
	}
}
