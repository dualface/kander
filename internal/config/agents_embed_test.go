package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

func TestDefaultConfigJSONMatchesPinnedSnapshot(t *testing.T) {
	cfg := DefaultConfig()
	got, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/default-config.json")
	if err != nil {
		t.Fatal(err)
	}
	var pinned Config
	if err := json.Unmarshal(raw, &pinned); err != nil {
		t.Fatal(err)
	}
	pinned.Launcher = DefaultLauncher()
	expected, err := json.MarshalIndent(&pinned, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(expected) {
		t.Fatalf("default config JSON drifted\ngot:\n%s\nwant:\n%s", got, expected)
	}
}

func TestMinimalCursorFixtureJSONRoundTrip(t *testing.T) {
	raw := minimalPayload(nil)
	cfg, err := Validate(raw)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	// Captured from minimalPayload(nil) at 8abdfe2e, before the migration.
	want, err := os.ReadFile("testdata/minimal-cursor-config.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != strings.TrimSuffix(string(want), "\n") {
		t.Fatalf("fixture JSON differs from the pre-migration output\ngot:\n%s\nwant:\n%s", data, want)
	}
	again, err := ValidateJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.MarshalIndent(again, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(second) {
		t.Fatalf("fixture JSON is not stable\n%s\n%s", data, second)
	}
}

func TestEmbeddedDefinitionsLoadWithoutLookPath(t *testing.T) {
	t.Setenv("PATH", filepath.Join(t.TempDir(), "empty-path"))
	if AgentExecutableName("cursor") != "cursor-agent" {
		t.Fatal(AgentExecutableName("cursor"))
	}
	d := AgentFor(nil, "codex")
	if d.Session.Mode != "hook:codex-rollout" || d.PromptDelivery.Mode != "argv" {
		t.Fatalf("%+v", d)
	}
	data, err := json.MarshalIndent(DefaultConfig(), "", "  ")
	if err != nil || !strings.Contains(string(data), `"kanban_agent"`) {
		t.Fatalf("%s %v", data, err)
	}
}

func TestEmbeddedUnknownSessionHookNamesAgentAndHook(t *testing.T) {
	raw, err := embeddedAgentFS.ReadFile("agents/codex.json")
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), "hook:codex-rollout", "hook:missing-hook", 1))
	_, err = parseEmbeddedAgentFile("codex.json", raw)
	if err == nil || !strings.Contains(err.Error(), "codex") || !strings.Contains(err.Error(), "missing-hook") {
		t.Fatalf("error must name the agent and unknown hook: %v", err)
	}
}

func TestUserDiscoveredStillRejectedAndMissingSchemaIsOne(t *testing.T) {
	base, _ := json.Marshal(DefaultConfig())
	var root map[string]any
	json.Unmarshal(base, &root)
	root["agents"] = map[string]any{"claude": map[string]any{"session": map[string]any{"mode": "discovered"}}}
	data, _ := json.Marshal(root)
	if _, err := ValidateJSON(data); err == nil {
		t.Fatal("user discovered accepted")
	}
	root["agents"] = map[string]any{"helper": map[string]any{"dialect": "claude"}}
	data, _ = json.Marshal(root)
	got, err := ValidateJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agents["helper"].SchemaVersion != 0 {
		t.Fatalf("missing schema_version should stay omitted: %+v", got.Agents["helper"])
	}
}

func TestPromptDeliveryValidation(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	base, _ := json.Marshal(DefaultConfig())
	for _, test := range []struct {
		name string
		def  map[string]any
		bad  bool
	}{
		{"argv", map[string]any{"path": exe, "dialect": "claude", "prompt_delivery": map[string]any{"mode": "argv"}}, false},
		{"argv-ready", map[string]any{"path": exe, "dialect": "claude", "prompt_delivery": map[string]any{"mode": "argv", "ready": map[string]any{"match": "x", "timeout_ms": 1}}}, true},
		{"pane", map[string]any{"path": exe, "args": map[string]any{"start": []string{}, "resume": []string{}}, "session": map[string]any{"mode": "generated"}, "prompt_delivery": map[string]any{"mode": "pane", "ready": map[string]any{"match": "READY", "timeout_ms": 1000}}}, false},
		{"pane-regex", map[string]any{"path": exe, "args": map[string]any{"start": []string{}, "resume": []string{}}, "session": map[string]any{"mode": "generated"}, "prompt_delivery": map[string]any{"mode": "pane", "ready": map[string]any{"match": "regex:READY", "timeout_ms": 1000}, "blocked": []any{map[string]any{"match": "regex:TRUST", "reason": "trust"}}}}, false},
		{"pane-bad-regex", map[string]any{"path": exe, "args": map[string]any{"start": []string{}, "resume": []string{}}, "session": map[string]any{"mode": "generated"}, "prompt_delivery": map[string]any{"mode": "pane", "ready": map[string]any{"match": "regex:[", "timeout_ms": 1000}}}, true},
		{"pane-timeout", map[string]any{"path": exe, "args": map[string]any{"start": []string{}, "resume": []string{}}, "session": map[string]any{"mode": "generated"}, "prompt_delivery": map[string]any{"mode": "pane", "ready": map[string]any{"match": "READY", "timeout_ms": 0}}}, true},
		{"pane-reason", map[string]any{"path": exe, "args": map[string]any{"start": []string{}, "resume": []string{}}, "session": map[string]any{"mode": "generated"}, "prompt_delivery": map[string]any{"mode": "pane", "ready": map[string]any{"match": "READY", "timeout_ms": 1}, "blocked": []any{map[string]any{"match": "X", "reason": "bad\nline"}}}}, true},
		{"bad-mode", map[string]any{"path": exe, "dialect": "claude", "prompt_delivery": map[string]any{"mode": "stdin"}}, true},
		{"schema-2", map[string]any{"path": exe, "dialect": "claude", "schema_version": 2}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var root map[string]any
			json.Unmarshal(base, &root)
			root["agents"] = map[string]any{"helper": test.def}
			data, _ := json.Marshal(root)
			_, err := ValidateJSON(data)
			if (err != nil) != test.bad {
				t.Fatalf("err=%v", err)
			}
			if err != nil && !strings.Contains(err.Error(), "helper") {
				t.Fatalf("error missing agent name: %v", err)
			}
		})
	}
}

func TestExpandAgentArgsKeepsEmptySessionEquals(t *testing.T) {
	got := ExpandAgentArgs([]string{"--resume", "{session=}"}, "", "", "")
	if !reflect.DeepEqual(got, []string{"--resume", ""}) {
		t.Fatalf("%q", got)
	}
	dropped := RewriteKeepSession([]string{"--resume", "{session=}"}, true)
	got = ExpandAgentArgs(dropped, "", "", "")
	if !reflect.DeepEqual(got, []string{}) {
		t.Fatalf("%q", got)
	}
}

func TestEmbeddedUnknownSchemaRejected(t *testing.T) {
	_, _, err := loadEmbeddedAgentsFrom(fstest.MapFS{
		"index.json": {Data: []byte(`["demo"]`)},
		"demo.json": {Data: []byte(`{
  "schema_version": 99,
  "path": "demo",
  "process_name": "demo",
  "args": {"start": [], "resume": []},
  "session": {"mode": "generated"},
  "display_name": "Demo",
  "supports_effort": true,
  "prompt_delivery": {"mode": "argv"},
  "rules_target": {"global": ".demo/AGENTS.md", "project": "AGENTS.md"},
  "rules_integration": "markdown-reference",
  "large_model": "",
  "small_model": "",
  "large_effort": "high",
  "small_effort": "high",
  "model": "",
  "effort": "high"
}`)},
	})
	if err == nil || !strings.Contains(err.Error(), "demo.json") || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("err=%v", err)
	}
}

func TestEmbeddedInvalidStructureNamesFileAndField(t *testing.T) {
	data, err := embeddedAgentFS.ReadFile("agents/codex.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		field string
		value any
		want  string
	}{
		{"path", "", "path"},
		{"process_name", "bad\nname", "process_name"},
		{"args", map[string]any{"resume": []string{}}, "args.start"},
		{"session", map[string]any{"mode": "unknown"}, "session.mode"},
		{"prompt_delivery", map[string]any{"mode": "pane"}, "prompt_delivery.ready"},
		{"rules_integration", "shell", "rules_integration"},
	} {
		t.Run(test.field, func(t *testing.T) {
			var definition map[string]any
			if err := json.Unmarshal(data, &definition); err != nil {
				t.Fatal(err)
			}
			definition[test.field] = test.value
			invalid, err := json.Marshal(definition)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = loadEmbeddedAgentsFrom(fstest.MapFS{
				"index.json": {Data: []byte(`["codex"]`)},
				"codex.json": {Data: invalid},
			})
			if err == nil || !strings.Contains(err.Error(), "codex.json") || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("missing file or field: %v", err)
			}
		})
	}
}

func TestAgentSupportsEffortUsesDefinition(t *testing.T) {
	if AgentSupportsEffort(nil, "cursor") {
		t.Fatal("cursor should hide effort")
	}
	if !AgentSupportsEffort(nil, "claude") {
		t.Fatal("claude should show effort")
	}
	cfg := DefaultConfig()
	cfg.Agents = map[string]AgentDefinition{"cursor": {Args: &AgentArgs{Start: []string{"{effort}"}, Resume: []string{}}}}
	if !AgentSupportsEffort(cfg, "cursor") {
		t.Fatal("custom cursor args should show effort")
	}
	if ReviewModelSupportsEffort(cfg, "cursor") {
		t.Fatal("cursor review model has no effort key")
	}
	if !ReviewModelSupportsEffort(nil, "claude") || ReviewModelSupportsEffort(nil, "cursor") {
		t.Fatal("embedded review effort keys")
	}
}
