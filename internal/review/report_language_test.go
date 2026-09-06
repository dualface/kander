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
	if got := reportLanguageFromConfig(); got != "" {
		t.Fatalf("missing config must not pick a language, got %q", got)
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := reportLanguageFromConfig(); got != "" {
		t.Fatalf("invalid config must not pick a language, got %q", got)
	}
	// An uninitialized config still carries the user's explicit choice.
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"welcome_complete":false,"kanban_agent":"codex","launcher":"tmux","language":"en","agent_language":"ja","reviewers":{"PM":"codex","CSA":"codex","Hacker":"codex","QA":"codex"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := reportLanguageFromConfig(); got != "ja" {
		t.Fatalf("explicit agent_language lost, got %q", got)
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
