package menu

import (
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestSessionKeepsStoredAgentLanguage(t *testing.T) {
	existing := config.DefaultConfig()
	existing.WelcomeComplete = true
	existing.Language = "en"
	existing.AgentLanguage = "ja"
	session, err := NewSessionForTest(existing)
	if err != nil {
		t.Fatal(err)
	}
	if session.Config.AgentLanguage != "ja" {
		t.Fatalf("session dropped agent_language: %q", session.Config.AgentLanguage)
	}
	session.SetAgentLanguage("  ko ")
	if session.Config.AgentLanguage != "ko" {
		t.Fatalf("SetAgentLanguage should trim, got %q", session.Config.AgentLanguage)
	}
}
