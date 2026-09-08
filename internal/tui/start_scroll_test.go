package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/dualface/kander/internal/launch"
)

func TestLoadingStartWheelChangesSelectionAndDiscardsPreview(t *testing.T) {
	app := startTestApp("todo")
	app.Model.SetBoard(BoardPayload{Tasks: []Task{
		{TaskID: "start-task", State: "todo"}, {TaskID: "second-task", State: "todo"},
	}})
	app.Model.SelectTaskIndex("todo", 0)
	selected := app.Model.SelectedTask().TaskID
	app.HandleKey("s")
	old := app.takePending()
	app.HandleMouse(0, 0, mouseBtn5Pressed)
	if app.Model.SelectedTask().TaskID == selected || app.StartConfirmation != nil {
		t.Fatal("loading dialog blocked board wheel or retained stale selection")
	}
	app.HandleKey("s")
	dialog := app.StartConfirmation
	app.applyWork(old().(workMsg).payload)
	if app.StartConfirmation != dialog || dialog.phase != startLoading {
		t.Fatal("old wheel selection overwrote new preview")
	}
	finishStartPreview(app)
	if dialog.phase != startReady || dialog.TaskID != app.Model.SelectedTask().TaskID {
		t.Fatal("new selection preview lost")
	}
}

func TestStartResultViewportRetainsNarrowContent(t *testing.T) {
	t.Setenv("KANDER_LANG", "en")
	t.Setenv("KANDER_LANG_CLI", "1")
	const id = "20260908-options-workflow-flowchart-task"
	const address = "kb-board-start-task-key-12345678:@9:%9"
	const warning = "final-warning-must-remain-readable-after-notice-expires"
	for _, height := range []int{8, 10} {
		for _, failed := range []bool{false, true} {
			app := startTestApp("todo")
			app.Width, app.Height = 40, height
			prepare := app.PrepareStart
			app.PrepareStart = func(id string) (startRequest, error) { r, e := prepare(id); r.Launcher = "tmux-session"; return r, e }
			app.Model.SetBoard(BoardPayload{Tasks: []Task{{TaskID: id, State: "todo"}}})
			app.StartTask = func(r startRequest) (launch.StartResult, error) {
				result := launch.StartResult{TaskID: r.TaskID, Agent: r.Agent,
					Plan:    launch.LaunchPlan{Launcher: "tmux-session", Session: "kb-board-start-task-key-12345678"},
					Outcome: launch.LaunchOutcome{Window: "@9", Pane: "%9"}, Warnings: []string{strings.Repeat("warning-prefix-", 4) + warning}}
				if failed {
					return result, errors.New("launch-rolled-back-for-" + address)
				}
				return result, nil
			}
			app.HandleKey("s")
			finishStartPreview(app)
			app.HandleKey("y")
			app.applyWork(app.takePending()().(workMsg).payload)
			app.CopyNoticeUntil = app.Now()
			dialog := app.StartConfirmation
			rows := map[int]string{}
			for i := 0; i < 100; i++ {
				app.View()
				for row, text := range strings.Split(ansi.Strip(dialog.bodyView.View()), "\n") {
					rows[dialog.bodyView.YOffset+row] = strings.TrimSpace(text)
				}
				before := dialog.bodyView.YOffset
				app.HandleMouse(0, 0, mouseBtn5Pressed)
				if dialog.bodyView.YOffset == before {
					break
				}
			}
			var all strings.Builder
			for i := 0; i < len(rows); i++ {
				all.WriteString(rows[i])
			}
			for _, want := range []string{address, warning} {
				if !strings.Contains(all.String(), want) {
					t.Fatalf("height=%d failed=%v missing %q: %s", height, failed, want, all.String())
				}
			}
			if app.StartConfirmation != dialog || dialog.bodyView.YOffset == 0 {
				t.Fatalf("result did not persist and scroll: height=%d failed=%v phase=%v offset=%d viewHeight=%d lines=%d", height, failed, dialog.phase, dialog.bodyView.YOffset, dialog.bodyView.Height, len(rows))
			}
			app.Width, app.Height = 100, 30
			app.View()
			if dialog.bodyView.YOffset != 0 {
				t.Fatal("resize did not clamp result scroll")
			}
			app.HandleKey("y")
			if app.StartConfirmation != nil || app.pendingWork != nil {
				t.Fatal("result key did not only close dialog")
			}
		}
	}
}
