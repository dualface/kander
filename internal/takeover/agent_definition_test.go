package takeover

import (
	"github.com/dualface/kander/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDismissResolvesOverriddenProcessName(t *testing.T) {
	root, _ := setupBoard(t)
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Agents = map[string]config.AgentDefinition{"claude": {ProcessName: "node"}}
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANBAN_TMUX_STALE_PANE", "%9")
	t.Setenv("KANBAN_TMUX_CURRENT_COMMAND", "node")
	t.Setenv("KANBAN_TMUX_PANE_SESSION", "session-1")
	t.Setenv("KANBAN_TMUX_LIST_PANES", "%8\t$88\trelocated\t@8\tnode\t0\tsession-1")
	t.Setenv("KANBAN_TMUX_TARGET_SESSION", "$88")
	t.Setenv("KANBAN_TMUX_TARGET_SESSION_NAME", "relocated")
	t.Setenv("KANBAN_TMUX_TARGET_WINDOW", "@8")
	id, path := makeDone(t, root, "dismiss-wrapper", "tmux:$1:@1:%9")
	before, _ := os.ReadFile(path)
	out, _, err := capture(t, func() error { return commandDismiss(root, id, 61) })
	after, _ := os.ReadFile(path)
	if err != nil || !strings.Contains(out, "关闭容器=@8") || string(before) != string(after) {
		t.Fatalf("%s %v", out, err)
	}
}
