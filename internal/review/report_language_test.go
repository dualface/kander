package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestReportLanguageFromConfigNeedsReadableConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(config.EnvConfig, path)
	if _, err := reportLanguageFromConfig(); err == nil {
		t.Fatal("missing config must fail")
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := reportLanguageFromConfig(); err == nil {
		t.Fatal("invalid JSON must fail")
	}
	if err := os.WriteFile(path, []byte(`{"language":"xx"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := reportLanguageFromConfig(); err == nil {
		t.Fatal("schema-invalid config must fail")
	}
	// An uninitialized config still carries the user's explicit choice.
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"welcome_complete":false,"kanban_agent":"codex","launcher":"tmux","language":"en","agent_language":"ja","reviewers":{"PM":"codex","CSA":"codex","Hacker":"codex","QA":"codex"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := reportLanguageFromConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got != "ja" {
		t.Fatalf("explicit agent_language lost, got %q", got)
	}
}

func TestReviewerFromConfigRequiresCompleteConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(config.EnvConfig, path)
	if _, err := reviewerFromConfig("PM"); err == nil {
		t.Fatal("missing config must fail")
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := reviewerFromConfig("PM"); err == nil {
		t.Fatal("invalid JSON must fail")
	}
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"welcome_complete":true,"kanban_agent":"codex","launcher":"tmux","language":"en","reviewers":{"PM":"grok","CSA":"codex","Hacker":"codex","QA":"codex"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := reviewerFromConfig("PM")
	if err != nil {
		t.Fatal(err)
	}
	if got != "grok" {
		t.Fatalf("got %q", got)
	}
}

func TestReportLanguageRuleOnlyWhenKnown(t *testing.T) {
	if reportLanguageRule("") != "" {
		t.Fatal("no language must add no instruction")
	}
	rule := reportLanguageRule("ja")
	if !strings.Contains(rule, `"ja"`) || !strings.Contains(rule, "exactly as they are") {
		t.Fatalf("unexpected rule: %q", rule)
	}
}
