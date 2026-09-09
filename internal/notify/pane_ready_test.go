package notify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestNotifyDirectDeliveryDoesNotWaitForPaneReady(t *testing.T) {
	root, _ := setupBoard(t)
	writePaneClaudeConfig(t, filepath.Join(root, "config.json"))
	t.Setenv("KANBAN_HERDR_SESSION", "session-1")
	taskID, path := makeReview(t, root, "notify-pane-ready")
	setWindow(t, path, "herdr:w1:t9:w1:p9")
	_, _, err := capture(t, func() error {
		return commandNotify(root, taskID, "x", "", "", true, 61)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "herdr.log.prompt")); err != nil {
		t.Fatal("direct notify did not prompt")
	}
	wait, _ := os.ReadFile(filepath.Join(root, "herdr.log.wait"))
	if strings.Contains(string(wait), "TUI_READY") {
		t.Fatalf("notify waited for pane ready: %s", wait)
	}
}

func writePaneClaudeConfig(t *testing.T, path string) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.KanbanAgent = "claude"
	cfg.KanbanAgents = map[string]string{"large": "claude", "small": "claude"}
	cfg.Agents = map[string]config.AgentDefinition{
		"claude": {
			PromptDelivery: &config.PromptDelivery{
				Mode: "pane",
				Ready: &config.PromptReady{
					Match:     "TUI_READY",
					TimeoutMS: 2000,
				},
			},
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
