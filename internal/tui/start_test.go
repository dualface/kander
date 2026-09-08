package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/launch"
)

func startTestApp(state string) *App {
	app := focusTestApp()
	task := Task{TaskID: "start-task", Title: "Start", State: state, Kind: "small"}
	app.GetTask = func(string) (Task, error) { return task, nil }
	app.GetBoard = func() (BoardPayload, error) { return BoardPayload{Tasks: []Task{task}}, nil }
	app.refreshBoard()
	app.Model.ShowArchived = true
	app.Model.ColumnIndex = indexOf(app.Model.States(), state)
	app.PrepareStart = func(id string) (startRequest, error) {
		return startRequest{StartPreview: launch.StartPreview{TaskID: id, State: state, Size: "small", Agent: "claude", Launcher: "herdr"}}, nil
	}
	app.StartTask = func(startRequest) (launch.StartResult, error) { panic("unexpected launch") }
	return app
}

func TestStartKeyStatesAndCancellation(t *testing.T) {
	for _, state := range []string{"backlog", "todo", "working", "review", "done", "archived", "trash"} {
		t.Run(state, func(t *testing.T) {
			app := startTestApp(state)
			app.HandleKey("s")
			if state != "backlog" && state != "todo" {
				if app.StartConfirmation != nil || app.pendingWork != nil || app.CopyNotice == "" {
					t.Fatal("invalid state must only show notice")
				}
				return
			}
			if app.StartConfirmation == nil {
				t.Fatal("missing confirmation")
			}
			text := ansi.Strip(app.View())
			for _, want := range []string{"start-task", "claude", "herdr"} {
				if !strings.Contains(text, want) {
					t.Fatalf("missing %s: %s", want, text)
				}
			}
			if state == "backlog" && !strings.Contains(text, "todo") {
				t.Fatal("missing backlog move warning")
			}
			for _, key := range []string{"esc", "n", "q", "s", "enter", "Y", "ctrl-c"} {
				app.HandleKey(key)
				if app.StartConfirmation != nil || app.pendingWork != nil || !app.Running {
					t.Fatalf("cancel %q produced side effect", key)
				}
				app.HandleKey("s")
			}
		})
	}
	app := startTestApp("todo")
	app.Model.SetBoard(BoardPayload{})
	app.HandleKey("s")
	if app.StartConfirmation != nil || app.CopyNotice == "" {
		t.Fatal("empty selection must show notice")
	}
}

func TestStartKeyContextsAndHelp(t *testing.T) {
	app := startTestApp("todo")
	app.HandleKey("/")
	app.HandleKey("s")
	if app.Model.Query != "s" || app.StartConfirmation != nil {
		t.Fatal("s must remain search input")
	}
	app.HandleKey("esc")
	app.HandleKey("enter")
	app.HandleKey("s")
	if app.Detail == nil || app.StartConfirmation != nil {
		t.Fatal("detail s must not start")
	}
	found := false
	for _, entry := range boardHelpGroups()[0].Entries {
		if entry.Keys == "s" {
			found = entry.Desc != ""
		}
	}
	if !found {
		t.Fatal("missing board help")
	}
}

func TestStartConfirmationErrors(t *testing.T) {
	for _, launcher := range []string{"foreground", "console"} {
		app := startTestApp("backlog")
		prepare := app.PrepareStart
		app.PrepareStart = func(id string) (startRequest, error) { r, e := prepare(id); r.Launcher = launcher; return r, e }
		app.HandleKey("s")
		if app.StartConfirmation != nil || app.pendingWork != nil || !strings.Contains(app.CopyNotice, "kander start") {
			t.Fatal("unsupported launcher must require CLI")
		}
		_, err := runTaskStart(startRequest{StartPreview: launch.StartPreview{State: "backlog", Launcher: launcher}, root: "missing"})
		if err == nil || !strings.Contains(err.Error(), "kander start") {
			t.Fatalf("launcher must reject before board access: %v", err)
		}
	}
	app := startTestApp("todo")
	app.PrepareStart = func(string) (startRequest, error) { return startRequest{}, errors.New("config unreadable") }
	app.HandleKey("s")
	if app.StartConfirmation != nil || !strings.Contains(app.CopyNotice, "config unreadable") {
		t.Fatal("missing preparation error")
	}
}

func TestStartBackgroundCompletion(t *testing.T) {
	for _, launcher := range []string{"herdr", "tmux", "tmux-session"} {
		for _, failed := range []bool{false, true} {
			app := startTestApp("todo")
			prepare := app.PrepareStart
			app.PrepareStart = func(id string) (startRequest, error) { r, e := prepare(id); r.Launcher = launcher; return r, e }
			calls, refreshes := 0, 0
			app.GetBoard = func() (BoardPayload, error) {
				refreshes++
				return BoardPayload{Tasks: []Task{{TaskID: "start-task", State: "working"}}}, nil
			}
			app.StartTask = func(r startRequest) (launch.StartResult, error) {
				calls++
				if r.TaskID != "start-task" || r.Agent != "claude" || r.Launcher != launcher {
					t.Fatalf("wrong confirmed request: %+v", r)
				}
				if failed {
					return launch.StartResult{}, errors.New("launch broke")
				}
				return launch.StartResult{TaskID: r.TaskID, Agent: r.Agent, Plan: launch.LaunchPlan{Launcher: launcher, Session: "$4"}, Outcome: launch.LaunchOutcome{Tab: "w1:t9", Window: "@9", Pane: "%9"}, Warnings: []string{"identity warning"}}, nil
			}
			p := program{app: app}
			app.HandleKey("s")
			_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
			if cmd == nil || calls != 0 || refreshes != 0 || app.StartConfirmation != nil {
				t.Fatal("start must be asynchronous")
			}
			app.HandleKey("/")
			if !app.Searching {
				t.Fatal("background start blocked input")
			}
			app.Options = &optionsPanel{app: app}
			p.Update(cmd())
			if calls != 1 || refreshes != 1 {
				t.Fatalf("calls=%d refreshes=%d", calls, refreshes)
			}
			if failed {
				if !strings.Contains(app.CopyNotice, "launch broke") {
					t.Fatal(app.CopyNotice)
				}
			} else {
				for _, want := range []string{"start-task", "claude", launcher, "%9", "identity warning"} {
					if !strings.Contains(app.CopyNotice, want) {
						t.Fatal(app.CopyNotice)
					}
				}
				if app.Model.Tasks[0].State != "working" {
					t.Fatal("board not refreshed")
				}
			}
		}
	}
}

func TestBacklogStartUsesControlledGate(t *testing.T) {
	root := t.TempDir()
	for _, state := range board.States {
		if err := os.Mkdir(filepath.Join(root, state), 0700); err != nil {
			t.Fatal(err)
		}
	}
	path, err := board.NewTask(root, "feature", "start-gate", "Start gate", "en", false)
	if err != nil {
		t.Fatal(err)
	}
	id := filepath.Base(path)
	before, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	request := startRequest{root: root, StartPreview: launch.StartPreview{TaskID: id, State: "backlog", Agent: "claude", Launcher: "herdr"}}
	_, err = runTaskStart(request)
	if err == nil {
		t.Fatal("incomplete backlog contract accepted")
	}
	after, e := board.ReadSnapshot(root, id)
	if e != nil {
		t.Fatal(e)
	}
	if after.Entry.State != "backlog" || after.Revision != before.Revision || after.Text != before.Text {
		t.Fatal("failed gate changed card")
	}
	app := startTestApp("backlog")
	app.applyStartResult(startResult{err: err})
	if !strings.Contains(app.CopyNotice, strings.ReplaceAll(err.Error(), "\n", " ")) {
		t.Fatal("gate error not visible")
	}
}

func TestBacklogStartMovesToTodoBeforeLaunchPreflight(t *testing.T) {
	root := t.TempDir()
	for _, state := range board.States {
		if err := os.Mkdir(filepath.Join(root, state), 0700); err != nil {
			t.Fatal(err)
		}
	}
	path, err := board.NewTask(root, "feature", "start-ready", "Ready", "en", false)
	if err != nil {
		t.Fatal(err)
	}
	id := filepath.Base(path)
	before, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(before.Text, "<FILL_IN>", "Ready")
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: Ready\n", 1)
	if err := os.WriteFile(filepath.Join(path, "spec.md"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANDER_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("HERDR_ENV", "")
	t.Setenv("HERDR_WORKSPACE_ID", "")
	_, err = runTaskStart(startRequest{root: root, StartPreview: launch.StartPreview{TaskID: id, State: "backlog", Agent: "claude", Launcher: "herdr"}})
	if err == nil {
		t.Fatal("missing launcher preflight failure")
	}
	after, e := board.ReadSnapshot(root, id)
	if e != nil {
		t.Fatal(e)
	}
	if after.Entry.State != "todo" || after.Text != text {
		t.Fatalf("failed start must retain migrated todo: %s", after.Entry.State)
	}
}

func TestStartConfirmationNarrowTerminalAndMouse(t *testing.T) {
	app := startTestApp("todo")
	app.HandleKey("s")
	for _, width := range []int{12, 40, 80} {
		app.Width, app.Height = width, 24
		view := app.View()
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("overflow at width %d", width)
			}
		}
	}
	selected := app.Model.CurrentState()
	app.HandleMouse(1, 1, mouseBtn1Double)
	if app.StartConfirmation == nil || app.Detail != nil || app.Model.CurrentState() != selected {
		t.Fatal("mouse escaped confirmation")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if app.StartConfirmation != nil || !app.Running {
		t.Fatal("Ctrl+C must cancel confirmation")
	}
}

func TestStartQuitWaitsForBackgroundCompletion(t *testing.T) {
	for _, key := range []string{"q", "ctrl-c"} {
		for _, failed := range []bool{false, true} {
			app := startTestApp("todo")
			entered, release := make(chan struct{}), make(chan struct{})
			t.Cleanup(func() {
				select {
				case <-release:
				default:
					close(release)
				}
			})
			app.StartTask = func(r startRequest) (launch.StartResult, error) {
				close(entered)
				<-release
				if failed {
					return launch.StartResult{}, errors.New("rolled back")
				}
				return launch.StartResult{TaskID: r.TaskID, Agent: r.Agent, Plan: launch.LaunchPlan{Launcher: r.Launcher}}, nil
			}
			p := program{app: app}
			app.HandleKey("s")
			_, start := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
			completed := make(chan tea.Msg, 1)
			go func() { completed <- start() }()
			<-entered
			event := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
			if key == "ctrl-c" {
				event = tea.KeyMsg{Type: tea.KeyCtrlC}
				app.Options = &optionsPanel{app: app}
			}
			_, quit := p.Update(event)
			if quit != nil || !app.Running || !app.quitAfterStarts || app.startsRunning != 1 {
				t.Fatal("quit interrupted active start")
			}
			app.Options = nil
			p.Update(tea.WindowSizeMsg{Width: 85, Height: 25})
			if app.Width != 85 {
				t.Fatal("event loop stopped while draining start")
			}
			close(release)
			_, quit = p.Update(<-completed)
			if app.Running || app.startsRunning != 0 || quit == nil {
				t.Fatal("quit was not completed after start settled")
			}
			if _, ok := quit().(tea.QuitMsg); !ok {
				t.Fatal("missing terminal quit command")
			}
		}
	}
}

func TestStartQuitWaitsForAllQueuedStarts(t *testing.T) {
	app := startTestApp("todo")
	for i := 0; i < 2; i++ {
		app.HandleKey("s")
		app.HandleKey("y")
		app.takePending()
	}
	app.HandleKey("q")
	app.applyStartResult(startResult{err: errors.New("first failed")})
	if !app.Running || app.startsRunning != 1 {
		t.Fatal("quit before every start settled")
	}
	app.applyStartResult(startResult{err: errors.New("second failed")})
	if app.Running || app.startsRunning != 0 {
		t.Fatal("pending quit lost")
	}
}

func TestStartResultRendersCompleteContainerAddress(t *testing.T) {
	app := startTestApp("todo")
	app.Width = 80
	const address = "kb-board-start-task-key-12345678:@9:%9"
	app.applyStartResult(startResult{result: launch.StartResult{
		TaskID: "20260908-options-workflow-flowchart-task", Agent: "claude",
		Plan:    launch.LaunchPlan{Launcher: "tmux-session", Session: "kb-board-start-task-key-12345678"},
		Outcome: launch.LaunchOutcome{Window: "@9", Pane: "%9"},
	}})
	lines := strings.Split(ansi.Strip(app.View()), "\n")
	footer := lines[len(lines)-1]
	for _, want := range []string{"claude", "tmux-session", address} {
		if !strings.Contains(footer, want) {
			t.Fatalf("missing %q in footer %q", want, footer)
		}
	}
	app.Width = 40
	lines = strings.Split(ansi.Strip(app.View()), "\n")
	for i, line := range lines {
		lines[i] = strings.Trim(line, " │")
	}
	if !strings.Contains(strings.Join(lines, ""), address) {
		t.Fatal("narrow result must show complete wrapped address")
	}
	app.HandleKey("/")
	if !app.Searching || app.StartConfirmation != nil {
		t.Fatal("result overlay must not capture input")
	}
	app.CopyNoticeUntil = app.Now()
	if app.startNoticeOverflows(app.Width) {
		t.Fatal("expired result overlay remained")
	}
}
