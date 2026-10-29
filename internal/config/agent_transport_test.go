package config

import "testing"

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
