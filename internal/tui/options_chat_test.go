package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/internal/config"
)

func TestExecutionSectionOrderPutsChatBeforeLauncher(t *testing.T) {
	_, panel := openPanel(t)
	pumpPanel(panel, panel.openSection(sectionExecution))
	_, view := panel.view()
	plain := ansi.Strip(view)
	large := optionTitle(uiText("tui.titles.large"))
	small := optionTitle(uiText("tui.titles.small"))
	chat := optionTitle(uiText("tui.titles.chat"))
	launcher := optionTitle(uiText("menu.launcher"))
	li, si, ci, lai := strings.Index(plain, large), strings.Index(plain, small), strings.Index(plain, chat), strings.Index(plain, launcher)
	if li < 0 || si < 0 || ci < 0 || lai < 0 {
		t.Fatalf("missing titles:\n%s", plain)
	}
	if !(li < si && si < ci && ci < lai) {
		t.Fatalf("order large=%d small=%d chat=%d launcher=%d\n%s", li, si, ci, lai, plain)
	}
	indentModel := optionTitle(modelIndent + uiText("menu.field_model"))
	if !strings.Contains(plain, indentModel) {
		t.Fatalf("chat/execution model title is not indented:\n%s", plain)
	}
}

func TestChatAgentSwitchRebuildsFieldsAndKeepsFocus(t *testing.T) {
	_, panel := openPanel(t)
	pumpPanel(panel, panel.openSection(sectionExecution))
	if panel.bind == nil {
		t.Fatal("no bind")
	}
	before := panel.bind.chat
	next := "claude"
	if before == next {
		next = "grok"
	}
	panel.bind.chat = next
	panel.bind.apply(panel)
	if panel.rebuildFocus != chatFocusKey() {
		t.Fatalf("rebuildFocus=%q", panel.rebuildFocus)
	}
	if panel.session.Config.ChatAgent != next {
		t.Fatalf("chat_agent=%s", panel.session.Config.ChatAgent)
	}
	pumpPanel(panel, panel.rebuildSection())
	if panel.bind.chat != next {
		t.Fatalf("bind.chat=%s", panel.bind.chat)
	}
	fields := panel.session.ChatModelFieldsFor()
	if len(fields) == 0 || fields[0].Agent != next {
		t.Fatalf("fields=%+v", fields)
	}
}

func TestCursorChatOmitsEffortInput(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.ChatAgent = "cursor"
	_, panel := openPanel(t, cfg)
	pumpPanel(panel, panel.openSection(sectionExecution))
	for _, field := range panel.bind.modelFields {
		if field.Kind() == "chat" && field.FieldName() == "effort" {
			t.Fatalf("cursor chat created effort: %+v", field)
		}
	}
	var chatFields int
	for _, field := range panel.bind.modelFields {
		if field.Kind() == "chat" {
			chatFields++
		}
	}
	if chatFields != 1 {
		t.Fatalf("chat fields=%d", chatFields)
	}
}
