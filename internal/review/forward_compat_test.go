package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

func TestReviewerAgentsThisVersionCannotUseFailOnUse(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(config.EnvConfig, path)
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Reviewers["large"]["PMQA"] = "newreviewer"
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	doc["agents"] = map[string]any{"futureagent": map[string]any{"path": "sh", "args": map[string]any{"start": []any{}, "resume": []any{}}, "session": map[string]any{"mode": "hook:future-hook"}}}
	data, _ = json.Marshal(doc)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = reviewerFromConfig("PMQA", "large")
	if err == nil || !strings.Contains(err.Error(), "newreviewer") {
		t.Fatalf("a role naming an unknown reviewer must fail on use and name it: %v", err)
	}
	_, _, err = splitAgentArgs([]string{"futureagent", "/abs/path"})
	if err == nil || !strings.Contains(err.Error(), "futureagent") || !strings.Contains(err.Error(), "hook:future-hook") {
		t.Fatalf("an explicit unusable reviewer must name the agent and the mode: %v", err)
	}
}

// A built-in reviewer whose agents entry this version cannot use is out of service,
// whether the role or the command line names it.
func TestUnusableBuiltinReviewerIsRefused(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(config.EnvConfig, path)
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Reviewers["large"]["PMQA"] = "codex"
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	doc["agents"] = map[string]any{"codex": map[string]any{"args": map[string]any{"start": []any{}, "resume": []any{}}, "session": map[string]any{"mode": "hook:future-hook"}}}
	data, _ = json.Marshal(doc)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := reviewerFromConfig("PMQA", "large"); err == nil || !strings.Contains(err.Error(), "codex") || !strings.Contains(err.Error(), "hook:future-hook") {
		t.Fatalf("a role naming an unusable built-in reviewer: %v", err)
	}
	if _, _, err := splitAgentArgs([]string{"codex", "/abs/path"}); err == nil || !strings.Contains(err.Error(), "hook:future-hook") {
		t.Fatalf("an explicit unusable built-in reviewer: %v", err)
	}
}
