package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func TestAgentConfigDirValidation(t *testing.T) {
	for _, tt := range []struct {
		name, agent, value string
		ok                 bool
	}{
		{"absolute", "codex", filepath.Join(t.TempDir(), "codex"), true},
		{"tilde", "claude", "~", true},
		{"tilde prefix", "pi", "~/.config/pi", true},
		{"relative", "codex", "codex", false},
		{"control", "codex", "/tmp/a\nb", false},
		{"custom agent", "wrapper", "/tmp/wrapper", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fields := map[string]any{"config_dir": tt.value}
			if tt.agent == "wrapper" {
				fields["path"], fields["dialect"] = "sh", "codex"
			}
			_, err := validateAgentDefinitions(map[string]any{tt.agent: fields})
			if tt.ok != (err == nil) {
				t.Fatalf("err=%v want ok=%v", err, tt.ok)
			}
			if err != nil && !strings.Contains(err.Error(), tt.agent) {
				t.Fatalf("error does not name the agent: %v", err)
			}
		})
	}
}

func TestEmbeddedConfigHomeValidation(t *testing.T) {
	load := func(home map[string]any, global string) error {
		def := map[string]any{
			"schema_version":    1,
			"path":              "demo",
			"process_name":      "demo",
			"args":              map[string]any{"start": []string{}, "resume": []string{}, "review": []string{"-"}},
			"review":            map[string]any{"cwd": "root", "output_name": "output.txt", "inspection": "read-only", "output": map[string]any{"source": "stdout", "parse": "raw"}},
			"session":           map[string]any{"mode": "generated"},
			"display_name":      "Demo",
			"prompt_delivery":   map[string]any{"mode": "argv"},
			"rules_target":      map[string]any{"global": global, "project": "AGENTS.md"},
			"config_home":       home,
			"rules_integration": "markdown-reference",
		}
		data, _ := json.Marshal(def)
		_, _, err := loadEmbeddedAgentsFrom(fstest.MapFS{
			"index.json": &fstest.MapFile{Data: []byte(`["demo"]`)},
			"demo.json":  &fstest.MapFile{Data: data},
		})
		return err
	}
	if err := load(map[string]any{"default": ".demo", "env": []any{map[string]any{"name": "DEMO_HOME"}}}, ".demo/AGENTS.md"); err != nil {
		t.Fatalf("valid declaration rejected: %v", err)
	}
	for _, tt := range []struct {
		name   string
		home   map[string]any
		global string
		want   string
	}{
		{"missing default", map[string]any{"env": []any{}}, ".demo/AGENTS.md", "config_home.default"},
		{"absolute default", map[string]any{"default": "/demo"}, ".demo/AGENTS.md", "config_home.default"},
		{"bad env name", map[string]any{"default": ".demo", "env": []any{map[string]any{"name": "demo-home"}}}, ".demo/AGENTS.md", "config_home.env.name"},
		{"escaping join", map[string]any{"default": ".demo", "env": []any{map[string]any{"name": "XDG", "join": "../x"}}}, ".demo/AGENTS.md", "config_home.env.join"},
		{"global outside", map[string]any{"default": ".demo"}, ".other/AGENTS.md", "rules_target.global"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := load(tt.home, tt.global); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err=%v want %s", err, tt.want)
			}
		})
	}
}

func TestOverlayAgentConfigDirs(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)
	overlay := filepath.Join(work, OverlayFilename)
	data := `{"agents":{"pi":{"config_dir":"/tmp/pi"},"codex":{"path":"codex"},"claude":{"config_dir":"~/c"}}}`
	if err := os.WriteFile(overlay, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	file, names, err := OverlayAgentConfigDirs()
	if err != nil {
		t.Fatal(err)
	}
	if !sameFile(t, file, overlay) || !slices.Equal(names, []string{"claude", "pi"}) {
		t.Fatalf("file=%q names=%v", file, names)
	}
}

func sameFile(t *testing.T, a, b string) bool {
	t.Helper()
	ia, errA := os.Stat(a)
	ib, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(ia, ib)
}
