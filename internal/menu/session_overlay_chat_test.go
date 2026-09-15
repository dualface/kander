package menu

import (
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestSetChatAgentWritesOnlyChatAgentOnProjectTab(t *testing.T) {
	session, _ := tempOverlaySession(t, config.ModeGlobal)
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	next := "claude"
	if session.Config.ChatAgent == next {
		next = "grok"
	}
	session.SetChatAgent(next)
	if session.Config.ChatAgent != next {
		t.Fatalf("effective chat_agent=%s", session.Config.ChatAgent)
	}
	if !session.FieldOverridden("chat_agent") {
		t.Fatal("chat_agent was not written")
	}
	if config.OverlayHas(session.overlayRaw, "models", "chat") {
		t.Fatalf("switching agent wrote models.chat: %#v", session.overlayRaw)
	}
}

func TestChatModelEditPinsChatAgent(t *testing.T) {
	session, _ := tempOverlaySession(t, config.ModeGlobal)
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	fields := session.ChatModelFieldsFor()
	if len(fields) == 0 {
		t.Fatal("no chat model fields")
	}
	if session.FieldOverridden("chat_agent") {
		t.Fatal("chat_agent already overridden")
	}
	session.NoteModelOverride(fields[0], "project-chat-model")
	if !session.FieldOverridden("chat_agent") {
		t.Fatal("model edit did not pin chat_agent")
	}
	if !session.FieldOverridden("models", "chat", fields[0].Agent, "model") {
		t.Fatal("chat model overlay missing")
	}
}

func TestRestoreChatFieldAndSection(t *testing.T) {
	session, _ := tempOverlaySession(t, config.ModeGlobal)
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	scopeAgent := session.Config.ChatAgent
	session.SetChatAgent("claude")
	fields := session.ChatModelFieldsFor()
	session.NoteModelOverride(fields[0], "overlay-only")
	if err := session.RestoreInherit("models", "chat", fields[0].Agent, "model"); err != nil {
		t.Fatal(err)
	}
	if session.FieldOverridden("models", "chat", fields[0].Agent, "model") {
		t.Fatal("single field restore left the overlay key")
	}
	if !session.FieldOverridden("chat_agent") {
		t.Fatal("single field restore cleared chat_agent")
	}
	if err := session.RestoreSection("execution"); err != nil {
		t.Fatal(err)
	}
	if session.FieldOverridden("chat_agent") || config.OverlayHas(session.overlayRaw, "models", "chat") {
		t.Fatalf("section restore left chat overrides: %#v", session.overlayRaw)
	}
	if session.Config.ChatAgent != scopeAgent {
		t.Fatalf("effective chat_agent=%s want %s", session.Config.ChatAgent, scopeAgent)
	}
}

func TestCursorChatFieldsOmitEffort(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.ChatAgent = "cursor"
	session, err := NewSessionForTest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	fields := session.ChatModelFieldsFor()
	if len(fields) != 1 || fields[0].FieldName() != "model" || fields[0].Kind() != "chat" {
		t.Fatalf("cursor fields=%+v", fields)
	}
}

func TestGlobalChatAgentSwitchFallsBackToKanbanLarge(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.ChatAgent = "codex"
	cfg.Models.Chat = map[string]map[string]string{
		"codex": {"model": "codex-chat", "effort": "high"},
	}
	cfg.Models.Kanban["claude"]["large_model"] = "custom-claude-large"
	cfg.Models.Kanban["claude"]["large_effort"] = "high"
	session, err := NewSessionForTest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	session.SetChatAgent("claude")
	got := config.ChatSettingsFor(session.Config)
	if got.Agent != "claude" || got.Model != "custom-claude-large" || got.Effort != "high" {
		t.Fatalf("chat settings=%+v", got)
	}
	fields := session.ChatModelFieldsFor()
	if len(fields) == 0 {
		t.Fatal("no chat model fields after agent switch")
	}
	if fields[0].Value() != "custom-claude-large" {
		t.Fatalf("displayed model=%q", fields[0].Value())
	}
}
