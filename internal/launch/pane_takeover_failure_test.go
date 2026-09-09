package launch

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

func TestPaneTakeoverFailuresRestoreOriginalCard(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX terminal fakes")
	}
	for _, launcher := range []string{"herdr", "tmux"} {
		for _, cause := range []string{"blocked", "timeout", "rejected"} {
			if launcher == "tmux" && cause == "rejected" {
				continue // tmux has no approval-dialog rejection primitive.
			}
			t.Run(launcher+"/"+cause, func(t *testing.T) {
				root, _, bin := setupBoard(t)
				freezeClock(t)
				id, path := makeTodo(t, root, "takeover-failure")
				startThenReview(t, root, "claude", id, path)
				original, err := board.ReadSnapshot(root, id)
				if err != nil {
					t.Fatal(err)
				}
				cfg := usePaneAgent(t, root, bin, launcher)
				cfg.Agents["panecli"].PromptDelivery.Ready.TimeoutMS = 250
				log := filepath.Join(root, "tmux.log")
				closeLog, promptLog := log+".kill", log+".send-keys"
				prefix := "KANBAN_TMUX_"
				outputKey := prefix + "PANE_OUTPUT"
				if launcher == "herdr" {
					log = installHerdr(t, root, bin)
					closeLog, promptLog = log+".close", log+".prompt"
					prefix = "KANBAN_HERDR_"
					outputKey = prefix + "AGENT_OUTPUT"
				}
				output := "TUI_READY TRUST_DIALOG"
				switch cause {
				case "blocked":
					t.Setenv(prefix+"BLOCKED_OUTPUT", output)
				case "timeout":
					output = "still starting"
					t.Setenv(outputKey, output)
				case "rejected":
					output = "TUI_READY"
					t.Setenv(outputKey, output)
					t.Setenv(prefix+"PROMPT_FAIL", "1")
				}
				agent := "panecli"
				_, _, err = capture(t, func() error {
					return commandResume(root, &agent, launcher, id, "take over", "", true, 61)
				})
				if err == nil || !strings.Contains(err.Error(), output) {
					t.Fatalf("missing failure diagnostics: %v", err)
				}
				manual := strings.Contains(err.Error(), "手动跑一次该 CLI 回答对话框后重试 `kander start`")
				if manual != (cause != "timeout") {
					t.Fatalf("incorrect recovery guidance: %v", err)
				}
				got, err := board.ReadSnapshot(root, id)
				if err != nil || got.Entry.State != original.Entry.State || got.Text != original.Text {
					t.Fatalf("takeover did not restore original state, owner, session and window: %+v %v", got, err)
				}
				if _, err := os.Stat(closeLog); err != nil {
					t.Fatalf("new container not closed: %v", err)
				}
				if cause != "rejected" {
					if _, err := os.Stat(promptLog); !os.IsNotExist(err) {
						t.Fatalf("prompt sent before readiness: %v", err)
					}
				}
			})
		}
	}
}
