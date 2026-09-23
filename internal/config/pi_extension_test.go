package config

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func TestPiDeclaresRulesExtension(t *testing.T) {
	spec, data, ok := AgentExtension("pi")
	if !ok {
		t.Fatal("pi must declare a rules extension")
	}
	if spec.Source != "kander-rules.ts" ||
		spec.Global != ".pi/agent/extensions/kander-rules.ts" ||
		spec.Project != ".pi/extensions/kander-rules.ts" {
		t.Fatalf("spec=%+v", spec)
	}
	if len(data) == 0 || !strings.Contains(string(data), "kander-extension-version") {
		t.Fatalf("extension payload not embedded: %d bytes", len(data))
	}
	for _, marker := range []string{"KANDER-AGENTS.md", "KANDER-BASE-RULES.md", "KANDER-LOADING-RULES.md", "kander config --json", "PI_KANDER_RULES", "kander-rules"} {
		if !strings.Contains(string(data), marker) {
			t.Fatalf("extension payload misses %q", marker)
		}
	}
}

func TestOtherAgentsDeclareNoRulesExtension(t *testing.T) {
	for _, name := range ExecutionAgents {
		if name == "pi" {
			continue
		}
		if spec, data, ok := AgentExtension(name); ok || data != nil {
			t.Fatalf("%s unexpectedly declares an extension: %+v", name, spec)
		}
	}
	if _, _, ok := AgentExtension("no-such-agent"); ok {
		t.Fatal("unknown agent must not resolve an extension")
	}
}

// The extension field lives only on embedded definitions: a user agents.<name>
// overlay can neither declare nor remove it, so a full pi overlay leaves the
// embedded extension contract untouched.
func TestAgentExtensionSurvivesUserOverlay(t *testing.T) {
	spec, _, ok := AgentExtension("pi")
	if !ok {
		t.Fatal("pi extension missing")
	}
	cfg := &Config{Agents: map[string]AgentDefinition{
		"pi": {Path: "pi", Args: &AgentArgs{Start: []string{"--x"}, Resume: []string{"--y"}}},
	}}
	if got := AgentFor(cfg, "pi"); got.Args == nil {
		t.Fatal("overlay not applied")
	}
	again, data, againOK := AgentExtension("pi")
	if !againOK || again != spec || len(data) == 0 {
		t.Fatalf("overlay changed the extension spec: %+v %v", again, againOK)
	}
	if _, err := validateAgentDefinitions(map[string]any{
		"pi": map[string]any{"rules_extension": map[string]any{"source": "x.ts", "global": "g", "project": "p"}},
	}); err == nil {
		t.Fatal("a user overlay must not declare rules_extension")
	}
}

func TestPiReviewArgsAllowDefaultToolsAndResources(t *testing.T) {
	d := AgentFor(nil, "pi")
	if d.Args == nil {
		t.Fatal("pi args missing")
	}
	for _, flag := range []string{"--tools", "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-themes"} {
		if slices.Contains(d.Args.Review, flag) {
			t.Fatalf("pi review args must not restrict default tools or resources with %s: %v", flag, d.Args.Review)
		}
	}
}

func TestEmbeddedRulesExtensionValidation(t *testing.T) {
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
	mk := func(ext map[string]any, files fstest.MapFS) error {
		def := map[string]any{}
		for k, v := range base {
			def[k] = v
		}
		if ext != nil {
			def["rules_extension"] = ext
		}
		data, _ := json.Marshal(def)
		files["index.json"] = &fstest.MapFile{Data: []byte(`["demo"]`)}
		files["demo.json"] = &fstest.MapFile{Data: data}
		_, _, err := loadEmbeddedAgentsFrom(files)
		return err
	}
	source := &fstest.MapFile{Data: []byte("// extension\n")}
	valid := map[string]any{"source": "x.ts", "global": ".demo/extensions/x.ts", "project": ".demo/extensions/x.ts"}
	if err := mk(valid, fstest.MapFS{"agents/extensions/x.ts": source}); err != nil {
		t.Fatalf("valid extension rejected: %v", err)
	}
	for _, tc := range []struct {
		name string
		ext  map[string]any
		want string
	}{
		{"bad source", map[string]any{"source": "a/b.ts", "global": "g", "project": "p"}, "rules_extension.source"},
		{"empty global", map[string]any{"source": "x.ts", "global": "", "project": "p"}, "rules_extension.global"},
		{"empty project", map[string]any{"source": "x.ts", "global": "g", "project": ""}, "rules_extension.project"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := mk(tc.ext, fstest.MapFS{"agents/extensions/x.ts": source})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want %s", err, tc.want)
			}
		})
	}
	t.Run("missing source file", func(t *testing.T) {
		err := mk(valid, fstest.MapFS{})
		if err == nil || !strings.Contains(err.Error(), "x.ts") {
			t.Fatalf("dangling source accepted: %v", err)
		}
	})
}
