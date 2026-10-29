package config

import (
	"path/filepath"
	"testing"
)

func TestCodexStructuredTransportOverlayIsAccepted(t *testing.T) {
	definitions, err := validateAgentDefinitions(map[string]any{
		"codex": map[string]any{"transport": "structured"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := AgentFor(&Config{Agents: definitions}, "codex").Transport; got != "structured" {
		t.Fatalf("transport = %q, want structured", got)
	}
}

func TestCodexStructuredTransportRejectsForeignDialect(t *testing.T) {
	if _, err := validateAgentDefinitions(map[string]any{
		"codex": map[string]any{"transport": "structured", "dialect": "claude"},
	}); err == nil {
		t.Fatal("structured transport accepted a non-Codex dialect")
	}
}

func TestCodexStructuredTransportRejectsIncompatibleAdapterFields(t *testing.T) {
	for _, test := range []struct {
		name   string
		fields map[string]any
	}{
		{
			name: "custom args",
			fields: map[string]any{
				"args": map[string]any{"start": []string{"ignored"}, "resume": []string{"ignored", "{session}"}},
			},
		},
		{
			name:   "generated session",
			fields: map[string]any{"session": map[string]any{"mode": "generated"}},
		},
		{
			name:   "session disabled",
			fields: map[string]any{"session": map[string]any{"mode": "none"}},
		},
		{
			name: "pane delivery",
			fields: map[string]any{
				"prompt_delivery": map[string]any{
					"mode":  "pane",
					"ready": map[string]any{"match": "ready", "timeout_ms": 1000},
				},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition := map[string]any{"transport": "structured"}
			for key, value := range test.fields {
				definition[key] = value
			}
			if _, err := validateAgentDefinitions(map[string]any{"codex": definition}); err == nil {
				t.Fatalf("structured transport accepted %s", test.name)
			}
		})
	}
}

func TestCodexStructuredTransportAcceptsCompatibleDefaults(t *testing.T) {
	definitions, err := validateAgentDefinitions(map[string]any{
		"codex": map[string]any{
			"transport":       "structured",
			"session":         map[string]any{"mode": "hook:codex-rollout"},
			"prompt_delivery": map[string]any{"mode": "argv"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved := AgentFor(&Config{Agents: definitions}, "codex")
	if resolved.Session.Mode != "hook:codex-rollout" || resolved.PromptDelivery.Mode != "argv" {
		t.Fatalf("resolved adapter = %+v", resolved)
	}
}

func TestCodexInteractiveTransportKeepsCustomAdapterFields(t *testing.T) {
	if _, err := validateAgentDefinitions(map[string]any{
		"codex": map[string]any{
			"args":    map[string]any{"start": []string{"custom"}, "resume": []string{"resume", "{session}"}},
			"session": map[string]any{"mode": "generated"},
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCodexStructuredTransportValidatesMergedAdapter(t *testing.T) {
	customArgs := map[string]any{
		"args":    map[string]any{"start": []string{"ignored"}, "resume": []string{"ignored", "{session}"}},
		"session": map[string]any{"mode": "generated"},
	}
	for _, test := range []struct {
		name         string
		scopeAgent   map[string]any
		projectAgent map[string]any
	}{
		{
			name:         "scope transport project adapter",
			scopeAgent:   map[string]any{"transport": "structured"},
			projectAgent: customArgs,
		},
		{
			name:         "scope adapter project transport",
			scopeAgent:   customArgs,
			projectAgent: map[string]any{"transport": "structured"},
		},
		{
			name:       "scope transport project pane delivery",
			scopeAgent: map[string]any{"transport": "structured"},
			projectAgent: map[string]any{"prompt_delivery": map[string]any{
				"mode":  "pane",
				"ready": map[string]any{"match": "ready", "timeout_ms": 1000},
			}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			scope := minimalPayload(map[string]any{
				"agents": map[string]any{"codex": test.scopeAgent},
			})
			overlay := map[string]any{
				"agents": map[string]any{"codex": test.projectAgent},
			}
			if _, err := MergeOverlayOnRaw(scope, overlay); err == nil {
				t.Fatal("incompatible merged structured adapter was accepted")
			}
		})
	}
}

func TestLoadRejectsIncompatibleStructuredAdapterAcrossScopeAndProject(t *testing.T) {
	setupHome(t)
	root := t.TempDir()
	project := initGitRepo(t, filepath.Join(root, "repo"))
	t.Chdir(project)
	scopePath := filepath.Join(root, "config.json")
	t.Setenv(EnvConfig, scopePath)
	writeJSONFile(t, scopePath, minimalPayload(map[string]any{
		"agents": map[string]any{
			"codex": map[string]any{"transport": "structured"},
		},
	}))
	writeJSONFile(t, filepath.Join(project, OverlayFilename), map[string]any{
		"agents": map[string]any{
			"codex": map[string]any{
				"args": map[string]any{
					"start":  []string{"ignored"},
					"resume": []string{"ignored", "{session}"},
				},
				"session": map[string]any{"mode": "generated"},
			},
		},
	})

	if _, err := Load(false); err == nil {
		t.Fatal("runtime load accepted incompatible scope/project structured adapter")
	}
}

func TestAgentTransportRejectsUnsupportedValues(t *testing.T) {
	for _, test := range []struct {
		name      string
		transport string
	}{
		{name: "non-codex", transport: "structured"},
		{name: "codex-invalid", transport: "other"},
	} {
		t.Run(test.name, func(t *testing.T) {
			agent := "codex"
			if test.name == "non-codex" {
				agent = "claude"
			}
			if _, err := validateAgentDefinitions(map[string]any{
				agent: map[string]any{"transport": test.transport},
			}); err == nil {
				t.Fatal("expected invalid transport to be rejected")
			}
		})
	}
}
