package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/focus"
	"github.com/dualface/kander/internal/launch"
)

// chatApp builds a board app with fake chat bindings. start answers every
// submission; the returned slices record the messages and focused addresses.
func chatApp(t *testing.T, start func(string) (launch.ChatResult, error)) (*App, *[]string, *[]string) {
	t.Helper()
	app := newApp(true, 30, tuiPageContext(), func() (BoardPayload, error) { return BoardPayload{}, nil },
		func(string) (Task, error) { return Task{}, errors.New("no detail") }, "dark", 1, nil, nil)
	app.Width, app.Height = 120, 30
	app.PrepareChat = func() (launch.ChatPreview, error) {
		return launch.ChatPreview{Agent: "codex", Launcher: "tmux"}, nil
	}
	messages, focused := &[]string{}, &[]string{}
	app.StartChat = func(message string) (launch.ChatResult, error) {
		*messages = append(*messages, message)
		return start(message)
	}
	app.FocusWindow = func(_ context.Context, address string) focus.Result {
		*focused = append(*focused, address)
		return focus.Result{Success: true}
	}
	return app, messages, focused
}

func chatStarted(string) (launch.ChatResult, error) {
	return launch.ChatResult{Agent: "codex", Launcher: "tmux", Address: "tmux:$1:@2:%3"}, nil
}

func pressKey(app *App, key tea.KeyMsg) { app.Update(key) }

func typeText(app *App, text string) {
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
}

func openReadyChat(t *testing.T, app *App) {
	t.Helper()
	typeText(app, "c")
	if app.Chat == nil || app.Chat.phase != chatLoading {
		t.Fatalf("c must open the chat box while the preview loads: %+v", app.Chat)
	}
	runPendingWork(t, app)
	if app.Chat.phase != chatReady || app.Chat.agent != "codex" || app.Chat.launcher != "tmux" {
		t.Fatalf("preview not applied: %+v", app.Chat)
	}
}

func TestChatBoxSubmitsMultilineMessageFocusesAndCloses(t *testing.T) {
	app, messages, focused := chatApp(t, chatStarted)
	openReadyChat(t, app)
	if view := ansi.Strip(app.View()); !strings.Contains(view, config.Text("tui.chat_title")) || !strings.Contains(view, "codex") {
		t.Fatalf("chat box not rendered:\n%s", view)
	}

	typeText(app, "first")
	pressKey(app, tea.KeyMsg{Type: tea.KeyEnter})
	typeText(app, "second")
	if got := app.Chat.input.Value(); got != "first\nsecond" {
		t.Fatalf("Enter must insert a newline: %q", got)
	}

	pressKey(app, tea.KeyMsg{Type: tea.KeyCtrlS})
	if app.Chat.phase != chatRunning || len(*messages) != 0 {
		t.Fatalf("Ctrl+S must start in the background: phase=%v calls=%v", app.Chat.phase, *messages)
	}
	runPendingWork(t, app)
	if len(*messages) != 1 || (*messages)[0] != "first\nsecond" {
		t.Fatalf("messages=%q", *messages)
	}
	if len(*focused) != 1 || (*focused)[0] != "tmux:$1:@2:%3" {
		t.Fatalf("focus must use the full address: %q", *focused)
	}
	if app.Chat != nil || app.chatDraft != "" || app.CopyNotice != "" {
		t.Fatalf("a started chat must close the box without a board notice: chat=%+v draft=%q notice=%q", app.Chat, app.chatDraft, app.CopyNotice)
	}
}

func TestChatBoxEscKeepsTheDraftAndRejectsBlankMessages(t *testing.T) {
	app, messages, _ := chatApp(t, chatStarted)
	openReadyChat(t, app)

	typeText(app, "   ")
	pressKey(app, tea.KeyMsg{Type: tea.KeyCtrlS})
	if app.Chat.phase != chatReady || app.pendingWork != nil || app.Chat.status != config.Text("tui.chat_empty") {
		t.Fatalf("blank message must not start: %+v", app.Chat)
	}

	typeText(app, "draft")
	pressKey(app, tea.KeyMsg{Type: tea.KeyEsc})
	if app.Chat != nil {
		t.Fatal("Esc must close the chat box")
	}
	openReadyChat(t, app)
	if got := app.Chat.input.Value(); got != "   draft" {
		t.Fatalf("draft not restored: %q", got)
	}
	if len(*messages) != 0 {
		t.Fatalf("nothing should have started: %q", *messages)
	}
}

func TestChatBoxFailureKeepsTheMessageForRetry(t *testing.T) {
	fail := true
	app, messages, focused := chatApp(t, func(message string) (launch.ChatResult, error) {
		if fail {
			return launch.ChatResult{}, errors.New("tmux exploded")
		}
		return chatStarted(message)
	})
	openReadyChat(t, app)
	typeText(app, "hello")
	pressKey(app, tea.KeyMsg{Type: tea.KeyCtrlS})

	// Keys are ignored while the start runs, Esc and Ctrl+C included.
	typeText(app, "x")
	pressKey(app, tea.KeyMsg{Type: tea.KeyEsc})
	pressKey(app, tea.KeyMsg{Type: tea.KeyCtrlC})
	if !app.Running || app.Chat == nil || app.Chat.input.Value() != "hello" {
		t.Fatalf("running chat must ignore keys: %+v", app.Chat)
	}
	runPendingWork(t, app)
	if app.Chat.phase != chatReady || app.Chat.input.Value() != "hello" || !strings.Contains(app.Chat.status, "tmux exploded") {
		t.Fatalf("failure must keep the input and show the error: %+v", app.Chat)
	}
	if len(*focused) != 0 {
		t.Fatalf("a failed start must not focus: %q", *focused)
	}

	fail = false
	pressKey(app, tea.KeyMsg{Type: tea.KeyCtrlS})
	runPendingWork(t, app)
	if len(*messages) != 2 || (*messages)[1] != "hello" || app.Chat != nil || app.chatDraft != "" {
		t.Fatalf("retry did not start the kept message and close: %q chat=%+v draft=%q", *messages, app.Chat, app.chatDraft)
	}
}

func TestChatBoxUnavailableLauncherNeverStarts(t *testing.T) {
	app, messages, _ := chatApp(t, chatStarted)
	app.PrepareChat = func() (launch.ChatPreview, error) {
		return launch.ChatPreview{}, errors.New("launcher foreground would take over the board's terminal")
	}
	typeText(app, "c")
	runPendingWork(t, app)
	if app.Chat.phase != chatUnavailable {
		t.Fatalf("phase=%v", app.Chat.phase)
	}
	if view := ansi.Strip(app.View()); !strings.Contains(view, "foreground") {
		t.Fatalf("reason missing:\n%s", view)
	}
	typeText(app, "hello")
	pressKey(app, tea.KeyMsg{Type: tea.KeyCtrlS})
	if app.pendingWork != nil || len(*messages) != 0 {
		t.Fatal("an unavailable chat must not start")
	}
}

func TestChatBoxDropsStaleResults(t *testing.T) {
	app, _, _ := chatApp(t, chatStarted)
	app.PrepareChat = func() (launch.ChatPreview, error) {
		return launch.ChatPreview{Agent: "stale", Launcher: "tmux"}, nil
	}
	typeText(app, "c")
	stalePreview := app.pendingWork
	app.pendingWork = nil
	pressKey(app, tea.KeyMsg{Type: tea.KeyEsc})
	typeText(app, "c")
	// The reopened box is still loading, so only the sequence can reject the
	// preview requested by the closed box.
	app.applyWork(stalePreview())
	if app.Chat.phase != chatLoading || app.Chat.agent != "" {
		t.Fatalf("stale preview applied: %+v", app.Chat)
	}

	runPendingWork(t, app)
	typeText(app, "hello")
	pressKey(app, tea.KeyMsg{Type: tea.KeyCtrlS})
	app.pendingWork = nil
	app.applyWork(chatStartResult{sequence: app.Chat.sequence - 1, result: launch.ChatResult{Address: "tmux:old"}})
	if app.Chat.phase != chatRunning || app.Chat.input.Value() != "hello" {
		t.Fatalf("stale start result applied: %+v", app.Chat)
	}
}

func TestChatBoxWithoutBoardOnlyShowsANotice(t *testing.T) {
	app, _, _ := chatApp(t, chatStarted)
	app.StartChat = nil
	typeText(app, "c")
	if app.Chat != nil || app.pendingWork != nil {
		t.Fatal("no chat box may open without a board")
	}
	if app.CopyNotice != config.Text("tui.chat_no_board") {
		t.Fatalf("notice=%q", app.CopyNotice)
	}
}

func TestChatBoxFocusFailureIsOnlyANotice(t *testing.T) {
	app, _, _ := chatApp(t, chatStarted)
	app.FocusWindow = func(context.Context, string) focus.Result {
		return focus.Result{Message: "pane is gone"}
	}
	openReadyChat(t, app)
	typeText(app, "hello")
	pressKey(app, tea.KeyMsg{Type: tea.KeyCtrlS})
	runPendingWork(t, app)
	if app.Chat != nil || app.chatDraft != "" {
		t.Fatalf("focus failure must still close the box: chat=%+v draft=%q", app.Chat, app.chatDraft)
	}
	if !strings.Contains(app.CopyNotice, "pane is gone") || !strings.Contains(app.CopyNotice, "tmux:$1:@2:%3") {
		t.Fatalf("focus failure must be a board notice: %q", app.CopyNotice)
	}
}
