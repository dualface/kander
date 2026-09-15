package tui

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func refreshTestApp(getBoard func() (BoardPayload, error), getTask func(string) (Task, error)) *App {
	if getBoard == nil {
		getBoard = func() (BoardPayload, error) {
			return BoardPayload{Tasks: []Task{{TaskID: "20260915-one-task", Title: "one", State: "todo"}}}, nil
		}
	}
	if getTask == nil {
		getTask = func(id string) (Task, error) {
			return Task{TaskID: id, Title: id, State: "todo", Document: "body"}, nil
		}
	}
	app := newApp(true, 1, pageContext{}, getBoard, getTask, "auto", 40, nil, nil)
	app.Width, app.Height = 100, 24
	app.Now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	app.LastRefresh = app.Now().Add(-time.Hour)
	app.Model.SetBoard(BoardPayload{Tasks: []Task{{TaskID: "20260915-one-task", Title: "one", State: "todo"}}})
	return app
}

func TestBoardRefreshDoesNotBlockUpdate(t *testing.T) {
	var queued atomic.Int32
	tickApp := refreshTestApp(func() (BoardPayload, error) {
		queued.Add(1)
		return BoardPayload{Tasks: []Task{{TaskID: "20260915-one-task", Title: "one", State: "todo"}}}, nil
	}, nil)
	program{app: tickApp}.Update(tickMsg(tickApp.Now()))
	if queued.Load() != 0 || tickApp.boardInFlightSeq == 0 {
		t.Fatal("tick Update must queue without reading")
	}

	started, release := make(chan struct{}), make(chan struct{})
	app := refreshTestApp(func() (BoardPayload, error) {
		close(started)
		<-release
		return BoardPayload{GeneratedAt: "new", Tasks: []Task{{TaskID: "20260915-one-task", Title: "updated", State: "todo"}}}, nil
	}, nil)
	p := program{app: app}
	app.requestBoardRefresh(true)
	cmd := app.takePending()
	if cmd == nil {
		t.Fatal("board read was not started")
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	<-started
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	p.Update(tea.MouseMsg{X: 2, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	p.Update(tea.WindowSizeMsg{Width: 90, Height: 26})
	if !app.Searching || app.Width != 90 {
		t.Fatal("blocked read captured input or resize")
	}
	close(release)
	p.Update(<-done)
	if app.Model.Tasks[0].Title != "updated" {
		t.Fatal("fresh snapshot was not applied")
	}
}

func TestBoardRefreshDropsStaleResultsAndKeepsLastData(t *testing.T) {
	var which atomic.Int32
	app := refreshTestApp(func() (BoardPayload, error) {
		n := which.Add(1)
		if n == 1 {
			return BoardPayload{Tasks: []Task{{TaskID: "20260915-one-task", Title: "stale", State: "backlog"}}}, nil
		}
		return BoardPayload{Tasks: []Task{{TaskID: "20260915-one-task", Title: "fresh", State: "todo"}}}, nil
	}, nil)
	app.Model.SetBoard(BoardPayload{Tasks: []Task{{TaskID: "20260915-one-task", Title: "original", State: "todo"}}})
	app.requestBoardRefresh(true)
	old := app.takePending()
	app.requestBoardRefresh(true)
	if !app.boardReadQueued {
		t.Fatal("second request must coalesce")
	}
	app.applyWork(old().(workMsg).payload)
	if app.Model.Tasks[0].Title == "stale" {
		t.Fatal("stale in-flight result replaced the board")
	}
	finishQueuedWork(t, app)
	if app.Model.Tasks[0].Title != "fresh" || app.Model.Tasks[0].State != "todo" {
		t.Fatalf("want fresh todo, got %+v", app.Model.Tasks[0])
	}

	app.GetBoard = func() (BoardPayload, error) { return BoardPayload{}, errors.New("read failed") }
	previous := app.Model.Tasks[0]
	app.requestBoardRefresh(true)
	finishQueuedWork(t, app)
	if app.Model.RefreshError == "" || app.Model.Tasks[0].Title != previous.Title || app.Model.Tasks[0].State != previous.State {
		t.Fatal("failed read must keep the last snapshot")
	}
}

func TestDetailSwitchDropsPreviousRead(t *testing.T) {
	first, second := make(chan struct{}), make(chan struct{})
	app := refreshTestApp(nil, func(id string) (Task, error) {
		if id == "20260915-one-task" {
			<-first
			return Task{TaskID: id, Title: "one-body", Document: "old"}, nil
		}
		<-second
		return Task{TaskID: id, Title: "two-body", Document: "new"}, nil
	})
	app.Model.SetBoard(BoardPayload{Tasks: []Task{
		{TaskID: "20260915-one-task", Title: "one", State: "todo"},
		{TaskID: "20260915-two-task", Title: "two", State: "todo"},
	}})
	app.Model.FocusState("todo")
	app.openDetail()
	old := app.takePending()
	app.Model.MoveTask(1)
	app.openDetail()
	close(first)
	app.applyWork(old().(workMsg).payload)
	if app.Detail != nil && app.Detail.Document == "old" {
		t.Fatal("previous card body landed on the new selection")
	}
	close(second)
	finishQueuedWork(t, app)
	if app.Detail == nil || app.Detail.TaskID != "20260915-two-task" || app.Detail.Document != "new" {
		t.Fatalf("detail %+v", app.Detail)
	}
}

func TestWriteInFlightSkipsUIBoardRead(t *testing.T) {
	app := refreshTestApp(func() (BoardPayload, error) {
		t.Fatal("UI refresh must not read during write")
		return BoardPayload{}, nil
	}, nil)
	app.TaskActions = &taskActions{id: "20260915-one-task", running: true}
	app.requestBoardRefresh(true)
	if cmd := app.takePending(); cmd != nil {
		t.Fatal("queued board read during write")
	}
	p := program{app: app}
	p.Update(tickMsg(app.Now()))
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if app.refreshBoard() {
		t.Fatal("sync refresh ran during write")
	}
}

func TestQuitCancelsInFlightBoardRead(t *testing.T) {
	started := make(chan struct{})
	finished := make(chan struct{})
	app := refreshTestApp(nil, nil)
	app.GetBoardCtx = func(ctx context.Context) (BoardPayload, error) {
		close(started)
		<-ctx.Done()
		close(finished)
		return BoardPayload{}, ctx.Err()
	}
	app.requestBoardRefresh(true)
	cmd := app.takePending()
	go func() { _ = cmd() }()
	<-started
	app.requestQuit()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("quit left the board reader running")
	}
	if app.Running {
		t.Fatal("quit did not stop the app")
	}
}

func TestPendingWorkStillRunsBesideBoardRefresh(t *testing.T) {
	app := refreshTestApp(func() (BoardPayload, error) {
		return BoardPayload{Tasks: []Task{{TaskID: "20260915-one-task", Title: "board", State: "todo"}}}, nil
	}, nil)
	ran := false
	app.pendingWork = func() any {
		ran = true
		return focusResult{message: "kept"}
	}
	app.requestBoardRefresh(true)
	finishQueuedWork(t, app)
	if !ran || app.CopyNotice != "kept" || app.Model.Tasks[0].Title != "board" {
		t.Fatalf("ran=%v notice=%q tasks=%v", ran, app.CopyNotice, app.Model.Tasks)
	}
}
