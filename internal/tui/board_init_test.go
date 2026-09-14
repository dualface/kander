package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/i18n"
	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/launch"
)

func missingBoardApp(t *testing.T) *App {
	t.Helper()
	app := emptyBoardApp(t)
	app.StartChat = nil
	app.missingBoard = true
	app.PreviewBoardInit = func() (string, error) {
		return "/tmp/project/kanban", nil
	}
	app.InitBoard = func() (string, error) {
		return "/tmp/project/kanban", nil
	}
	app.AttachBoard = func(root string) {
		app.boardRoot = root
		app.missingBoard = false
		app.PrepareChat = func() (launch.ChatPreview, error) {
			return launch.ChatPreview{Agent: "codex", Launcher: "tmux"}, nil
		}
		app.StartChat = func(string) (launch.ChatResult, error) {
			return launch.ChatResult{Agent: "codex", Launcher: "tmux", Address: "tmux:a"}, nil
		}
	}
	return app
}

func readyBoardInit(t *testing.T, app *App) {
	t.Helper()
	if app.BoardInit == nil || app.BoardInit.phase != confirmLoading {
		t.Fatalf("expected loading init dialog: %+v", app.BoardInit)
	}
	runPendingWork(t, app)
	if app.BoardInit == nil || app.BoardInit.phase != confirmReady || app.BoardInit.path != "/tmp/project/kanban" {
		t.Fatalf("preview not applied: %+v", app.BoardInit)
	}
}

func TestBoardInitCopyMatchesLockedCopy(t *testing.T) {
	want := map[string]map[string]string{
		"cn": {
			"tui.board_init_title": "初始化看板？",
			"dialog.keys":          "y：确认；n / Esc：取消",
			"tui.board_init_body":  "当前还没有 kanban/ 目录。所有任务卡文件都会保存在这个目录中：\n\nPATH\n\n要现在创建吗？",
		},
		"en": {
			"tui.board_init_title": "Initialize the board?",
			"dialog.keys":          "y: confirm; n / Esc: cancel",
			"tui.board_init_body":  "There is no kanban/ directory yet. All task card files will be stored in this directory:\n\nPATH\n\nCreate it now?",
		},
		"ja": {
			"tui.board_init_title": "ボードを初期化しますか？",
			"dialog.keys":          "y：確認；n / Esc：キャンセル",
			"tui.board_init_body":  "まだ kanban/ ディレクトリがありません。タスクカードのファイルはすべてこのディレクトリに保存されます：\n\nPATH\n\n今すぐ作成しますか？",
		},
	}
	for lang, keys := range want {
		for id, text := range keys {
			got := i18n.Text(lang, id)
			if id == "tui.board_init_body" {
				got = i18n.Text(lang, id, "PATH")
			}
			if got != text {
				t.Errorf("%s %s = %q, want %q", lang, id, got, text)
			}
		}
	}
}

func TestChatWithoutBoardOffersInitThenOpensChat(t *testing.T) {
	app := missingBoardApp(t)
	app.HandleKey("c")
	if app.Chat != nil {
		t.Fatal("chat must wait for board init")
	}
	if !strings.Contains(ansi.Strip(app.View()), config.Text("dialog.loading")) {
		t.Fatal("init dialog must cover welcome while the path loads")
	}
	readyBoardInit(t, app)
	view := ansi.Strip(app.View())
	for _, want := range []string{
		config.Text("tui.board_init_title"),
		"/tmp/project/kanban",
		config.Text("dialog.keys"),
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("init dialog missing %q\n%s", want, view)
		}
	}
	if strings.Contains(view, config.Text("tui.welcome_title")) {
		t.Fatal("welcome must stay under the init dialog")
	}

	app.HandleKey("y")
	if app.BoardInit == nil || app.BoardInit.phase != confirmRunning {
		t.Fatalf("y must start init: %+v", app.BoardInit)
	}
	runPendingWork(t, app)
	if app.BoardInit != nil {
		t.Fatal("successful init must close the dialog")
	}
	if app.missingBoard || app.boardRoot != "/tmp/project/kanban" {
		t.Fatalf("board was not attached: missing=%v root=%q", app.missingBoard, app.boardRoot)
	}
	if app.Chat == nil || app.Chat.phase != chatLoading {
		t.Fatalf("chat should continue after init: %+v", app.Chat)
	}
	runPendingWork(t, app)
	if app.Chat.phase != chatReady {
		t.Fatalf("chat preview not applied: %+v", app.Chat)
	}
}

func TestBoardInitEnterIsIgnoredAndEscCancels(t *testing.T) {
	app := missingBoardApp(t)
	app.HandleKey("c")
	readyBoardInit(t, app)
	app.HandleKey("esc")
	if app.BoardInit != nil || !app.missingBoard || app.Chat != nil {
		t.Fatal("esc must cancel without creating a board")
	}

	app.HandleKey("c")
	readyBoardInit(t, app)
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.BoardInit == nil || app.BoardInit.phase != confirmReady {
		t.Fatalf("enter must be ignored: %+v", app.BoardInit)
	}
}

func TestBoardInitFailureStaysInDialog(t *testing.T) {
	app := missingBoardApp(t)
	app.InitBoard = func() (string, error) { return "", errors.New("disk full") }
	app.HandleKey("c")
	readyBoardInit(t, app)
	app.HandleKey("y")
	runPendingWork(t, app)
	if app.BoardInit == nil || !app.BoardInit.failed || !app.missingBoard {
		t.Fatalf("failure must keep the dialog: %+v missing=%v", app.BoardInit, app.missingBoard)
	}
	if !strings.Contains(ansi.Strip(app.View()), "disk full") {
		t.Fatal("failure must show the init error")
	}
	app.HandleKey("esc")
	if app.BoardInit != nil {
		t.Fatal("any key must close the failed dialog")
	}
}

func TestBoardInitDropsStaleResults(t *testing.T) {
	app := missingBoardApp(t)
	app.HandleKey("c")
	stalePreview := app.pendingWork
	app.pendingWork = nil
	app.HandleKey("esc")
	app.applyWork(stalePreview())
	if app.BoardInit != nil {
		t.Fatal("stale preview must not reopen a cancelled dialog")
	}

	app.HandleKey("c")
	readyBoardInit(t, app)
	app.HandleKey("y")
	staleInit := app.pendingWork
	app.pendingWork = nil
	app.BoardInit = nil
	app.applyWork(staleInit())
	if !app.missingBoard || app.StartChat != nil {
		t.Fatal("stale init must not attach a board after the dialog is gone")
	}
}

func TestBoardInitPreviewErrorShowsNotice(t *testing.T) {
	app := missingBoardApp(t)
	app.PreviewBoardInit = func() (string, error) { return "", errors.New("no cwd") }
	app.HandleKey("c")
	runPendingWork(t, app)
	if app.BoardInit != nil || app.Chat != nil {
		t.Fatal("preview failure must not keep a dialog")
	}
	if !strings.Contains(app.CopyNotice, "no cwd") {
		t.Fatalf("notice=%q", app.CopyNotice)
	}
}

func TestIssuesImportWithoutBoardOffersInitThenImports(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	imported := false
	app, calls := issuesImportApp(t, fake, nil, nil, func(context.Context, issue.Repository, int, issue.ImportOptions) (issue.ImportResult, error) {
		imported = true
		return issue.ImportResult{TaskID: "task-42", State: "backlog"}, nil
	})
	app.missingBoard = true
	app.PreviewBoardInit = func() (string, error) { return "/tmp/project/kanban", nil }
	app.InitBoard = func() (string, error) { return "/tmp/project/kanban", nil }
	app.AttachBoard = func(root string) {
		app.boardRoot = root
		app.missingBoard = false
	}
	app.HandleKey("g")
	runPendingWork(t, app)
	app.HandleKey("i")
	if imported || len(*calls) != 0 {
		t.Fatal("import must wait for board init")
	}
	readyBoardInit(t, app)
	app.HandleKey("y")
	runPendingWork(t, app)
	if app.BoardInit != nil || !app.Issues.importing {
		t.Fatalf("import should continue after init: dialog=%v importing=%v", app.BoardInit, app.Issues != nil && app.Issues.importing)
	}
	runPendingWork(t, app)
	if !imported || len(*calls) != 1 || (*calls)[0].number != 42 {
		t.Fatalf("import not resumed: imported=%v calls=%v", imported, *calls)
	}
}

func TestIssuesTakeoverWithoutBoardOffersInitThenStarts(t *testing.T) {
	started := false
	app, fake, calls := takeoverListApp(t, func(context.Context, issue.Repository, int, issue.TriageOptions) (issue.TriageOutcome, error) {
		started = true
		return issue.TriageOutcome{Agent: "claude", Launcher: "tmux", Address: "tmux:a"}, nil
	})
	_ = fake
	app.missingBoard = true
	app.PreviewBoardInit = func() (string, error) { return "/tmp/project/kanban", nil }
	app.InitBoard = func() (string, error) { return "/tmp/project/kanban", nil }
	app.AttachBoard = func(root string) {
		app.boardRoot = root
		app.missingBoard = false
	}
	app.HandleKey("s")
	if started || app.Takeover != nil {
		t.Fatal("takeover must wait for board init")
	}
	readyBoardInit(t, app)
	app.HandleKey("y")
	runPendingWork(t, app)
	if app.BoardInit != nil || app.Takeover == nil {
		t.Fatalf("takeover should continue after init: dialog=%v takeover=%v", app.BoardInit, app.Takeover)
	}
	runPendingWork(t, app)
	app.HandleKey("y")
	runPendingWork(t, app)
	if !started || len(*calls) != 1 {
		t.Fatalf("takeover not resumed: started=%v calls=%v", started, *calls)
	}
}
