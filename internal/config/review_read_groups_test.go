package config

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func TestCodexDeclaresReviewWindowsReadGroups(t *testing.T) {
	if got := ReviewWindowsReadGroups("codex"); !slices.Equal(got, []string{"CodexSandboxUsers"}) {
		t.Fatalf("codex groups = %v", got)
	}
	for _, name := range ExecutionAgents {
		if name == "codex" {
			continue
		}
		if got := ReviewWindowsReadGroups(name); len(got) != 0 {
			t.Fatalf("%s unexpectedly declares review read groups: %v", name, got)
		}
	}
	if got := ReviewWindowsReadGroups("no-such-agent"); got != nil {
		t.Fatalf("unknown agent resolved groups: %v", got)
	}
}

// The groups live only on embedded definitions: a user or project agents.<name> overlay can neither declare nor
// change them, so an untrusted project configuration cannot widen access to review inputs.
func TestReviewWindowsReadGroupsSurviveUserOverlay(t *testing.T) {
	cfg := &Config{Agents: map[string]AgentDefinition{
		"codex": {Path: "codex", Args: &AgentArgs{Start: []string{"--x"}, Resume: []string{"--y"}}},
	}}
	if got := AgentFor(cfg, "codex"); got.Args == nil {
		t.Fatal("overlay not applied")
	}
	if got := ReviewWindowsReadGroups("codex"); !slices.Equal(got, []string{"CodexSandboxUsers"}) {
		t.Fatalf("overlay changed the groups: %v", got)
	}
	for _, overlay := range []map[string]any{
		{"review_windows_read_groups": []string{"Users"}},
		{"review": map[string]any{"review_windows_read_groups": []string{"Users"}}},
		{"review": map[string]any{"windows_read_groups": []string{"Users"}}},
	} {
		if _, err := validateAgentDefinitions(map[string]any{"codex": overlay}); err == nil {
			t.Fatalf("a user overlay must not declare review read groups: %v", overlay)
		}
	}
	returned := ReviewWindowsReadGroups("codex")
	returned[0] = "Users"
	if got := ReviewWindowsReadGroups("codex"); got[0] != "CodexSandboxUsers" {
		t.Fatalf("caller mutation leaked into the embedded definition: %v", got)
	}
}

func TestEmbeddedReviewWindowsReadGroupsValidation(t *testing.T) {
	base := map[string]any{
		"schema_version": 1,
		"path":           "demo",
		"process_name":   "demo",
		"args":           map[string]any{"start": []string{}, "resume": []string{}, "review": []string{"-"}},
		"review": map[string]any{
			"cwd":         "root",
			"output_name": "output.txt",
			"inspection":  "read-only",
			"output":      map[string]any{"source": "stdout", "parse": "raw"},
		},
		"session":           map[string]any{"mode": "generated"},
		"display_name":      "Demo",
		"supports_effort":   true,
		"prompt_delivery":   map[string]any{"mode": "argv"},
		"rules_target":      map[string]any{"global": ".demo/AGENTS.md", "project": "AGENTS.md"},
		"rules_integration": "markdown-reference",
	}
	mk := func(groups []string) error {
		def := map[string]any{}
		for k, v := range base {
			def[k] = v
		}
		def["review_windows_read_groups"] = groups
		data, _ := json.Marshal(def)
		files := fstest.MapFS{
			"index.json": &fstest.MapFile{Data: []byte(`["demo"]`)},
			"demo.json":  &fstest.MapFile{Data: data},
		}
		_, _, err := loadEmbeddedAgentsFrom(files)
		return err
	}
	if err := mk([]string{"DemoSandboxUsers"}); err != nil {
		t.Fatalf("valid groups rejected: %v", err)
	}
	for _, groups := range [][]string{
		{""},
		{`BUILTIN\Users`},
		{"a/b"},
		{"user@domain"},
		{"line\nbreak"},
		{"Demo", "demo"},
	} {
		err := mk(groups)
		if err == nil || !strings.Contains(err.Error(), "review_windows_read_groups") {
			t.Fatalf("groups %q: err=%v", groups, err)
		}
	}
}
