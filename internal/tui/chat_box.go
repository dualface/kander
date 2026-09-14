package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/internal/launch"
)

type chatPhase int

const (
	chatLoading chatPhase = iota
	chatReady
	chatUnavailable
	chatRunning
)

// chatInputRows is the preferred height of the message editor; short
// terminals shrink it so the frame still fits.
const chatInputRows = 8

// chatBox is the popup that hands one typed message to a new agent session.
// While it is non-nil it owns the input; sequence binds background results to
// the dialog that requested them.
type chatBox struct {
	sequence uint64
	phase    chatPhase
	input    textarea.Model
	agent    string
	launcher string
	// status is the last validation, failure or success line under the editor.
	status string
	failed bool
}

type chatPreviewResult struct {
	sequence uint64
	preview  launch.ChatPreview
	err      error
}

// chatStartResult carries one finished start and the focus attempt that
// followed a successful start.
type chatStartResult struct {
	sequence uint64
	result   launch.ChatResult
	err      error
	focused  bool
	focusMsg string
}

// openChat shows the chat box with the kept draft and resolves the agent and
// launcher in the background, so a slow configuration read never blocks the key.
func (a *App) openChat() {
	if a.offerBoardInit(boardInitChat) {
		return
	}
	if a.StartChat == nil {
		a.showFocusNotice(t("tui.chat_no_board"))
		return
	}
	a.chatSeq++
	input := textarea.New()
	input.ShowLineNumbers = false
	input.Prompt = ""
	input.Placeholder = t("tui.chat_placeholder")
	input.CharLimit = 0
	input.FocusedStyle.CursorLine = lipgloss.NewStyle()
	input.FocusedStyle.Base = lipgloss.NewStyle()
	input.Cursor.SetMode(cursor.CursorStatic)
	input.SetValue(a.chatDraft)
	dialog := &chatBox{sequence: a.chatSeq, phase: chatLoading, input: input}
	a.Chat = dialog
	a.resizeChat()
	// The cursor is static, so focusing returns no blink command.
	dialog.input.Focus()
	prepare := a.PrepareChat
	if prepare == nil {
		// Tests may build an App without the binding; the start still resolves
		// the configured defaults, the dialog just cannot show them in advance.
		dialog.phase = chatReady
		return
	}
	sequence := dialog.sequence
	a.pendingWork = func() any {
		preview, err := prepare()
		return chatPreviewResult{sequence: sequence, preview: preview, err: err}
	}
}

func (a *App) applyChatPreview(result chatPreviewResult) {
	dialog := a.Chat
	if dialog == nil || dialog.sequence != result.sequence || dialog.phase != chatLoading {
		return
	}
	if result.err != nil {
		dialog.phase, dialog.failed = chatUnavailable, true
		dialog.status = t("tui.chat_unavailable", result.err.Error())
		return
	}
	dialog.agent, dialog.launcher = result.preview.Agent, result.preview.Launcher
	dialog.phase = chatReady
}

// updateChat routes raw messages to the editor, which needs the full key event
// (runes, paste, Alt) that mapKey reduces away.
func (a *App) updateChat(msg tea.Msg) tea.Cmd {
	dialog := a.Chat
	key, isKey := msg.(tea.KeyMsg)
	if dialog.phase == chatRunning {
		// The start is in flight; ignoring keys keeps the dialog, and the message
		// it carries, in place until the result lands.
		return nil
	}
	if !isKey {
		if _, isMouse := msg.(tea.MouseMsg); isMouse {
			return nil
		}
		var cmd tea.Cmd
		dialog.input, cmd = dialog.input.Update(msg)
		return cmd
	}
	switch mapKey(key) {
	case "esc":
		a.chatDraft = dialog.input.Value()
		a.Chat = nil
		return nil
	case "ctrl-s":
		a.submitChat()
		return nil
	}
	var cmd tea.Cmd
	dialog.input, cmd = dialog.input.Update(msg)
	return cmd
}

func (a *App) submitChat() {
	dialog := a.Chat
	switch dialog.phase {
	case chatLoading:
		dialog.status, dialog.failed = t("tui.chat_loading_keys"), false
		return
	case chatUnavailable:
		return
	}
	message := dialog.input.Value()
	if strings.TrimSpace(message) == "" {
		dialog.status, dialog.failed = t("tui.chat_empty"), true
		return
	}
	dialog.phase, dialog.status, dialog.failed = chatRunning, "", false
	run, focusWindow, sequence := a.StartChat, a.FocusWindow, dialog.sequence
	a.pendingWork = func() any {
		result, err := run(message)
		out := chatStartResult{sequence: sequence, result: result, err: err}
		if err == nil && focusWindow != nil {
			focused := focusWindow(context.Background(), result.Address)
			out.focused, out.focusMsg = focused.Success, focused.Message
		}
		return out
	}
}

func (a *App) applyChatStart(result chatStartResult) {
	dialog := a.Chat
	if dialog == nil || dialog.sequence != result.sequence || dialog.phase != chatRunning {
		return
	}
	if result.err != nil {
		dialog.phase = chatReady
		dialog.failed = true
		dialog.status = t("tui.chat_start_failed", result.err.Error())
		return
	}
	a.chatDraft = ""
	a.Chat = nil
	// Focus already moved the user to the new session; only surface a board
	// notice when focus failed or the start returned warnings.
	var lines []string
	if !result.focused || len(result.result.Warnings) > 0 {
		lines = append(lines, t("tui.chat_started", result.result.Agent, result.result.Launcher, result.result.Address))
	}
	if !result.focused {
		lines = append(lines, t("tui.chat_focus_failed", result.focusMsg))
	}
	lines = append(lines, result.result.Warnings...)
	if len(lines) > 0 {
		a.showFocusNotice(strings.Join(lines, "\n"))
	}
}

// resizeChat fits the editor to the popup width and the screen height.
func (a *App) resizeChat() {
	dialog := a.Chat
	if dialog == nil {
		return
	}
	h, w := a.size()
	frame := a.chatFrame()
	dialog.input.SetWidth(frame.inner(w, h, 72))
	// Four rows stay outside the editor: the settings line and the blank row
	// under it, and the blank row plus the first status line below it.
	dialog.input.SetHeight(max(1, min(chatInputRows, h-frame.chrome()-4)))
}

func (a *App) chatFrame() popup {
	hint := t("tui.chat_keys")
	if a.Chat != nil && a.Chat.phase == chatRunning {
		hint = t("tui.chat_running_keys")
	}
	return popup{Title: t("tui.chat_title"), Hint: hint, TightFit: true}
}

func (a *App) renderChat() (popupBox, string) {
	dialog := a.Chat
	h, w := a.size()
	p := themePalette(a.Theme)
	frame := a.chatFrame()
	if dialog.phase == chatRunning {
		frame.Title = t("tui.start_title_starting")
	}
	inner := frame.inner(w, h, 72)
	agent, launcher := orDash(dialog.agent), orDash(dialog.launcher)
	if dialog.phase == chatLoading {
		agent, launcher = t("tui.start_loading"), t("tui.start_loading")
	}
	rows := []string{
		styleFor("popup-dim", p).Render(ansi.Truncate(t("dialog.settings", agent, launcher), inner, "…")),
		"",
		dialog.input.View(),
	}
	if dialog.status != "" {
		status := ansi.Wrap(printableText(ansi.Strip(dialog.status)), inner, "")
		style := styleFor("popup-dim", p)
		if dialog.failed {
			style = p.ink(p.Warn)
		}
		rows = append(rows, "", style.Render(status))
	}
	box, _, out := frame.render(p, w, h, inner, strings.Join(rows, "\n"))
	return box, out
}
