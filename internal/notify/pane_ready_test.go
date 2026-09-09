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
	for _, launcher := range []string{"herdr", "tmux"} {
		t.Run(launcher, func(t *testing.T) {
			root, _ := setupBoard(t)
			writePaneClaudeConfig(t, filepath.Join(root, "config.json"))
			t.Setenv("KANBAN_HERDR_SESSION", "session-1")
			taskID, path := makeReview(t, root, "notify-pane-ready")
			window, sentLog := "herdr:w1:t9:w1:p9", "herdr.log.prompt"
			if launcher == "tmux" {
				window, sentLog = "tmux:$1:@1:%1", "tmux.log.send"
				t.Setenv("KANBAN_TMUX_CURRENT_COMMAND", "claude")
				t.Setenv("KANBAN_TMUX_PANE_SESSION", "session-1")
			}
			setWindow(t, path, window)
			_, _, err := capture(t, func() error {
				return commandNotify(root, taskID, "x", "", "", true, 61)
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(root, sentLog)); err != nil {
				t.Fatal("direct notify did not prompt")
			}
			wait, _ := os.ReadFile(filepath.Join(root, "herdr.log.wait"))
			if strings.Contains(string(wait), "TUI_READY") {
				t.Fatalf("notify waited for pane ready: %s", wait)
			}
			// A capture after send is the existing transport-marker check, not readiness.
			if _, err := os.Stat(filepath.Join(root, "tmux.log.capture-before-send")); !os.IsNotExist(err) {
				t.Fatalf("direct notify waited for pane output before delivery: %v", err)
			}
		})
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
