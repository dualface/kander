package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/launch"
	"github.com/dualface/kander/internal/menu"
)

func TestConfirmKeyAction(t *testing.T) {
	cases := []struct {
		phase confirmPhase
		key   string
		want  confirmAction
	}{
		{confirmReady, "y", confirmAccept},
		{confirmReady, "Y", confirmAccept},
		{confirmReady, "n", confirmCancel},
		{confirmReady, "N", confirmCancel},
		{confirmReady, "esc", confirmCancel},
		{confirmReady, "enter", confirmIgnore},
		{confirmReady, "q", confirmIgnore},
		{confirmLoading, "y", confirmWait},
		{confirmLoading, "Y", confirmWait},
		{confirmLoading, "n", confirmCancel},
		{confirmLoading, "N", confirmCancel},
		{confirmLoading, "esc", confirmCancel},
		{confirmLoading, "enter", confirmIgnore},
		{confirmRunning, "y", confirmIgnore},
		{confirmRunning, "n", confirmIgnore},
		{confirmRunning, "esc", confirmIgnore},
		{confirmFinished, "x", confirmClose},
		{confirmFinished, "y", confirmClose},
		{confirmFinished, "esc", confirmClose},
	}
	for _, tc := range cases {
		if got := confirmKeyAction(tc.phase, tc.key); got != tc.want {
			t.Fatalf("phase=%d key=%q got=%d want=%d", tc.phase, tc.key, got, tc.want)
		}
	}
}

func TestConfirmReadyKeysOnStartBoardInitAndTakeover(t *testing.T) {
	start := startTestApp("todo")
	start.StartTask = func(startRequest) (launch.StartResult, error) {
		return launch.StartResult{}, nil
	}
	openStart := func() {
		start.confirmSelectedStart()
		finishStartPreview(start)
	}
	openStart()

	board := missingBoardApp(t)
	openBoard := func() {
		board.HandleKey("c")
		readyBoardInit(t, board)
	}
	openBoard()

	app, _, _ := takeoverListApp(t, func(context.Context, issue.Repository, int, issue.TriageOptions) (issue.TriageOutcome, error) {
		return issue.TriageOutcome{Agent: "claude", Launcher: "tmux"}, nil
	})
	openTakeover := func() {
		app.HandleKey("s")
		runPendingWork(t, app)
	}
	openTakeover()

	type dialog struct {
		name   string
		app    *App
		open   func() bool
		reopen func()
		accept func() bool
	}
	cases := []dialog{
		{
			name:   "start",
			app:    start,
			open:   func() bool { return start.StartConfirmation != nil },
			reopen: openStart,
			accept: func() bool {
				return start.StartConfirmation != nil && start.StartConfirmation.phase == confirmRunning
			},
		},
		{
			name:   "board-init",
			app:    board,
			open:   func() bool { return board.BoardInit != nil },
			reopen: openBoard,
			accept: func() bool {
				return board.BoardInit != nil && board.BoardInit.phase == confirmRunning
			},
		},
		{
			name:   "takeover",
			app:    app,
			open:   func() bool { return app.Takeover != nil },
			reopen: openTakeover,
			accept: func() bool {
				return app.Takeover != nil && app.Takeover.phase == confirmRunning
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range []string{"enter", "q", "s", " "} {
				tc.app.HandleKey(key)
				if !tc.open() {
					t.Fatalf("ignored %q closed the dialog", key)
				}
			}
			for _, key := range []string{"n", "N", "esc"} {
				tc.app.HandleKey(key)
				if tc.open() {
					t.Fatalf("%q must cancel", key)
				}
				tc.reopen()
			}
			tc.app.HandleKey("Y")
			if !tc.accept() {
				t.Fatal("Y must confirm")
			}
		})
	}
}

func TestConfirmLoadingAndRunningKeys(t *testing.T) {
	app := startTestApp("todo")
	app.confirmSelectedStart()
	if app.StartConfirmation.phase != confirmLoading {
		t.Fatal("expected loading")
	}
	app.HandleKey("enter")
	app.HandleKey("q")
	if app.StartConfirmation == nil {
		t.Fatal("loading ignored keys closed the dialog")
	}
	app.HandleKey("y")
	if app.StartConfirmation.phase != confirmLoading || !strings.Contains(app.CopyNotice, config.Text("dialog.loading_keys")) {
		t.Fatal("loading y must wait")
	}
	app.HandleKey("n")
	if app.StartConfirmation != nil {
		t.Fatal("loading n must cancel")
	}

	app = startTestApp("todo")
	app.StartTask = func(startRequest) (launch.StartResult, error) { return launch.StartResult{}, nil }
	app.confirmSelectedStart()
	finishStartPreview(app)
	app.HandleKey("y")
	if app.StartConfirmation.phase != confirmRunning {
		t.Fatal("expected running")
	}
	for _, key := range []string{"y", "n", "esc", "enter", "q"} {
		app.HandleKey(key)
		if app.StartConfirmation == nil || app.StartConfirmation.phase != confirmRunning {
			t.Fatalf("running key %q escaped", key)
		}
	}
}

func TestConfirmFinishedAnyKeyAndWheel(t *testing.T) {
	app := startTestApp("todo")
	app.confirmSelectedStart()
	finishStartPreview(app)
	app.StartConfirmation.finish(strings.Repeat("result line\n", 8), false)
	app.Width, app.Height = 40, 10
	app.View()
	app.HandleMouse(0, 0, mouseBtn5Pressed)
	if app.StartConfirmation == nil || app.StartConfirmation.phase != confirmFinished {
		t.Fatal("wheel closed the result")
	}
	app.HandleKey("x")
	if app.StartConfirmation != nil {
		t.Fatal("any key must close the result")
	}
}

func TestOptionsRestoreAndHerdrUseReadyConfirm(t *testing.T) {
	_, panel := openPanel(t)
	panel.current = sectionInterface
	panel.openRestoreConfirm()
	if panel.confirm == nil || panel.confirm.phase != confirmReady {
		t.Fatal("restore must open a ready-only dialog")
	}
	drivePanel(panel, keyMsg("enter"))
	if panel.confirm == nil {
		t.Fatal("enter must be ignored")
	}
	drivePanel(panel, keyMsg("n"))
	if panel.confirm != nil {
		t.Fatal("n must cancel restore")
	}

	pumpPanel(panel, panel.beginDoctor(menu.TerminalTools{}))
	if panel.confirm == nil || panel.confirmKind != optionsConfirmHerdr {
		t.Fatal("herdr must open a ready-only dialog")
	}
	drivePanel(panel, keyMsg("q"))
	if panel.confirm == nil {
		t.Fatal("other keys must be ignored")
	}
	drivePanel(panel, keyMsg("esc"))
	if panel.confirm != nil {
		t.Fatal("esc must skip herdr install")
	}
}

func TestConfirmIgnoresCtrlCOnTakeoverAndOptions(t *testing.T) {
	app, _, _ := takeoverListApp(t, func(context.Context, issue.Repository, int, issue.TriageOptions) (issue.TriageOutcome, error) {
		return issue.TriageOutcome{Agent: "claude", Launcher: "tmux"}, nil
	})
	app.HandleKey("s")
	runPendingWork(t, app)
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !app.Running || app.Takeover == nil {
		t.Fatal("ctrl-c must be ignored on the takeover dialog")
	}

	host, panel := openPanel(t)
	panel.current = sectionInterface
	panel.openRestoreConfirm()
	host.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !host.Running || panel.confirm == nil {
		t.Fatal("ctrl-c must be ignored on the options confirm")
	}
}

func TestOptionsConfirmHintUsesDialogKeys(t *testing.T) {
	_, panel := openPanel(t)
	panel.current = sectionInterface
	panel.openRestoreConfirm()
	if panel.pageHint() != config.Text("dialog.keys") {
		t.Fatalf("hint=%q", panel.pageHint())
	}
}
