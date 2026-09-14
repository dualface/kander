package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/focus"
)

func actionTestSource(t *testing.T, state string, ready bool) taskActionSource {
	t.Helper()
	root := t.TempDir()
	for _, state := range board.States {
		if err := os.Mkdir(filepath.Join(root, state), 0700); err != nil {
			t.Fatal(err)
		}
	}
	const id = "20260914-action-task"
	path := filepath.Join(root, state, id)
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	text := "# Task actions\n\n- TYPE: Feature\n- SIZE: small\n- LANGUAGE: en\n- WINDOW: herdr:w1:t2:w1:p3\n- RESULT: "
	if state == "done" {
		text += "completed"
	}
	text += "\n\n## GOAL\n\nGoal\n\n## EXPECTED_OUTCOME\n\nOutcome\n\n## ACCEPTANCE_CRITERIA\n\n- [ ] Test\n\n## OUT_OF_SCOPE\n\nOther work\n\n## DISCUSSION\n\nSELF_REVIEW: passed\n\n## SUMMARY\n\nCompleted\n"
	if !ready {
		text = strings.Replace(text, "Goal", "<FILL_IN>", 1)
	}
	if err := os.WriteFile(filepath.Join(path, "spec.md"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	return taskActionSource{root: root, snapshot: snapshot}
}

func actionTestApp(t *testing.T, source taskActionSource) *App {
	t.Helper()
	app := startTestApp(source.snapshot.Entry.State)
	app.GetBoard = func() (BoardPayload, error) { return board.BoardPayload(source.root) }
	app.GetTask = func(id string) (Task, error) { return board.TaskPayload(source.root, id) }
	app.LoadTaskActions = func(id string) (taskActionSource, error) {
		snapshot, err := board.ReadSnapshot(source.root, id)
		return taskActionSource{root: source.root, snapshot: snapshot}, err
	}
	app.refreshBoard()
	return app
}

func chooseTaskAction(t *testing.T, app *App, action taskAction) tea.Cmd {
	t.Helper()
	app.Update(keyMsg("m"))
	if app.TaskActions == nil {
		t.Fatal("menu did not open")
	}
	runPendingWork(t, app)
	if app.TaskActions == nil {
		t.Fatal("menu did not load")
	}
	for i, item := range app.TaskActions.items {
		if item == action {
			app.TaskActions.cursor = i
			return app.Update(keyMsg("enter"))
		}
	}
	t.Fatalf("missing action %s", action)
	return nil
}

func pumpActionForm(app *App, cmd tea.Cmd) {
	for i := 0; i < 24 && cmd != nil && app.TaskActions != nil; i++ {
		msg, ok := runCmd(cmd)
		if !ok || msg == nil {
			return
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, item := range batch {
				pumpActionForm(app, item)
			}
			return
		}
		cmd = app.Update(msg)
	}
}

func TestTaskActionsAvailability(t *testing.T) {
	expected := map[string][]taskAction{
		"backlog": {actionStart, actionPick, actionArchive, actionTrash},
		"todo":    {actionStart, actionBacklog, actionArchive, actionTrash},
		"working": nil, "review": nil, "done": {actionArchive}, "archived": nil, "trash": nil,
	}
	for state, want := range expected {
		for _, window := range []string{"", "herdr:w1:t2:w1:p3"} {
			t.Run(state+"/"+window, func(t *testing.T) {
				expected := append([]taskAction(nil), want...)
				if window != "" {
					expected = append(expected, actionFocus)
				}
				if got := availableTaskActions(state, window); !reflect.DeepEqual(got, expected) {
					t.Fatalf("got %v want %v", got, expected)
				}
			})
		}
	}
}

func TestTaskActionsShortcutContexts(t *testing.T) {
	app := actionTestApp(t, actionTestSource(t, "backlog", false))
	app.Update(keyMsg("m"))
	runPendingWork(t, app)
	if app.TaskActions == nil || app.TaskActions.loading {
		t.Fatal("m must open the loaded task actions menu")
	}
	app.Update(keyMsg("m"))
	if app.TaskActions != nil || app.pendingWork != nil {
		t.Fatal("m must close the task actions menu")
	}
	app.Update(keyMsg("/"))
	app.Update(keyMsg("m"))
	if app.Model.Query != "m" || app.TaskActions != nil || app.pendingWork != nil {
		t.Fatal("search m must remain input")
	}
}

func TestTaskActionsEmptyAndStaleLoads(t *testing.T) {
	app := startTestApp("working")
	app.Model.SetBoard(BoardPayload{})
	app.HandleKey("esc")
	app.Update(keyMsg("m"))
	if app.TaskActions != nil || app.pendingWork != nil || app.CopyNotice != tuiText("actions.no_selection") {
		t.Fatal("empty selection opened menu")
	}
	app = startTestApp("archived")
	app.LoadTaskActions = func(string) (taskActionSource, error) {
		return taskActionSource{snapshot: board.Snapshot{Entry: board.Entry{State: "archived"}}}, nil
	}
	app.Update(keyMsg("m"))
	runPendingWork(t, app)
	if app.TaskActions != nil || app.CopyNotice != tuiText("actions.none") {
		t.Fatal("empty menu remained open")
	}
	app.Update(keyMsg("m"))
	stale := app.takePending()
	app.Update(keyMsg("esc"))
	app.Update(keyMsg("m"))
	current := app.TaskActions
	app.applyWork(stale().(workMsg).payload)
	if app.TaskActions != current || !current.loading {
		t.Fatal("stale load replaced dialog")
	}
	app.LoadTaskActions = func(string) (taskActionSource, error) { return taskActionSource{}, errors.New("read failed") }
	app.Update(keyMsg("esc"))
	runPendingWork(t, app)
	app.Update(keyMsg("m"))
	runPendingWork(t, app)
	if app.TaskActions != nil || app.CopyNotice != "read failed" {
		t.Fatal("load failure hidden")
	}
}

// tuiText avoids shadowing the package's t translation helper in tests.
func tuiText(id string) string { return t(id) }

func TestTaskActionsReuseStartAndFocus(t *testing.T) {
	source := actionTestSource(t, "todo", true)
	app := actionTestApp(t, source)
	app.PrepareStart = func(id string) (startRequest, error) { return startRequest{}, errors.New("preview reached " + id) }
	chooseTaskAction(t, app, actionStart)
	if app.TaskActions != nil || app.StartConfirmation == nil || app.pendingWork == nil {
		t.Fatal("start confirmation not reused")
	}
	app = actionTestApp(t, source)
	called := false
	app.FocusWindow = func(_ context.Context, address string) focus.Result {
		called = true
		if address != "herdr:w1:t2:w1:p3" {
			t.Fatal(address)
		}
		return focus.Result{Success: true, Message: "focused"}
	}
	chooseTaskAction(t, app, actionFocus)
	if called || !app.focusRunning {
		t.Fatal("focus was not queued")
	}
	runPendingWork(t, app)
	if !called || app.CopyNotice != "focused" {
		t.Fatal("focus flow not reused")
	}
	for _, key := range []string{"s", "f", "F"} {
		app = actionTestApp(t, source)
		app.Update(keyMsg(key))
		if app.TaskActions != nil || app.StartConfirmation != nil || app.pendingWork != nil {
			t.Fatalf("old key %s still active", key)
		}
	}
}

func TestTaskActionMoves(t *testing.T) {
	for _, tc := range []struct {
		state  string
		action taskAction
		ready  bool
		target string
	}{
		{"backlog", actionPick, true, "todo"}, {"backlog", actionPick, false, "backlog"}, {"todo", actionBacklog, true, "backlog"},
	} {
		t.Run(tc.state+"/"+string(tc.action)+"/"+tc.target, func(t *testing.T) {
			source := actionTestSource(t, tc.state, tc.ready)
			app := actionTestApp(t, source)
			chooseTaskAction(t, app, tc.action)
			before, _ := board.ReadSnapshot(source.root, source.snapshot.Entry.TaskID)
			if before.Entry.State != tc.state || !app.TaskActions.running {
				t.Fatal("move ran on UI goroutine")
			}
			work := app.takePending()
			result := work().(workMsg).payload.(taskActionResult)
			app.applyWork(result)
			after, _ := board.ReadSnapshot(source.root, source.snapshot.Entry.TaskID)
			if after.Entry.State != tc.target || app.TaskActions != nil {
				t.Fatalf("move result: %s", after.Entry.State)
			}
			if !tc.ready && (result.err == nil || !strings.Contains(app.CopyNotice, result.err.Error())) {
				t.Fatal("board error not passed through")
			}
			if tc.ready && (result.err != nil || app.Model.Tasks[0].State != tc.target || app.CopyNotice == "") {
				t.Fatal("successful move not refreshed")
			}
		})
	}
	// A changed card rejects the stale snapshot, including a reverse move.
	source := actionTestSource(t, "todo", true)
	app := actionTestApp(t, source)
	chooseTaskAction(t, app, actionBacklog)
	if err := board.UpdateDocument(source.root, source.snapshot.Entry.TaskID, board.UpdateOptions{Document: "spec.md", Text: source.snapshot.Text + "\nNew note\n", ExpectedRevision: source.snapshot.Revision}); err != nil {
		t.Fatal(err)
	}
	runPendingWork(t, app)
	after, _ := board.ReadSnapshot(source.root, source.snapshot.Entry.TaskID)
	if after.Entry.State != "todo" || app.CopyNotice == "" {
		t.Fatal("stale reverse move was accepted")
	}
}

func TestTaskActionLifecycleForms(t *testing.T) {
	for _, tc := range []struct {
		state  string
		action taskAction
		result string
	}{
		{"backlog", actionArchive, "cancelled"}, {"todo", actionArchive, "duplicate"}, {"backlog", actionArchive, "wontfix"}, {"done", actionArchive, "completed"},
		{"backlog", actionTrash, "trashed"}, {"todo", actionTrash, "trashed"},
	} {
		t.Run(tc.state+"/"+tc.result, func(t *testing.T) {
			source := actionTestSource(t, tc.state, true)
			app := actionTestApp(t, source)
			pumpActionForm(app, chooseTaskAction(t, app, tc.action))
			dialog := app.TaskActions
			if dialog == nil || dialog.form == nil || app.pendingWork != nil {
				t.Fatal("missing lifecycle form")
			}
			send := func(key string) { pumpActionForm(app, app.Update(keyMsg(key))) }
			if tc.result == "cancelled" || tc.result == "duplicate" || tc.result == "wontfix" {
				count := map[string]int{"cancelled": 0, "duplicate": 1, "wontfix": 2}[tc.result]
				for i := 0; i < count; i++ {
					send("down")
				}
				send("enter")
			}
			if tc.result == "duplicate" {
				send("enter")
				if app.pendingWork != nil || dialog.options.DuplicateOf != "" {
					t.Fatal("blank replacement accepted")
				}
				replacement, err := board.NewTask(source.root, "feature", "replacement", "Replacement", "en", false)
				if err != nil {
					t.Fatal(err)
				}
				send(filepath.Base(replacement))
				send("enter")
			}
			send("enter")
			if app.pendingWork != nil || dialog.running {
				t.Fatal("blank reason submitted")
			}
			send("User reason")
			send("enter")
			send("enter")
			if app.pendingWork != nil || dialog.running {
				t.Fatal("blank decision submitted")
			}
			send("User decision reference")
			send("enter")
			if !dialog.running || app.pendingWork == nil {
				t.Fatalf("form did not submit: %s", dialog.form.View())
			}
			if dialog.options.Result != tc.result {
				t.Fatalf("result %s", dialog.options.Result)
			}
			runPendingWork(t, app)
			after, err := board.ReadSnapshot(source.root, source.snapshot.Entry.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			target := "archived"
			if tc.action == actionTrash {
				target = "trash"
			}
			for _, want := range []string{"LIFECYCLE_DECISION", "User reason", "User decision reference", "RESULT: " + tc.result} {
				if !strings.Contains(after.Text, want) {
					t.Fatalf("missing %s: %s; notice: %s", want, after.Text, app.CopyNotice)
				}
			}
			if after.Entry.State != target {
				t.Fatalf("state %s notice %s", after.Entry.State, app.CopyNotice)
			}
			if tc.result == "duplicate" && !strings.Contains(after.Text, "replacement-task") {
				t.Fatal("replacement lost")
			}
		})
	}
}

func TestTaskActionLifecycleRejectionAndCancel(t *testing.T) {
	for _, action := range []taskAction{actionArchive, actionTrash} {
		t.Run(string(action), func(t *testing.T) {
			source := actionTestSource(t, "backlog", true)
			result := "cancelled"
			if action == actionTrash {
				result = "trashed"
			}
			for _, options := range []board.MoveOptions{
				{Result: result, Reason: " ", Decision: "decision"},
				{Result: result, Reason: "reason", Decision: " "},
			} {
				_, err := runTaskAction(source, action, options)
				if err == nil {
					t.Fatal("board accepted missing authorization fields")
				}
				after, _ := board.ReadSnapshot(source.root, source.snapshot.Entry.TaskID)
				if after.Revision != source.snapshot.Revision || after.Entry.State != "backlog" {
					t.Fatal("failed lifecycle changed card")
				}
			}
			app := actionTestApp(t, source)
			pumpActionForm(app, chooseTaskAction(t, app, action))
			app.Update(keyMsg("esc"))
			if app.TaskActions != nil || app.pendingWork != nil {
				t.Fatal("cancel submitted form")
			}
		})
	}
}

func TestTaskActionRunningIgnoresInputAndRefresh(t *testing.T) {
	source := actionTestSource(t, "backlog", true)
	app := actionTestApp(t, source)
	chooseTaskAction(t, app, actionPick)
	calls := 0
	app.GetBoard = func() (BoardPayload, error) { calls++; return BoardPayload{}, nil }
	if app.refreshBoard() || calls != 0 {
		t.Fatal("UI refresh can block behind write transaction")
	}
	for _, key := range []string{"enter", "m", "q", "esc"} {
		app.Update(keyMsg(key))
	}
	app.Update(tea.MouseMsg{X: 10, Y: 10, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if !app.Running || !app.TaskActions.running || app.pendingWork == nil {
		t.Fatal("input discarded active move")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !app.Running {
		t.Fatal("quit discarded active write")
	}
	// A stale completion must not consume the active operation.
	dialog := app.TaskActions
	app.applyTaskActionResult(taskActionResult{id: dialog.id, sequence: dialog.sequence + 1})
	if app.TaskActions != dialog {
		t.Fatal("stale completion consumed move")
	}
	runPendingWork(t, app)
}
