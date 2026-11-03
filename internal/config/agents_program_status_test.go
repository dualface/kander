package config

import (
	"strings"
	"testing"
)

// Every built-in agent declares OSC 7501; only agents whose report was
// observed in herdr-core declare that they send it.
func TestEmbeddedAgentsDeclareProgramStatus(t *testing.T) {
	want := map[string]ProgramStatus{
		"claude":   {Reports: true, App: "claude-code"},
		"codex":    {Reports: true, App: "codex"},
		"cursor":   {Reports: false},
		"pi":       {Reports: true, App: "pi"},
		"grok":     {Reports: true, App: "grok-build"},
		"kimi":     {Reports: true, App: "kimi-code"},
		"devin":    {Reports: true, App: "devin"},
		"opencode": {Reports: false},
		"droid":    {Reports: false},
	}
	for _, name := range ExecutionAgents {
		got := AgentFor(nil, name).ProgramStatus
		expected, listed := want[name]
		if got == nil || !listed || *got != expected {
			t.Fatalf("%s program_status=%+v, want %+v (listed %v)", name, got, expected, listed)
		}
	}
}

func TestCustomAgentProgramStatus(t *testing.T) {
	definitions, err := validateAgentDefinitions(map[string]any{
		"claude-work": map[string]any{"dialect": "claude", "program_status": map[string]any{"reports": true, "app": "claude-work"}},
		"claude-wrap": map[string]any{"dialect": "claude"},
		"cursor":      map[string]any{"program_status": map[string]any{"reports": true, "app": "cursor-agent"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &Config{Agents: definitions}
	for name, app := range map[string]string{"claude-work": "claude-work", "claude-wrap": "claude-code", "cursor": "cursor-agent"} {
		if got, ok := ProgramStatusApp(AgentFor(cfg, name)); !ok || got != app {
			t.Fatalf("%s app=%q ok=%v, want %q", name, got, ok, app)
		}
	}
	for _, invalid := range []map[string]any{
		{"reports": true},
		{"reports": true, "app": " "},
		{"reports": false, "app": "x"},
		{"reports": true, "app": "x", "kind": "y"},
	} {
		_, err := validateAgentDefinitions(map[string]any{"claude": map[string]any{"program_status": invalid}})
		if err == nil || !strings.Contains(err.Error(), "program_status") && !strings.Contains(err.Error(), `unknown field "kind"`) {
			t.Fatalf("program_status %v accepted: %v", invalid, err)
		}
	}
	if _, err := validateAgentDefinitions(map[string]any{"claude": map[string]any{"program_status": "yes"}}); err == nil {
		t.Fatal("non-object program_status accepted")
	}
}
