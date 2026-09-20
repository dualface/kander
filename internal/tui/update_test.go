package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dualface/kander/internal/install"
)

func updateTestApp() *App {
	app := newApp(true, 30, tuiPageContext(), func() (BoardPayload, error) {
		return BoardPayload{}, nil
	}, func(string) (Task, error) {
		return Task{}, nil
	}, "auto", 5, nil, nil)
	app.welcomeDismissed = true
	return app
}

func TestUpdateCheckRunsSeparatelyAndDefersForOverlay(t *testing.T) {
	app := updateTestApp()
	called := false
	app.updateCheck = func(ctx context.Context) (*install.UpdateInfo, error) {
		called = true
		return &install.UpdateInfo{Current: "1.0.0", Version: "1.1.0"}, nil
	}
	cmd := app.startUpdateCheck()
	if cmd == nil {
		t.Fatal("missing update check command")
	}
	message := cmd().(updateCheckMsg)
	if !called || message.err != nil || message.info == nil {
		t.Fatalf("called=%v message=%+v", called, message)
	}
	app.Help = true
	app.receiveUpdateCheck(message.info, nil)
	if app.UpdateDialog != nil || app.pendingUpdate == nil {
		t.Fatal("update was not deferred behind help")
	}
	app.Help = false
	app.activatePendingUpdate()
	if app.UpdateDialog == nil || app.pendingUpdate != nil {
		t.Fatal("deferred update did not activate")
	}
}

func TestUpdateDialogFailureAndSuccess(t *testing.T) {
	app := updateTestApp()
	app.updateApply = func(context.Context, install.UpdateInfo) (install.UpdateResult, error) {
		return install.UpdateResult{Diagnostic: "brew output"}, errors.New("failed")
	}
	app.receiveUpdateCheck(&install.UpdateInfo{Current: "1.0.0", Version: "1.1.0"}, nil)
	cmd := app.handleUpdateKey("y")
	if cmd == nil || app.UpdateDialog.phase != confirmRunning {
		t.Fatal("update did not start")
	}
	message := cmd().(updateApplyMsg)
	app.receiveUpdateApply(message.result, message.err)
	if !app.UpdateDialog.failed || !strings.Contains(app.UpdateDialog.message, "brew output") {
		t.Fatalf("failure dialog=%+v", app.UpdateDialog)
	}
	app.handleUpdateKey("x")
	if app.UpdateDialog != nil || !app.Running {
		t.Fatal("failed result did not close")
	}

	app.receiveUpdateCheck(&install.UpdateInfo{Current: "1.0.0", Version: "1.1.0"}, nil)
	app.UpdateDialog.run()
	app.receiveUpdateApply(install.UpdateResult{Path: "/verified/kander", Version: "1.1.0"}, nil)
	if app.RestartPath != "/verified/kander" || app.UpdateDialog.failed {
		t.Fatalf("success path=%q dialog=%+v", app.RestartPath, app.UpdateDialog)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if app.Running {
		t.Fatal("successful result did not request restart")
	}
}

func TestUpdateCheckFailureIsNotice(t *testing.T) {
	app := updateTestApp()
	app.receiveUpdateCheck(nil, errors.New("offline"))
	if app.UpdateDialog != nil || !strings.Contains(app.CopyNotice, "offline") {
		t.Fatalf("dialog=%+v notice=%q", app.UpdateDialog, app.CopyNotice)
	}
}
