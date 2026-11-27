package config

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenameCommandValidation(t *testing.T) {
	data, err := embeddedAgentFS.ReadFile("agents/claude.json")
	if err != nil {
		t.Fatal(err)
	}
	var embedded map[string]any
	if err := json.Unmarshal(data, &embedded); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		value any
		ok    bool
	}{
		{"empty", "", true},
		{"template", "/rename {name}", true},
		{"missing-placeholder", "/rename title", false},
		{"newline", "/rename {name}\n/other", false},
		{"control", "/rename {name}\t", false},
		{"format", "/rename {name}\u202e", false},
		{"line-separator", "/rename {name}\u2028", false},
		{"not-string", true, false},
		{"null", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			embedded["rename_command"] = tc.value
			b, err := json.Marshal(embedded)
			if err != nil {
				t.Fatal(err)
			}
			_, embeddedErr := parseEmbeddedAgentFile("claude.json", b)
			_, userErr := Validate(minimalPayload(map[string]any{
				"agents": map[string]any{"claude": map[string]any{"rename_command": tc.value}},
			}))
			for kind, result := range map[string]error{"embedded": embeddedErr, "user": userErr} {
				if tc.ok && result != nil || !tc.ok && (result == nil || !strings.Contains(result.Error(), "rename_command")) {
					t.Errorf("%s rename_command=%#v: %v", kind, tc.value, result)
				}
			}
		})
	}
}

func TestRenameCommandUsesScopeAndProjectOverlays(t *testing.T) {
	temp := t.TempDir()
	scopePath := filepath.Join(temp, "config.json")
	t.Setenv(EnvConfig, scopePath)
	setCommand := func(value string) map[string]any {
		return map[string]any{"agents": map[string]any{"claude": map[string]any{"rename_command": value}}}
	}
	project := filepath.Join(temp, "project")
	overlay := filepath.Join(project, ".kander-config.json")
	for _, tc := range []struct {
		name    string
		scope   string
		project *string
		want    string
	}{
		{"scope-template", "/title {name}", nil, "/title {name}"},
		{"scope-disable", "", nil, ""},
		{"project-template", "/title {name}", testRenameText("/label {name}"), "/label {name}"},
		{"project-disable", "/title {name}", testRenameText(""), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeJSONFile(t, scopePath, minimalPayload(setCommand(tc.scope)))
			cfg, err := LoadScope(false)
			if err != nil {
				t.Fatal(err)
			}
			if tc.project != nil {
				writeJSONFile(t, overlay, setCommand(*tc.project))
				cfg, err = LoadFor(project)
				if err != nil {
					t.Fatal(err)
				}
			}
			d := AgentFor(cfg, "claude")
			if d.RenameCommand == nil || *d.RenameCommand != tc.want {
				t.Fatalf("resolved rename_command=%v, want %q", d.RenameCommand, tc.want)
			}
		})
	}
}

func TestRenameCommandAndDialogsInheritFromDialect(t *testing.T) {
	cfg := &Config{Agents: map[string]AgentDefinition{"wrapped": {Dialect: "claude"}}}
	def := AgentFor(cfg, "wrapped")
	if def.RenameCommand == nil || *def.RenameCommand != "/rename {name}" || def.RenameDelivery == nil ||
		len(def.RenameDelivery.Blocked) == 0 || def.RenameDelivery.Complete == "" {
		t.Fatalf("incomplete inherited rename configuration: %+v", def)
	}
	*def.RenameCommand, def.RenameDelivery.Blocked[0].Match = "modified", "modified"
	got := AgentFor(cfg, "wrapped")
	if *got.RenameCommand == "modified" || got.RenameDelivery.Blocked[0].Match == "modified" {
		t.Fatal("resolved definition modified embedded defaults")
	}
}

// Readiness for a rename comes from the agent state a terminal reports, so the
// built-in definitions carry no composer text rule, while a user definition
// that still has one keeps loading.
func TestRenameReadyRuleIsOnlyAccepted(t *testing.T) {
	for _, name := range []string{"claude", "codex", "cursor"} {
		def, ok := embeddedByName(name)
		if !ok || def.RenameCommand == "" || def.RenameDelivery == nil {
			t.Fatalf("%s: built-in rename configuration missing", name)
		}
		if def.RenameDelivery.Ready != nil {
			t.Errorf("%s: built-in rename_delivery.ready = %+v", name, *def.RenameDelivery.Ready)
		}
	}
	_, err := Validate(minimalPayload(map[string]any{"agents": map[string]any{"claude": map[string]any{
		"rename_delivery": map[string]any{"ready": map[string]any{"match": "regex:^> $", "timeout_ms": 5000}},
	}}}))
	if err != nil {
		t.Fatalf("user rename_delivery.ready refused: %v", err)
	}
}

func testRenameText(value string) *string { return &value }
