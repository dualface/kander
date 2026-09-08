package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/dualface/kander/internal/focus"
)

func focusTestApp() *App {
	task := Task{TaskID: "focus-task", Title: "Focus", State: "working", Document: "- WINDOW: herdr:w1:t2:w1:p3\n"}
	app := newApp(true, 30, tuiPageContext(), func() (BoardPayload, error) { return BoardPayload{Tasks: []Task{task}}, nil }, func(string) (Task, error) { return task, nil }, "dark", 1, nil, nil)
	app.Width, app.Height = 120, 30
	app.refreshBoard()
	app.Model.ColumnIndex = indexOf(app.Model.States(), "working")
	return app
}

func TestBoardFocusBackgroundResult(t *testing.T) {
	for _, tc := range []struct {
		name             string
		success, options bool
	}{
		{"success", true, false}, {"failure", false, false}, {"options-opened-during-focus", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := focusTestApp()
			calls, reads := 0, 0
			getTask := app.GetTask
			app.GetTask = func(id string) (Task, error) { reads++; return getTask(id) }
			app.FocusWindow = func(ctx context.Context, address string) focus.Result {
				calls++
				if address != "herdr:w1:t2:w1:p3" {
					t.Fatalf("address=%q", address)
				}
				return focus.Result{Success: tc.success, Message: "focus result"}
			}
			p := program{app: app}
			_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
			if cmd == nil || reads != 0 || calls != 0 || !app.focusRunning {
				t.Fatal("focus must be queued without blocking input")
			}
			_, duplicate := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
			if duplicate != nil {
				t.Fatal("duplicate focus queued")
			}
			app.HandleKey("/")
			if !app.Searching {
				t.Fatal("input blocked while focus pending")
			}
			if tc.options {
				app.Options = &optionsPanel{app: app}
			}
			p.Update(cmd())
			if reads != 1 || calls != 1 || app.focusRunning {
				t.Fatalf("reads=%d calls=%d running=%v", reads, calls, app.focusRunning)
			}
			if app.CopyNotice != "focus result" || !app.CopyNoticeUntil.After(app.Now()) {
				t.Fatalf("notice=%q", app.CopyNotice)
			}
			if !strings.Contains(ansi.Strip(app.renderBoardView()), "focus result") {
				t.Fatal("result missing from footer")
			}
		})
	}
}

func TestFocusKeyContexts(t *testing.T) {
	app := focusTestApp()
	app.HandleKey("/")
	app.HandleKey("g")
	if app.Model.Query != "g" || app.pendingWork != nil {
		t.Fatal("search g must remain input")
	}
	app.HandleKey("esc")
	app.HandleKey("enter")
	app.HandleKey("g")
	if !app.DetailPendingG || app.pendingWork != nil {
		t.Fatal("detail g must remain gg prefix")
	}
	app.HandleKey("g")
	if app.DetailPendingG || app.DetailScroll != 0 {
		t.Fatal("detail gg changed")
	}
	found := false
	for _, entry := range boardHelpGroups()[0].Entries {
		if entry.Keys == "g" {
			found = entry.Desc != ""
		}
	}
	if !found {
		t.Fatal("board help missing focus key")
	}
}

func TestFocusMissingTask(t *testing.T) {
	app := focusTestApp()
	app.GetTask = func(string) (Task, error) { return Task{}, errors.New("task removed") }
	app.FocusWindow = func(context.Context, string) focus.Result {
		t.Fatal("unreadable task must not focus")
		return focus.Result{}
	}
	app.HandleKey("g")
	app.applyWork(app.takePending()().(workMsg).payload)
	if app.focusRunning || !strings.Contains(app.CopyNotice, "task removed") {
		t.Fatalf("notice=%q", app.CopyNotice)
	}
	app.Model.SetBoard(BoardPayload{})
	app.HandleKey("g")
	if app.pendingWork != nil || app.CopyNotice == "" {
		t.Fatal("empty selection must show notice")
	}
}
