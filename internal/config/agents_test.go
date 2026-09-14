package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestAgentDefinitionFallbacksAndClone(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Agents = map[string]AgentDefinition{"claude": {Path: filepath.Join(string(filepath.Separator), "bin", "wrapper"), ProcessName: "node"}, "cursor": {Path: "renamed"}, "fresh": {Dialect: "claude"}}
	for _, test := range []struct{ name, path, process, dialect, mode string }{
		{"claude", cfg.Agents["claude"].Path, "node", "claude", "generated"},
		{"cursor", "renamed", "renamed", "cursor", "hook:cursor-create-chat"},
		{"codex", "codex", "codex", "codex", "hook:codex-rollout"},
		{"fresh", "fresh", "fresh", "claude", "generated"},
		{"unregistered", "unregistered", "unregistered", "", "generated"},
	} {
		d := AgentFor(cfg, test.name)
		if d.Path != test.path || d.ProcessName != test.process || d.Dialect != test.dialect || d.Session.Mode != test.mode {
			t.Fatalf("%s: %+v", test.name, d)
		}
	}
	cfg.Agents["fresh"] = AgentDefinition{Args: &AgentArgs{Start: []string{"{model}"}, Resume: []string{}}, Session: &AgentSessionDefinition{Mode: "allocated", Allocate: []string{"allocator"}}}
	clone := Clone(cfg)
	d := clone.Agents["fresh"]
	d.Args.Start[0] = "changed"
	d.Session.Allocate[0] = "changed"
	if cfg.Agents["fresh"].Args.Start[0] != "{model}" || cfg.Agents["fresh"].Session.Allocate[0] != "allocator" {
		t.Fatal("shallow clone")
	}
	raw, _ := json.Marshal(DefaultConfig())
	if strings.Contains(string(raw), `"agents"`) {
		t.Fatal("changed legacy JSON")
	}
}

func TestAgentDefinitionValidation(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	base, _ := json.Marshal(DefaultConfig())
	for _, test := range []struct {
		name       string
		definition map[string]any
		bad        bool
	}{
		{"path", map[string]any{"path": executable}, false},
		{"trampoline", map[string]any{"process_name": "node"}, false},
		{"empty-path", map[string]any{"path": ""}, true},
		{"relative", map[string]any{"path": "./wrapper"}, true},
		{"missing", map[string]any{"path": filepath.Join(t.TempDir(), "missing")}, true},
		{"control", map[string]any{"path": "bad\tvalue"}, true},
		{"process-control", map[string]any{"process_name": "node\x7f"}, true},
		{"process-empty", map[string]any{"process_name": ""}, true},
		{"unknown", map[string]any{"typo": "x"}, true},
		{"dialect", map[string]any{"dialect": "other"}, true},
		{"null-args", map[string]any{"args": nil}, true},
		{"bad-args", map[string]any{"args": map[string]any{"start": []string{}, "typo": true}}, true},
		{"missing-start", map[string]any{"args": map[string]any{"resume": []string{}}}, true},
		{"missing-resume", map[string]any{"args": map[string]any{"start": []string{}}}, true},
		{"empty-arrays", map[string]any{"args": map[string]any{"start": []string{}, "resume": []string{}}}, false},
		{"placeholder", map[string]any{"args": map[string]any{"start": []string{"{prompt}"}, "resume": []string{}}}, true},
		{"generated", map[string]any{"session": map[string]any{"mode": "generated"}}, false},
		{"none", map[string]any{"session": map[string]any{"mode": "none"}, "args": map[string]any{"start": []string{}}}, false},
		{"discovered", map[string]any{"session": map[string]any{"mode": "discovered"}}, true},
		{"hook-codex", map[string]any{"args": map[string]any{"start": []string{}, "resume": []string{}}, "session": map[string]any{"mode": "hook:codex-rollout"}}, false},
		{"hook-cursor", map[string]any{"args": map[string]any{"start": []string{}, "resume": []string{}}, "session": map[string]any{"mode": "hook:cursor-create-chat"}}, false},
		{"hook-unknown", map[string]any{"args": map[string]any{"start": []string{}, "resume": []string{}}, "session": map[string]any{"mode": "hook:not-registered"}}, true},
		{"exit-ok", map[string]any{"exit_command": "/bye"}, false},
		{"exit-empty", map[string]any{"exit_command": ""}, false},
		{"exit-multiline", map[string]any{"exit_command": "/bye\n/extra"}, true},
		{"exit-control", map[string]any{"exit_command": "/bye\t"}, true},
		{"unknown-session", map[string]any{"session": map[string]any{"mode": "generated", "typo": 1}}, true},
		{"allocate", map[string]any{"session": map[string]any{"mode": "allocated", "allocate": []string{executable, "allocate"}, "json_field": "id"}}, false},
		{"bad-allocate", map[string]any{"session": map[string]any{"mode": "allocated", "allocate": []string{executable, "bad\narg"}}}, true},
		{"unneeded-allocate", map[string]any{"session": map[string]any{"mode": "generated", "allocate": []string{executable}}}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var root map[string]any
			json.Unmarshal(base, &root)
			root["agents"] = map[string]any{"claude": test.definition}
			data, _ := json.Marshal(root)
			got, err := ValidateJSON(data)
			if (err != nil) != test.bad {
				t.Fatalf("config=%+v err=%v", got, err)
			}
		})
	}
	if runtime.GOOS != "windows" {
		p := filepath.Join(t.TempDir(), "file")
		os.WriteFile(p, []byte("x"), 0600)
		if validateAgentProgram(p) {
			t.Fatal("non executable")
		}
	}
}

func TestCustomAgentsModelsAndRepair(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Agents = map[string]AgentDefinition{"helper": {Dialect: "claude"}, "plain": {Args: &AgentArgs{Start: []string{}}, Session: &AgentSessionDefinition{Mode: "none"}}}
	cfg.KanbanAgent = "helper"
	cfg.KanbanAgents["small"] = "plain"
	cfg.KanbanAgents["large"] = "helper"
	data, _ := json.Marshal(cfg)
	got, err := ValidateJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Models.Kanban["helper"]["large_model"] != "opus" || got.Models.Kanban["plain"] == nil {
		t.Fatal(got.Models)
	}
	if len(AgentWarnings(got)) != 1 {
		t.Fatal(AgentWarnings(got))
	}
	raw, _ := decodeJSON(data)
	repaired, err := repairValues(raw)
	if err != nil || !reflect.DeepEqual(repaired.Agents, got.Agents) || repaired.KanbanAgents["small"] != "plain" {
		t.Fatalf("%+v %v", repaired, err)
	}
	root := raw.(map[string]any)
	root["reviewers"].(map[string]any)["PM"] = "helper"
	if _, err := Validate(root); err == nil {
		t.Fatal("dialect wrapper accepted as reviewer")
	}
	root["reviewers"].(map[string]any)["PM"] = "plain"
	if _, err := Validate(root); err == nil {
		t.Fatal("start-only reviewer accepted")
	}
	cfg.Agents["plain"] = AgentDefinition{}
	data, _ = json.Marshal(cfg)
	if _, err := ValidateJSON(data); err == nil {
		t.Fatal("missing adapter accepted")
	}
}

func TestExpandAgentArgs(t *testing.T) {
	for _, test := range []struct {
		name                   string
		args                   []string
		model, effort, session string
		want                   []string
	}{
		{"text", []string{"--model", "prefix-{model}", "--config", `effort="{effort}"`, "{session}"}, "model", "high", "id", []string{"--model", "prefix-model", "--config", `effort="high"`, "id"}},
		{"empty", []string{"start", "--model", "{model}", "--effort", "{effort}", "--resume", "{session}"}, "", "", "", []string{"start"}},
		{"adjacent-only", []string{"--flag", "literal", "{model}"}, "", "", "", []string{"--flag", "literal"}},
		{"mixed", []string{"--both", "{model}/{effort}"}, "model", "", "", []string{}},
		{"inline", []string{"--model={model}", "keep"}, "", "", "", []string{"keep"}},
		{"literal-injection", []string{"--model", "{model}", "{effort}"}, "$(touch /tmp/no); ' \" {effort}", "high", "", []string{"--model", "$(touch /tmp/no); ' \" {effort}", "high"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := ExpandAgentArgs(test.args, test.model, test.effort, test.session)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("%q != %q", got, test.want)
			}
		})
	}
}

func TestUnregisteredSessionHookNamesAgentAndHook(t *testing.T) {
	base, _ := json.Marshal(DefaultConfig())
	var root map[string]any
	json.Unmarshal(base, &root)
	root["agents"] = map[string]any{"helper": map[string]any{
		"args":    map[string]any{"start": []string{}, "resume": []string{}},
		"session": map[string]any{"mode": "hook:missing-hook"},
	}}
	data, _ := json.Marshal(root)
	_, err := ValidateJSON(data)
	if err == nil || !strings.Contains(err.Error(), "helper") || !strings.Contains(err.Error(), "missing-hook") {
		t.Fatalf("err=%v", err)
	}
}

func TestDialectSessionCompatibility(t *testing.T) {
	for _, test := range []struct {
		dialect, mode string
		bad           bool
	}{
		{"codex", "generated", true}, {"codex", "allocated", true}, {"cursor", "generated", true},
		{"cursor", "allocated", false}, {"claude", "allocated", false},
		{"codex", "none", false}, {"cursor", "none", false}, {"claude", "generated", false},
		{"codex", "hook:codex-rollout", false}, {"cursor", "hook:cursor-create-chat", false},
	} {
		t.Run(test.dialect+"-"+test.mode, func(t *testing.T) {
			cfg := DefaultConfig()
			exe, _ := os.Executable()
			session := &AgentSessionDefinition{Mode: test.mode}
			if test.mode == "allocated" {
				session.Allocate = []string{exe}
			}
			cfg.Agents = map[string]AgentDefinition{"alias": {Dialect: test.dialect, Session: session}}
			data, _ := json.Marshal(cfg)
			_, err := ValidateJSON(data)
			if (err != nil) != test.bad {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestLoadAgentRequiresCompleteConfig(t *testing.T) {
	setupHome(t)
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvConfig, path)
	if _, err := LoadAgent("codex"); err == nil {
		t.Fatal("missing config must fail")
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAgent("codex"); err == nil {
		t.Fatal("invalid JSON must fail")
	}
	if err := os.WriteFile(path, []byte(`{"language":"xx"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAgent("codex"); err == nil {
		t.Fatal("schema-invalid config must fail")
	}
	cfg := DefaultConfig()
	cfg.WelcomeComplete = true
	if _, err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	got, err := LoadAgent("codex")
	if err != nil {
		t.Fatal(err)
	}
	if got.Dialect != "codex" {
		t.Fatalf("%+v", got)
	}
}
