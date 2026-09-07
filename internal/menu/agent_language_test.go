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

func TestAgentLanguageChoicesOrderAndPreserveOutOfList(t *testing.T) {
	want := []Choice{
		{Value: "en", Label: "English"},
		{Value: "zh-CN", Label: "简体中文"},
		{Value: "zh-TW", Label: "繁體中文"},
		{Value: "ja", Label: "日本語"},
		{Value: "ko", Label: "한국어"},
		{Value: "es", Label: "Español"},
		{Value: "fr", Label: "Français"},
		{Value: "de", Label: "Deutsch"},
	}
	got := agentLanguageChoices("zh-CN")
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("choice[%d]=%+v want %+v", i, got[i], want[i])
		}
	}

	existing := config.DefaultConfig()
	existing.WelcomeComplete = true
	existing.AgentLanguage = "pt"
	session, err := NewSessionForTest(existing)
	if err != nil {
		t.Fatal(err)
	}
	choices := session.AgentLanguageChoices()
	if len(choices) != len(want)+1 {
		t.Fatalf("out-of-list len=%d want %d", len(choices), len(want)+1)
	}
	last := choices[len(choices)-1]
	if last.Value != "pt" || last.Label != "pt" {
		t.Fatalf("preserved choice=%+v", last)
	}
	for i := range want {
		if choices[i] != want[i] {
			t.Fatalf("fixed choice[%d]=%+v want %+v", i, choices[i], want[i])
		}
	}
}
