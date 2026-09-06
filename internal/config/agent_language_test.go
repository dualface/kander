package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestAgentLanguageDefaultsFollowInterfaceLanguage(t *testing.T) {
	setupHome(t)
	base := func() map[string]any {
		return map[string]any{
			"schema_version":   1,
			"welcome_complete": true,
			"kanban_agent":     "codex",
			"launcher":         "tmux",
			"reviewers":        map[string]any{"PM": "codex", "CSA": "codex", "Hacker": "codex", "QA": "codex"},
		}
	}
	payload := base()
	cfg, err := Validate(payload)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AgentLanguage != "zh-CN" {
		t.Fatalf("missing language should derive zh-CN, got %q", cfg.AgentLanguage)
	}
	payload["language"] = "en"
	if cfg, err = Validate(payload); err != nil || cfg.AgentLanguage != "en" {
		t.Fatalf("en config should derive en, got %q err=%v", cfg.AgentLanguage, err)
	}
	payload["agent_language"] = " ja "
	if cfg, err = Validate(payload); err != nil || cfg.AgentLanguage != "ja" {
		t.Fatalf("explicit value should be kept trimmed, got %q err=%v", cfg.AgentLanguage, err)
	}
	if cfg, err = Validate(map[string]any{"schema_version": 1, "welcome_complete": true, "kanban_agent": "codex", "launcher": "tmux",
		"reviewers":      map[string]any{"PM": "codex", "CSA": "codex", "Hacker": "codex", "QA": "codex"},
		"agent_language": strings.Repeat("语", 64)}); err != nil || utf8.RuneCountInString(cfg.AgentLanguage) != 64 {
		t.Fatalf("64 multibyte characters must be accepted: %q err=%v", cfg.AgentLanguage, err)
	}
	for _, bad := range []any{"", "   ", nil, 3, "a\nb", "a\rb", "a\u2028b", "a\u2029b", "a\u0085b", strings.Repeat("x", 65), strings.Repeat("语", 65)} {
		payload["agent_language"] = bad
		if _, err := Validate(payload); !IsError(err) {
			t.Fatalf("expected error for agent_language %#v, got %v", bad, err)
		}
	}
	if DefaultAgentLanguage("nope") != "en" {
		t.Fatal("unknown interface language must fall back to en")
	}
}

func TestRepairDerivesAgentLanguageFromStoredLanguage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvConfig, path)
	if err := os.WriteFile(path, []byte(`{"language":"cn"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Repair(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Language != "cn" || cfg.AgentLanguage != "zh-CN" {
		t.Fatalf("language=%q agent_language=%q", cfg.Language, cfg.AgentLanguage)
	}
	if err := os.WriteFile(path, []byte(`{"language":"cn","agent_language":"ja"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if cfg, _, err = Repair(nil); err != nil || cfg.AgentLanguage != "ja" {
		t.Fatalf("explicit agent_language lost: %q err=%v", cfg.AgentLanguage, err)
	}
	if err := os.WriteFile(path, []byte(`{"language":"cn","agent_language":""}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if cfg, _, err = Repair(nil); err != nil || cfg.AgentLanguage != "zh-CN" {
		t.Fatalf("empty agent_language should be repaired from language: %q err=%v", cfg.AgentLanguage, err)
	}
}
