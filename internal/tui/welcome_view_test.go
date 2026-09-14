package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/i18n"
	"github.com/dualface/kander/internal/launch"
)

func emptyBoardApp(t *testing.T) *App {
	t.Helper()
	app := newApp(true, 30, tuiPageContext(),
		func() (BoardPayload, error) { return BoardPayload{}, nil },
		func(string) (Task, error) { return Task{}, errors.New("no detail") },
		"dark", 1, nil, nil)
	app.Width, app.Height = 120, 28
	app.StartChat = func(string) (launch.ChatResult, error) {
		return launch.ChatResult{Agent: "codex", Launcher: "tmux", Address: "tmux:a"}, nil
	}
	return app
}

func welcomeView(app *App) string {
	return ansi.Strip(app.View())
}

func TestEmptyBoardShowsWelcome(t *testing.T) {
	app := emptyBoardApp(t)
	if !app.shouldShowWelcome() {
		t.Fatal("empty board should show welcome")
	}
	view := welcomeView(app)
	for _, want := range []string{
		config.Text("tui.welcome_title"),
		config.Text("tui.welcome_tip_title"),
		config.Text("tui.welcome_hint"),
		prefixRunes(config.Text("tui.welcome_intro"), 16),
		prefixRunes(config.Text("tui.welcome_item_chat"), 16),
		prefixRunes(config.Text("tui.welcome_item_issues"), 16),
		prefixRunes(config.Text("tui.welcome_item_help"), 16),
		prefixRunes(config.Text("tui.welcome_item_options"), 16),
		prefixRunes(config.Text("tui.welcome_tip"), 16),
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("welcome missing %q\n%s", want, view)
		}
	}
}

func prefixRunes(text string, n int) string {
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return string(runes[:n])
}

func TestWelcomeHiddenWhenBoardHasCards(t *testing.T) {
	app := emptyBoardApp(t)
	for _, state := range []string{"todo", "archived", "trash"} {
		app.Model.SetBoard(BoardPayload{Tasks: []Task{{TaskID: "card-" + state, State: state}}})
		if app.shouldShowWelcome() {
			t.Fatalf("welcome must stay closed when a %s card exists", state)
		}
		if strings.Contains(welcomeView(app), config.Text("tui.welcome_title")) {
			t.Fatalf("welcome title shown with %s card:\n%s", state, welcomeView(app))
		}
	}
}

func TestWelcomeWaitsUntilOptionsClose(t *testing.T) {
	app := emptyBoardApp(t)
	app.Options = &optionsPanel{app: app}
	if app.shouldShowWelcome() {
		t.Fatal("options must cover welcome")
	}
	app.Options.close()
	if !app.shouldShowWelcome() {
		t.Fatal("welcome should open after options close on an empty board")
	}
}

func TestWelcomeShortcutsStillWork(t *testing.T) {
	app := emptyBoardApp(t)
	app.HandleKey("c")
	if app.Chat == nil {
		t.Fatal("c should open chat")
	}
	if app.shouldShowWelcome() {
		t.Fatal("chat should cover welcome")
	}
	app.Chat = nil

	app.HandleKey("g")
	if app.Issues == nil {
		t.Fatal("g should open issues")
	}
	app.closeIssues()
	if !app.shouldShowWelcome() {
		t.Fatal("welcome should return after issues close")
	}

	app.HandleKey("?")
	if !app.Help || app.shouldShowWelcome() {
		t.Fatal("? should open help over welcome")
	}
	app.HandleKey("x")
	if app.Help || !app.shouldShowWelcome() {
		t.Fatal("closing help should restore welcome")
	}

	app.HandleKey("o")
	if app.Options == nil || app.shouldShowWelcome() {
		t.Fatal("o should open options over welcome")
	}
	app.Options.close()
	if !app.shouldShowWelcome() {
		t.Fatal("welcome should return after options close")
	}

	app.HandleKey("/")
	if app.Searching {
		t.Fatal("unlisted keys must not reach the board while welcome is open")
	}
}

func TestWelcomeDismissStaysClosedUntilRestart(t *testing.T) {
	app := emptyBoardApp(t)
	app.HandleKey("esc")
	if app.shouldShowWelcome() || !app.welcomeDismissed {
		t.Fatal("Esc should dismiss welcome for this process")
	}
	app.HandleKey("?")
	app.HandleKey("x")
	if app.shouldShowWelcome() {
		t.Fatal("dismissed welcome must not return after help")
	}

	clicked := emptyBoardApp(t)
	clicked.HandleMouse(10, 10, mouseBtn1Clicked)
	if clicked.shouldShowWelcome() {
		t.Fatal("click should dismiss welcome")
	}
}

func TestWelcomeHidesAfterRefreshAddsCards(t *testing.T) {
	app := emptyBoardApp(t)
	app.GetBoard = func() (BoardPayload, error) {
		return BoardPayload{Tasks: []Task{{TaskID: "new-card", State: "todo"}}}, nil
	}
	if !app.refreshBoard() {
		t.Fatal("refresh should load the new card")
	}
	if app.shouldShowWelcome() {
		t.Fatal("welcome must hide after cards appear")
	}
}

func TestWelcomeFollowsUILanguageNotAgentLanguage(t *testing.T) {
	t.Cleanup(func() { config.BindConfigLanguage(nil) })
	app := emptyBoardApp(t)
	config.BindConfigLanguage(&config.Config{Language: "en", AgentLanguage: "ja"})
	if view := welcomeView(app); !strings.Contains(view, "Welcome to Kander") || strings.Contains(view, "Kander へようこそ") {
		t.Fatalf("UI language en should win over agent language ja:\n%s", view)
	}
	config.BindConfigLanguage(&config.Config{Language: "ja", AgentLanguage: "en"})
	if view := welcomeView(app); !strings.Contains(view, "Kander へようこそ") {
		t.Fatalf("UI language ja missing:\n%s", view)
	}
}

func TestWelcomeCopyMatchesLockedCopy(t *testing.T) {
	want := map[string]map[string]string{
		"cn": {
			"tui.welcome_title":        "欢迎使用 Kander",
			"tui.welcome_intro":        "用看板调度多个 AI Agent。当前还没有任务卡，用以下方式开始：",
			"tui.welcome_item_chat":    "1. 按 `c` 打开对话：开始与 Agent 讨论要处理的任务",
			"tui.welcome_item_issues":  "2. 按 `g` 浏览 GitHub Issue，查看需要处理的 Issue",
			"tui.welcome_item_help":    "3. 按 `?` 查看可用的快捷键",
			"tui.welcome_item_options": "4. 按 `o` 打开选项对话框",
			"tui.welcome_tip_title":    "提示",
			"tui.welcome_tip":          "首次使用前建议先配置要使用的 Agent",
		},
		"en": {
			"tui.welcome_title":        "Welcome to Kander",
			"tui.welcome_intro":        "Coordinate multiple AI agents on one board. There are no task cards yet. Start here:",
			"tui.welcome_item_chat":    "1. Press `c` to open chat and discuss the work with an agent",
			"tui.welcome_item_issues":  "2. Press `g` to browse GitHub issues and see what needs attention",
			"tui.welcome_item_help":    "3. Press `?` to view available shortcuts",
			"tui.welcome_item_options": "4. Press `o` to open the options panel",
			"tui.welcome_tip_title":    "Tip",
			"tui.welcome_tip":          "Before your first run, configure the agent you want to use",
		},
		"ja": {
			"tui.welcome_title":        "Kander へようこそ",
			"tui.welcome_intro":        "カンバンで複数の AI Agent をまとめて進めます。まだタスクカードはありません。次の方法で始められます：",
			"tui.welcome_item_chat":    "1. `c` でチャットを開き、Agent とこれから扱うタスクを相談する",
			"tui.welcome_item_issues":  "2. `g` で GitHub Issue を閲覧し、対応が必要な Issue を確認する",
			"tui.welcome_item_help":    "3. `?` で利用可能なショートカットを表示する",
			"tui.welcome_item_options": "4. `o` でオプションを開く",
			"tui.welcome_tip_title":    "ヒント",
			"tui.welcome_tip":          "初めて使う前に、利用する Agent を設定することをおすすめします",
		},
	}
	for lang, keys := range want {
		for id, text := range keys {
			if got := i18n.Text(lang, id); got != text {
				t.Errorf("%s %s = %q, want %q", lang, id, got, text)
			}
		}
	}
}

func TestWelcomeOverlayFillsBackground(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(profile)
	for _, theme := range []string{"light", "dark"} {
		app := emptyBoardApp(t)
		app.Theme = theme
		app.Width, app.Height = 80, 24
		expectFilled(t, theme+" welcome", app.View())
	}
}
