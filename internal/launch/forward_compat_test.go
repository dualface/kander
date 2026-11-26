package launch

import (
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

// A role that names an agent this version cannot use fails when the role is used,
// with a message that names the agent and says why.
func TestUnavailableAgentsFailOnUse(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.KanbanAgents["large"] = "newagent"
	cfg.ChatAgent = "newagent"
	cfg.UnusableAgents = map[string]string{"futureagent": "session.mode \"future\" is not registered in this version"}

	if _, err := requireAgentProgram("newagent", cfg); err == nil || !strings.Contains(err.Error(), "newagent") ||
		err.Error() != config.UnavailableAgentError(cfg, "newagent").Error() {
		t.Fatalf("an unknown agent reference: %v", err)
	}
	if _, _, err := sessionDefaults(cfg, "large", "", ""); err == nil || !strings.Contains(err.Error(), "newagent") {
		t.Fatalf("start defaults for an unknown agent: %v", err)
	}
	if _, _, err := chatDefaults(cfg); err == nil || !strings.Contains(err.Error(), "newagent") {
		t.Fatalf("chat with an unknown Chat Agent: %v", err)
	}
	_, err := requireAgentProgram("futureagent", cfg)
	if err == nil || !strings.Contains(err.Error(), "futureagent") || !strings.Contains(err.Error(), "future") {
		t.Fatalf("an unusable agent must be reported with its reason: %v", err)
	}
	if _, _, err := sessionDefaults(cfg, "small", "futureagent", ""); err == nil || !strings.Contains(err.Error(), "futureagent") {
		t.Fatalf("an explicit unusable agent: %v", err)
	}
}

// Each role is judged on its own: an unusable large-scale agent does not block a
// usable Chat Agent, and the other way round. Start-side preflights refuse first.
func TestChatAndStartPreflightJudgeTheirOwnAgent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.KanbanAgents["large"] = "newagent"
	cfg.ChatAgent = "codex"
	if _, _, err := chatDefaults(cfg); err != nil && strings.Contains(err.Error(), "newagent") {
		t.Fatalf("an unknown large-scale agent blocked the Chat Agent: %v", err)
	}
	cfg.KanbanAgents["large"] = "codex"
	cfg.ChatAgent = "newagent"
	if _, _, err := chatDefaults(cfg); err == nil || !strings.Contains(err.Error(), "newagent") {
		t.Fatalf("an unknown Chat Agent must be refused: %v", err)
	}
	if err := programStatusPreflight(cfg, "foreground", "newagent"); err == nil || !strings.Contains(err.Error(), "newagent") {
		t.Fatalf("the shared preflight must refuse an unknown agent first: %v", err)
	}
}
