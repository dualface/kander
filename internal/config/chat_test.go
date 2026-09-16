package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultChatSettingsMatchLargeFallbacks(t *testing.T) {
	setupHome(t)
	cfg := DefaultConfig()
	if cfg.ChatAgent != cfg.KanbanAgents["large"] {
		t.Fatalf("chat_agent=%s large=%s", cfg.ChatAgent, cfg.KanbanAgents["large"])
	}
	for _, agent := range ExecutionAgents {
		entry := cfg.Models.Chat[agent]
		if entry == nil {
			t.Fatalf("missing chat defaults for %s", agent)
		}
		wantModel := KanbanModelFor(cfg.Models.Kanban[agent], "large")
		if entry["model"] != wantModel {
			t.Fatalf("%s model=%q want %s", agent, entry["model"], wantModel)
		}
		if AgentSupportsEffort(cfg, agent) {
			if entry["effort"] != cfg.Models.Kanban[agent]["large_effort"] {
				t.Fatalf("%s effort=%q", agent, entry["effort"])
			}
		} else if _, ok := entry["effort"]; ok {
			t.Fatalf("%s chat must not store effort: %v", agent, entry)
		}
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	again, err := ValidateJSON(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if again.ChatAgent != cfg.ChatAgent {
		t.Fatalf("round-trip chat_agent=%s", again.ChatAgent)
	}
	agent, model, effort := ResolveChat(again)
	wantAgent, wantModel, wantEffort := ResolveChat(cfg)
	if agent != wantAgent || model != wantModel || effort != wantEffort {
		t.Fatalf("round-trip chat=%s %s %s want %s %s %s", agent, model, effort, wantAgent, wantModel, wantEffort)
	}
}

func TestMissingChatFieldsFallBackToLarge(t *testing.T) {
	setupHome(t)
	payload := minimalPayload(nil)
	payload["kanban_agent"] = "grok"
	payload["kanban_agents"] = map[string]any{"large": "grok", "small": "codex"}
	payload["models"] = map[string]any{
		"kanban": map[string]any{"grok": map[string]any{"large_model": "grok-chat-large", "large_effort": "xhigh"}},
	}
	cfg, err := Validate(payload)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ChatAgent != "grok" {
		t.Fatalf("chat_agent=%s", cfg.ChatAgent)
	}
	agent, model, effort := ResolveChat(cfg)
	if agent != "grok" || model != "grok-chat-large" || effort != "xhigh" {
		t.Fatalf("resolved=%s %s %s", agent, model, effort)
	}
}

func TestChatRejectsUnknownAgentFieldsAndUnsupportedEffort(t *testing.T) {
	setupHome(t)
	cases := []struct {
		models   any
		fragment string
	}{
		{map[string]any{"chat": map[string]any{"vim": map[string]any{"model": "x"}}}, "models.chat 含未知 agent"},
		{map[string]any{"chat": map[string]any{"codex": map[string]any{"temperature": "1"}}}, "models.chat.codex 含未知字段"},
		{map[string]any{"chat": map[string]any{"codex": map[string]any{"model": 1}}}, "models.chat.codex.model"},
		{map[string]any{"chat": map[string]any{"codex": map[string]any{"model": "a\nb"}}}, "models.chat.codex.model"},
		{map[string]any{"chat": map[string]any{"codex": map[string]any{"effort": "hi\x00gh"}}}, "models.chat.codex.effort"},
		{map[string]any{"chat": map[string]any{"cursor": map[string]any{"effort": "high"}}}, "models.chat.cursor 含未知字段"},
		{map[string]any{"chat": map[string]any{"devin": map[string]any{"effort": "high"}}}, "models.chat.devin 含未知字段"},
	}
	for _, tc := range cases {
		payload := minimalPayload(nil)
		payload["models"] = tc.models
		_, err := Validate(payload)
		if err == nil || !strings.Contains(err.Error(), tc.fragment) {
			t.Fatalf("models=%v err=%v want %s", tc.models, err, tc.fragment)
		}
	}
}

func TestCloneAndFormatCoverChat(t *testing.T) {
	setupHome(t)
	cfg := DefaultConfig()
	cfg.WelcomeComplete = true
	cloned := Clone(cfg)
	cloned.ChatAgent = "claude"
	cloned.Models.Chat["claude"]["model"] = "cloned-model"
	if cfg.ChatAgent == "claude" || cfg.Models.Chat["claude"]["model"] == "cloned-model" {
		t.Fatal("clone aliased chat settings")
	}
	cfg.ChatAgent = "claude"
	lines, err := FormatConfigLines(cfg)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, Text("config.chat_agent")+": claude") {
		t.Fatalf("format missing chat agent:\n%s", joined)
	}
	if !strings.Contains(joined, Text("config.chat_model")+":") {
		t.Fatalf("format missing chat model:\n%s", joined)
	}
}

func TestOverlayChatMergePrefersExplicitThenScopeThenKanban(t *testing.T) {
	setupHome(t)
	scope := DefaultConfig()
	scope.WelcomeComplete = true
	scope.ChatAgent = "codex"
	scope.Models.Chat["claude"] = map[string]string{"model": "scope-chat", "effort": "high"}
	scope.Models.Kanban["claude"]["large_model"] = "kanban-large"
	overlay := map[string]any{
		"chat_agent": "claude",
		"models": map[string]any{
			"chat": map[string]any{"claude": map[string]any{"model": "overlay-chat"}},
		},
	}
	merged, err := ApplyOverlay(scope, overlay)
	if err != nil {
		t.Fatal(err)
	}
	if merged.ChatAgent != "claude" {
		t.Fatalf("chat_agent=%s", merged.ChatAgent)
	}
	if merged.Models.Chat["claude"]["model"] != "overlay-chat" {
		t.Fatalf("model=%s", merged.Models.Chat["claude"]["model"])
	}
	if merged.Models.Chat["claude"]["effort"] != "high" {
		t.Fatalf("effort=%s", merged.Models.Chat["claude"]["effort"])
	}

	overlay = map[string]any{"chat_agent": "claude"}
	merged, err = ApplyOverlay(scope, overlay)
	if err != nil {
		t.Fatal(err)
	}
	if merged.Models.Chat["claude"]["model"] != "scope-chat" {
		t.Fatalf("scope chat lost: %s", merged.Models.Chat["claude"]["model"])
	}

	delete(scope.Models.Chat, "claude")
	overlay = map[string]any{"chat_agent": "claude"}
	merged, err = ApplyOverlay(scope, overlay)
	if err != nil {
		t.Fatal(err)
	}
	if merged.Models.Chat["claude"]["model"] != "kanban-large" {
		t.Fatalf("kanban fallback=%s", merged.Models.Chat["claude"]["model"])
	}
}

func TestRepairMissingChatUsesLargeAgent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvConfig, path)
	original := []byte(`{
		"schema_version": 1, "welcome_complete": true, "kanban_agent": "claude",
		"launcher": "foreground",
		"reviewers": {"PMQA":"claude","Security":"codex"},
		"models": {"kanban":{"claude":{"large_model":"my-model","large_effort":"high"}}}
	}`)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Repair(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ChatAgent != "claude" {
		t.Fatalf("chat_agent=%s", cfg.ChatAgent)
	}
	if cfg.Models.Chat["claude"]["model"] != "my-model" {
		t.Fatalf("chat model=%s", cfg.Models.Chat["claude"]["model"])
	}
}

func TestLegacyOverlayWithoutChatUsesLarge(t *testing.T) {
	setupHome(t)
	scopeRaw := map[string]any{
		"schema_version": 1, "welcome_complete": true, "kanban_agent": "claude",
		"kanban_agents": map[string]any{"large": "claude", "small": "codex"},
		"launcher":      "tmux", "reviewers": map[string]any{"PMQA": "codex", "Security": "codex"},
		"models": map[string]any{
			"kanban": map[string]any{"claude": map[string]any{"large_model": "legacy-large", "large_effort": "high"}},
		},
	}
	merged, err := MergeOverlayOnRaw(scopeRaw, map[string]any{"launcher": "herdr"})
	if err != nil {
		t.Fatal(err)
	}
	agent, model, effort := ResolveChat(merged)
	if agent != "claude" || model != "legacy-large" || effort != "high" {
		t.Fatalf("resolved=%s %s %s", agent, model, effort)
	}
}

func TestExecutionAgentsInUseIncludesDistinctChatAgent(t *testing.T) {
	setupHome(t)
	cfg := DefaultConfig()
	cfg.KanbanAgents["large"] = "codex"
	cfg.KanbanAgents["small"] = "codex"
	cfg.ChatAgent = "claude"
	got := ExecutionAgentsInUse(cfg)
	if len(got) != 2 || got[0] != "codex" || got[1] != "claude" {
		t.Fatalf("%v", got)
	}
	kanban := KanbanAgentsInUse(cfg)
	if len(kanban) != 1 || kanban[0] != "codex" {
		t.Fatalf("kanban in use=%v", kanban)
	}
}

func TestFormatConfigLinesOmitsChatOnlyKanbanModel(t *testing.T) {
	setupHome(t)
	cfg := DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.KanbanAgents["large"] = "codex"
	cfg.KanbanAgents["small"] = "codex"
	cfg.ChatAgent = "claude"
	lines, err := FormatConfigLines(cfg)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, Text("config.chat_agent")+": claude") {
		t.Fatalf("missing chat agent:\n%s", joined)
	}
	kanbanLabel := Text("config.kanban_model") + " claude:"
	if strings.Contains(joined, kanbanLabel) {
		t.Fatalf("chat-only agent listed as kanban model:\n%s", joined)
	}
	if !strings.Contains(joined, Text("config.kanban_model")+":") && !strings.Contains(joined, Text("config.kanban_model")+" codex:") {
		t.Fatalf("missing kanban model line:\n%s", joined)
	}
}
