package takeover

import (
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/launch"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func exitCommand(value string) *string { return &value }

func TestTemplateAgentDismissUsesExitCommand(t *testing.T) {
	root, _ := setupBoard(t)
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Agents = map[string]config.AgentDefinition{"helper": {
		Path: exe, ProcessName: "node",
		Args:        &config.AgentArgs{Start: []string{}, Resume: []string{}},
		Session:     &config.AgentSessionDefinition{Mode: "generated"},
		ExitCommand: exitCommand("/bye"),
	}}
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANBAN_TMUX_CURRENT_COMMAND", "node")
	t.Setenv("KANBAN_TMUX_PANE_SESSION", "session-1")
	t.Setenv("KANBAN_TMUX_TARGET_SESSION", "$1")
	t.Setenv("KANBAN_TMUX_TARGET_WINDOW", "@1")
	id, path := makeDone(t, root, "template-bye", "tmux:$1:@1:%9")
	data, _ := os.ReadFile(path)
	data = []byte(strings.Replace(string(data), "SESSION: claude ", "SESSION: helper ", 1))
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	_, _, err = capture(t, func() error { return commandDismiss(root, id, 61) })
	if err != nil {
		t.Fatal(err)
	}
	instruction, err := os.ReadFile(filepath.Join(root, "tmux.log.instruction"))
	if err != nil || !strings.Contains(string(instruction), "/bye") || !strings.Contains(string(instruction), "Enter") {
		t.Fatalf("instruction=%s err=%v", instruction, err)
	}
}

func TestTemplateAgentDismissRejectsMissingExitCommand(t *testing.T) {
	root, _ := setupBoard(t)
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Agents = map[string]config.AgentDefinition{"helper": {
		Path: exe, ProcessName: "node",
		Args:    &config.AgentArgs{Start: []string{}, Resume: []string{}},
		Session: &config.AgentSessionDefinition{Mode: "generated"},
	}}
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	id, path := makeDone(t, root, "template-missing-exit", "tmux:$1:@1:%9")
	data, _ := os.ReadFile(path)
	data = []byte(strings.Replace(string(data), "SESSION: claude ", "SESSION: helper ", 1))
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	_, errb, err := capture(t, func() error { return commandDismiss(root, id, 61) })
	if err == nil {
		t.Fatal("missing exit_command accepted")
	}
	msg := err.Error() + errb
	if !strings.Contains(msg, "exit_command") {
		t.Fatalf("error should name exit_command: %s", msg)
	}
}

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

func TestConfiguredAgentDismissAndCleanup(t *testing.T) {
	for _, test := range []struct{ name, dialect, mode string }{
		{"my-claude", "claude", "generated"}, {"my-grok", "grok", "generated"}, {"claude", "claude", "none"},
	} {
		for _, action := range []string{"dismiss", "cleanup"} {
			t.Run(test.name+"-"+action, func(t *testing.T) {
				root, _ := setupBoard(t)
				t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
				cfg := config.DefaultConfig()
				cfg.WelcomeComplete = true
				cfg.Agents = map[string]config.AgentDefinition{test.name: {Dialect: test.dialect, ProcessName: "node", Session: &config.AgentSessionDefinition{Mode: test.mode}}}
				if _, err := config.Save(cfg); err != nil {
					t.Fatal(err)
				}
				t.Setenv("KANBAN_TMUX_CURRENT_COMMAND", "node")
				t.Setenv("KANBAN_TMUX_PANE_SESSION", "session-1")
				t.Setenv("KANBAN_TMUX_TARGET_SESSION", "$1")
				t.Setenv("KANBAN_TMUX_TARGET_WINDOW", "@1")
				if action == "cleanup" {
					got := Cleanup("tmux:$1:@1:%9", launch.AgentSession{Agent: test.name, Reference: "session-1"}, "tmux:$2:@2:%10", 61)
					if !got.Cleaned {
						t.Fatal(got)
					}
				} else {
					id, path := makeDone(t, root, "configured-dismiss", "tmux:$1:@1:%9")
					data, _ := os.ReadFile(path)
					data = []byte(strings.Replace(string(data), "SESSION: claude ", "SESSION: "+test.name+" ", 1))
					if err := os.WriteFile(path, data, 0600); err != nil {
						t.Fatal(err)
					}
					_, _, err := capture(t, func() error { return commandDismiss(root, id, 61) })
					if err != nil {
						t.Fatal(err)
					}
				}
				instruction, err := os.ReadFile(filepath.Join(root, "tmux.log.instruction"))
				expected := "/exit"
				if test.dialect == "grok" {
					expected = "/quit"
				}
				if err != nil || !strings.Contains(string(instruction), expected) {
					t.Fatalf("exit instruction=%s error=%v", instruction, err)
				}
			})
		}
	}
}
