package launch

import (
	"reflect"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestCodexStructuredTransportBuildsExecArguments(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents = map[string]config.AgentDefinition{"codex": {Transport: "structured"}}

	got, err := agentArguments("codex", cfg.Models.Kanban["codex"], "small", AgentSession{Agent: "codex"}, false, cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"exec", "--json", "--model", "gpt-5.6-sol", "--config", `model_reasoning_effort="medium"`,
		"--dangerously-bypass-approvals-and-sandbox", "-",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("structured start args = %q, want %q", got, want)
	}
}

func TestCodexStructuredTransportBuildsResumeArguments(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents = map[string]config.AgentDefinition{"codex": {Transport: "structured"}}

	got, err := agentArguments("codex", cfg.Models.Kanban["codex"], "large", AgentSession{Agent: "codex", Reference: "session-123"}, true, cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"exec", "resume", "--json", "--model", "gpt-5.6-sol", "--config", `model_reasoning_effort="high"`,
		"--dangerously-bypass-approvals-and-sandbox", "session-123", "-",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("structured resume args = %q, want %q", got, want)
	}
}

func TestCodexDefaultTransportKeepsInteractiveArguments(t *testing.T) {
	cfg := config.DefaultConfig()
	got, err := agentArguments("codex", cfg.Models.Kanban["codex"], "small", AgentSession{Agent: "codex", Reference: "session-123"}, true, cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"resume", "--model", "gpt-5.6-sol", "--config", `model_reasoning_effort="medium"`,
		"--dangerously-bypass-approvals-and-sandbox", "session-123",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default resume args = %q, want %q", got, want)
	}
}
