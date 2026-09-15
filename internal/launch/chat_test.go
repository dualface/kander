package launch

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// useChatConfig sets the Chat Agent and launcher for PreviewChat/StartChat.
func useChatConfig(t *testing.T, agent, launcher string) {
	t.Helper()
	oldLoad := loadEffective
	loadEffective = func() (*config.Config, error) {
		cfg := envConfig("codex", launcher, map[string]string{"large": agent, "small": "codex"})
		cfg.ChatAgent = agent
		return cfg, nil
	}
	t.Cleanup(func() { loadEffective = oldLoad })
}

// captureTaskFile records the body and path of every task file StartChat creates.
func captureTaskFile(t *testing.T) (body, path *string) {
	t.Helper()
	body, path = new(string), new(string)
	oldCreate := createTaskFile
	createTaskFile = func(text, prefix string) (string, error) {
		created, err := oldCreate(text, prefix)
		*body, *path = text, created
		return created, err
	}
	t.Cleanup(func() { createTaskFile = oldCreate })
	return body, path
}

func TestStartChatHandsTheMessageWithoutBoardWrites(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	useChatConfig(t, "claude", "tmux")
	oldNow := nowFn
	nowFn = func() time.Time { return time.Date(2026, 9, 14, 18, 5, 9, 0, time.Local) }
	t.Cleanup(func() { nowFn = oldNow })
	body, taskFile := captureTaskFile(t)
	message := "first line\nsecond line with \"quotes\" & $HOME"

	result, err := StartChat(ChatRequest{Root: root, Message: message})
	if err != nil {
		t.Fatal(err)
	}
	if result.Agent != "claude" || result.Launcher != "tmux" {
		t.Fatalf("result=%+v", result)
	}
	if !strings.HasPrefix(result.Address, "tmux:") {
		t.Fatalf("address must carry the launcher prefix for focus: %q", result.Address)
	}
	if *body != message {
		t.Fatalf("task file body must be the message verbatim: %q", *body)
	}
	data, err := os.ReadFile(*taskFile)
	if err != nil {
		t.Fatalf("task file handed to the agent must stay: %v", err)
	}
	if !strings.HasPrefix(string(data), message+"\n") {
		t.Fatalf("task file content=%q", data)
	}
	if !strings.HasPrefix(filepath.Base(*taskFile), "kander-chat-") {
		t.Fatalf("task file=%s", *taskFile)
	}
	if args := mustRead(t, filepath.Join(root, "tmux.log")); !strings.Contains(args, "chat-20260914-180509") {
		t.Fatalf("window name missing from tmux calls: %s", args)
	}
	command := lastCommand(t, root)
	if !strings.Contains(command, "claude") || !strings.Contains(command, *taskFile) {
		t.Fatalf("command=%s", command)
	}
	if strings.Contains(command, "second line") {
		t.Fatalf("the message must not reach the command line: %s", command)
	}
	for _, state := range board.States {
		entries, err := os.ReadDir(filepath.Join(root, state))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("chat wrote board state %s: %v", state, entries)
		}
	}
}

func TestStartChatFailureClosesTheContainerAndTaskFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	useChatConfig(t, "claude", "tmux")
	_, taskFile := captureTaskFile(t)
	t.Setenv("KANBAN_TMUX_RESPAWN_FAIL", "1")

	if _, err := StartChat(ChatRequest{Root: root, Message: "hello"}); err == nil {
		t.Fatal("expected the launch to fail")
	}
	if kill, err := os.ReadFile(filepath.Join(root, "tmux.log.kill")); err != nil || !strings.Contains(string(kill), "kill-window") {
		t.Fatalf("created window was not closed: %q %v", kill, err)
	}
	if *taskFile == "" {
		t.Fatal("task file was not created")
	}
	if _, err := os.Stat(*taskFile); !os.IsNotExist(err) {
		t.Fatalf("failed launch left task file %s", *taskFile)
	}
}

func TestStartChatReportsAContainerItCouldNotClose(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, fakeBin := setupBoard(t)
	useChatConfig(t, "claude", "tmux")
	t.Setenv("KANBAN_TMUX_RESPAWN_FAIL", "1")
	wrapper := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = kill-window ]; then echo kill-window refused >&2; exit 1; fi\nexec " + filepath.Join(fakeBin, "tmux") + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(wrapper, "tmux"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", wrapper+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := StartChat(ChatRequest{Root: root, Message: "hello"})
	if err == nil || !strings.Contains(err.Error(), "kill-window refused") {
		t.Fatalf("the close failure must be reported: %v", err)
	}
}

func TestStartChatRejectsBeforeCreatingAnything(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	for _, tc := range []struct {
		name, launcher, message, want string
	}{
		{"blank message", "tmux", " \n\t", "chat"},
		{"foreground launcher", "foreground", "hello", "foreground"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useChatConfig(t, "claude", tc.launcher)
			_, taskFile := captureTaskFile(t)
			_, err := StartChat(ChatRequest{Root: root, Message: tc.message})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v", err)
			}
			if *taskFile != "" {
				t.Fatalf("rejected chat created task file %s", *taskFile)
			}
			if _, err := os.Stat(filepath.Join(root, "tmux.log")); err == nil {
				t.Fatal("rejected chat created a container")
			}
		})
	}
}

func TestPreviewChatResolvesTheChatAgentAndRefusesTerminalLaunchers(t *testing.T) {
	setupBoard(t)
	useChatConfig(t, "grok", "tmux")
	preview, err := PreviewChat()
	if err != nil {
		t.Fatal(err)
	}
	if preview.Agent != "grok" || preview.Launcher != "tmux" {
		t.Fatalf("preview=%+v", preview)
	}
	useChatConfig(t, "grok", "foreground")
	if _, err := PreviewChat(); err == nil || !strings.Contains(err.Error(), "foreground") {
		t.Fatalf("foreground must be refused with its name: %v", err)
	}
}

func TestStartChatUsesChatModelNotKanbanLarge(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	oldLoad := loadEffective
	loadEffective = func() (*config.Config, error) {
		cfg := envConfig("codex", "tmux", map[string]string{"large": "codex", "small": "codex"})
		cfg.ChatAgent = "claude"
		cfg.Models.Kanban["claude"]["large_model"] = "kanban-large"
		cfg.Models.Kanban["claude"]["large_effort"] = "high"
		cfg.Models.Chat["claude"] = map[string]string{"model": "chat-only-model", "effort": "low"}
		return cfg, nil
	}
	t.Cleanup(func() { loadEffective = oldLoad })

	preview, err := PreviewChat()
	if err != nil {
		t.Fatal(err)
	}
	if preview.Agent != "claude" {
		t.Fatalf("preview agent=%s", preview.Agent)
	}
	if _, err := StartChat(ChatRequest{Root: root, Message: "hello"}); err != nil {
		t.Fatal(err)
	}
	command := lastCommand(t, root)
	if !strings.Contains(command, "chat-only-model") || !strings.Contains(command, "low") {
		t.Fatalf("chat argv missing dedicated model/effort: %s", command)
	}
	if strings.Contains(command, "kanban-large") || strings.Contains(command, "gpt-5.6-sol") {
		t.Fatalf("kanban large leaked into chat argv: %s", command)
	}
}

func TestStartChatFallsBackToLargeWhenChatKeysMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	oldLoad := loadEffective
	loadEffective = func() (*config.Config, error) {
		cfg := envConfig("codex", "tmux", map[string]string{"large": "claude", "small": "codex"})
		cfg.ChatAgent = ""
		cfg.Models.Chat = map[string]map[string]string{}
		cfg.Models.Kanban["claude"]["large_model"] = "legacy-large"
		cfg.Models.Kanban["claude"]["large_effort"] = "high"
		return cfg, nil
	}
	t.Cleanup(func() { loadEffective = oldLoad })
	preview, err := PreviewChat()
	if err != nil {
		t.Fatal(err)
	}
	if preview.Agent != "claude" {
		t.Fatalf("fallback agent=%s", preview.Agent)
	}
	if _, err := StartChat(ChatRequest{Root: root, Message: "hello"}); err != nil {
		t.Fatal(err)
	}
	command := lastCommand(t, root)
	if !strings.Contains(command, "legacy-large") || !strings.Contains(command, "high") {
		t.Fatalf("fallback argv=%s", command)
	}
}
