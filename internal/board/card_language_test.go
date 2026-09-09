package board

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func readCard(t *testing.T, root, taskID string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "backlog", taskID, "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestNewCardRecordsConfiguredAgentLanguage(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(config.EnvConfig, path)

	// No config file: operational commands fail instead of deriving a language.
	if code, _, stderr := capture(t, func() int { return RunNew([]string{"chore", "lang-default", "默认语种"}) }); code == 0 {
		t.Fatalf("missing config must fail kander new, stderr=%s", stderr)
	}

	// Explicit config value wins, even before initialization completes.
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"welcome_complete":false,"kanban_agent":"codex","launcher":"tmux","language":"en","agent_language":"ja","reviewers":{"PM":"codex","CSA":"codex","Hacker":"codex","QA":"codex"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := capture(t, func() int { return RunNew([]string{"chore", "lang-config", "配置语种"}) }); code != 0 {
		t.Fatalf("code=%d %s", code, stderr)
	}
	text := readCard(t, root, todayID("lang-config"))
	if got := MetadataFrom(text, FieldLanguage); got != "ja" {
		t.Fatalf("configured language=%q", got)
	}
	if !strings.Contains(text, "- TASK_GROUP:\n- LANGUAGE: ja\n- CREATED_AT: ") {
		t.Fatalf("LANGUAGE must sit between TASK_GROUP and CREATED_AT:\n%s", text)
	}

	// --language overrides the config and is normalized.
	if code, _, stderr := capture(t, func() int { return RunNew([]string{"--language", " ko ", "chore", "lang-flag", "显式语种"}) }); code != 0 {
		t.Fatalf("code=%d %s", code, stderr)
	}
	if got := MetadataFrom(readCard(t, root, todayID("lang-flag")), FieldLanguage); got != "ko" {
		t.Fatalf("flag language=%q", got)
	}

	// Invalid values and a dangling flag are usage errors (exit 2 with the usage line) and create no card.
	for _, args := range [][]string{
		{"--language", "a\nb", "chore", "lang-bad", "坏语种"},
		{"--language", "", "chore", "lang-bad", "坏语种"},
		{"--language", strings.Repeat("x", 65), "chore", "lang-bad", "坏语种"},
		{"chore", "lang-bad", "坏语种", "--language"},
	} {
		code, _, stderr := capture(t, func() int { return RunNew(args) })
		if code != 2 || !strings.Contains(stderr, "kander new [--large] [--language") {
			t.Fatalf("expected usage error for %q, got code=%d stderr=%q", args, code, stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "backlog", todayID("lang-bad")+".md")); !os.IsNotExist(err) {
		t.Fatalf("rejected card must not exist: %v", err)
	}

	// An invalid config file is an error, not a silently guessed language.
	if err := os.WriteFile(path, []byte(`{"language":"xx"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := capture(t, func() int { return RunNew([]string{"chore", "lang-broken", "坏配置"}) }); code != 1 {
		t.Fatalf("invalid config must fail kander new as an ordinary error, got %d", code)
	}
}

func TestLargeCardRecordsAgentLanguage(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	writeCompleteConfig(t)
	if code, _, stderr := capture(t, func() int {
		return RunNew([]string{"--large", "--language", "de", "feature", "lang-large", "大卡语种"})
	}); code != 0 {
		t.Fatalf("code=%d %s", code, stderr)
	}
	data, err := os.ReadFile(filepath.Join(root, "backlog", todayID("lang-large"), "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := MetadataFrom(string(data), FieldLanguage); got != "de" {
		t.Fatalf("large card language=%q", got)
	}
}
