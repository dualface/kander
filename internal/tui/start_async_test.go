package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/dualface/kander/internal/launch"
	"github.com/muesli/termenv"
)

func TestStartPreviewUpdateDoesNoIOAndRemainsResponsive(test *testing.T) {
	app := startTestApp("todo")
	prepare := app.PrepareStart
	entered, release := make(chan struct{}), make(chan struct{})

	app.PrepareStart = func(id string) (startRequest, error) {
		close(entered)
		<-release
		return prepare(id)
	}
	app.GetTask = func(string) (Task, error) { test.Fatal("key path read card"); return Task{}, nil }
	refreshes := 0
	app.GetBoard = func() (BoardPayload, error) { refreshes++; return BoardPayload{Tasks: app.Model.Tasks}, nil }
	p := program{app: app}
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd == nil || refreshes != 0 || app.StartConfirmation == nil || app.StartConfirmation.phase != startLoading {
		test.Fatal("same Update must open loading dialog without reading")
	}
	select {
	case <-entered:
		test.Fatal("Update called preview synchronously")
	default:
	}
	view := ansi.Strip(app.View())
	for _, want := range []string{"start-task", "todo", t("tui.start_loading")} {
		if !strings.Contains(view, want) {
			test.Fatalf("missing %q: %s", want, view)
		}
	}
	done := make(chan tea.Msg, 1)
	defer func() { close(release); <-done }()
	go func() { done <- cmd() }()
	<-entered
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if app.StartConfirmation.phase != startLoading || !strings.Contains(app.CopyNotice, t("tui.start_loading_keys")) {
		test.Fatal("loading confirmation must wait")
	}
	app.LastRefresh = app.Now().Add(-time.Hour)
	p.Update(tickMsg(app.Now()))
	if refreshes != 1 {
		test.Fatal("preview blocked refresh")
	}
	p.Update(tea.WindowSizeMsg{Width: 90, Height: 26})
	p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if app.StartConfirmation != nil || !app.Searching || app.Width != 90 {
		test.Fatal("background read blocked cancellation or input")
	}
}

func TestStartPreviewDiscardsLateResults(test *testing.T) {
	for _, mode := range []string{"closed", "same-card-reopened", "other-card", "selection-changed"} {
		test.Run(mode, func(test *testing.T) {
			app := startTestApp("todo")
			app.HandleKey("s")
			old := app.takePending()
			if mode != "selection-changed" {
				app.HandleKey("esc")
			}
			if mode == "other-card" || mode == "selection-changed" {
				app.Model.SetBoard(BoardPayload{Tasks: []Task{{TaskID: "other-task", State: "todo"}}})
			}
			if mode == "same-card-reopened" || mode == "other-card" {
				app.HandleKey("s")
			}
			dialog, notice := app.StartConfirmation, app.CopyNotice
			app.applyWork(old().(workMsg).payload)
			if mode == "selection-changed" {
				if app.StartConfirmation != nil {
					test.Fatal("changed selection retained old dialog")
				}
			} else if app.StartConfirmation != dialog || (dialog != nil && dialog.phase != startLoading) {
				test.Fatal("stale preview replaced current dialog")
			}
			if app.CopyNotice != notice {
				test.Fatal("stale preview overwrote notice")
			}
			if dialog != nil && mode != "selection-changed" {
				finishStartPreview(app)
				if dialog.phase != startReady {
					test.Fatal("current preview discarded")
				}
			}
		})
	}
}

func TestStartPreviewChangedStateClosesWithReason(test *testing.T) {
	app := startTestApp("todo")
	prepare := app.PrepareStart
	app.PrepareStart = func(id string) (startRequest, error) { r, e := prepare(id); r.State = "working"; return r, e }
	app.HandleKey("s")
	finishStartPreview(app)
	if app.StartConfirmation != nil || app.CopyNotice != t("tui.start_invalid_state", "working") {
		test.Fatal("changed state must reject before launch")
	}
}

func TestStartDialogRetainsAsyncProgressAndResultUntilKey(test *testing.T) {
	for _, failed := range []bool{false, true} {
		app := startTestApp("todo")
		app.HandleKey("s")
		finishStartPreview(app)
		calls := 0
		entered, release := make(chan struct{}), make(chan struct{})
		app.StartTask = func(r startRequest) (launch.StartResult, error) {
			calls++
			close(entered)
			<-release
			result := launch.StartResult{TaskID: r.TaskID, Agent: r.Agent, Plan: launch.LaunchPlan{Launcher: r.Launcher}, Warnings: []string{"session warning"}}
			if failed {
				return result, errors.New("launch failed and rolled back")
			}
			return result, nil
		}
		p := program{app: app}
		_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
		if calls != 0 || app.StartConfirmation.phase != startRunning || !strings.Contains(ansi.Strip(app.View()), t("tui.start_starting", "start-task")) {
			test.Fatal("confirmation must retain asynchronous progress dialog")
		}
		done := make(chan tea.Msg, 1)
		joined := false
		defer func() {
			if !joined {
				close(release)
				<-done
			}
		}()
		go func() { done <- cmd() }()
		<-entered
		for _, key := range []string{"y", "q", "esc", "ctrl-c", "s"} {
			app.HandleKey(key)
		}
		if app.StartConfirmation.phase != startRunning || !app.Running || app.pendingWork != nil {
			test.Fatal("key closed progress dialog or queued another launch")
		}
		p.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		close(release)
		p.Update(<-done)
		joined = true
		if calls != 1 || app.StartConfirmation.phase != startFinished {
			test.Fatal("completion lost")
		}
		want := t("tui.start_success", "start-task", "claude", "herdr", ":")
		if failed {
			want = "launch failed and rolled back"
		}
		view := ansi.Strip(app.View())
		for _, text := range []string{want, "session warning", t("tui.start_result_keys")} {
			if !strings.Contains(view, text) {
				test.Fatalf("missing result %q: %s", text, view)
			}
		}
		app.CopyNoticeUntil = app.Now()
		p.Update(tickMsg(app.Now()))
		if app.StartConfirmation == nil {
			test.Fatal("result expired without key")
		}
		p.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		if app.StartConfirmation != nil || !app.Running {
			test.Fatal("result key must only close dialog")
		}
	}
}

func TestStartDialogParagraphAndFooterLayout(test *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(profile)
	p := themePalette("dark")
	for _, tc := range []struct {
		height int
		want   string
	}{
		{6, "task\n\nsettings\n\nbacklog\nkeys"},
		{7, "task\n\nsettings\n\nbacklog\n\nkeys"},
		{4, "task\nsettings\nbacklog\nkeys"},
		{3, "task\nsettings\nkeys"},
	} {
		view := viewport.New(40, tc.height)
		got := fitStartDialog([]string{"task", "settings", "backlog"}, "keys", 40, tc.height, p, &view)
		lines := strings.Split(ansi.Strip(got), "\n")
		for i := range lines {
			lines[i] = strings.TrimRight(lines[i], " ")
		}
		if strings.Join(lines, "\n") != tc.want {
			test.Fatalf("height %d: %q", tc.height, got)
		}
		if !strings.HasSuffix(got, styleFor("popup-dim", p).Render("keys")) {
			test.Fatal("hint lost dim style")
		}
	}
	app := startTestApp("backlog")
	app.HandleKey("s")
	finishStartPreview(app)
	_, popup := app.renderStartConfirmation()
	lines := strings.Split(ansi.Strip(popup), "\n")
	if !strings.Contains(lines[1], t("tui.start_confirm")) || !strings.Contains(lines[2], "─") {
		test.Fatal("missing options-style title and separator")
	}
	app.Width, app.Height = 22, 60
	_, popup = app.renderStartConfirmation()
	var content strings.Builder
	for _, line := range strings.Split(ansi.Strip(popup), "\n") {
		if ansi.StringWidth(line) > app.Width {
			test.Fatal("horizontal overflow")
		}
		content.WriteString(strings.Trim(line, " │"))
	}
	if !strings.Contains(content.String(), "start-task") || !strings.Contains(content.String(), "claude") {
		test.Fatal("horizontal content truncated")
	}
}

func TestStartDialogTitleFollowsPhase(test *testing.T) {
	app := startTestApp("todo")
	app.HandleKey("s")
	if title := startDialogTitleText(app); !strings.Contains(title, t("tui.start_loading")) || strings.Contains(title, t("tui.start_confirm")) {
		test.Fatalf("loading title: %q", title)
	}
	finishStartPreview(app)
	if title := startDialogTitleText(app); !strings.Contains(title, t("tui.start_confirm")) {
		test.Fatalf("ready title: %q", title)
	}
	app.HandleKey("y")
	if title := startDialogTitleText(app); !strings.Contains(title, t("tui.start_title_starting")) || strings.Contains(title, "start-task") {
		test.Fatalf("running title: %q", title)
	}
	app.applyStartResult(startResult{
		sequence: app.StartConfirmation.sequence,
		result:   launch.StartResult{TaskID: "start-task", Agent: "claude", Plan: launch.LaunchPlan{Launcher: "herdr"}},
	})
	if title := startDialogTitleText(app); !strings.Contains(title, t("tui.start_title_started")) || strings.Contains(title, t("tui.start_confirm")) {
		test.Fatalf("success title: %q", title)
	}

	app = startTestApp("todo")
	app.HandleKey("s")
	finishStartPreview(app)
	app.HandleKey("y")
	app.applyStartResult(startResult{sequence: app.StartConfirmation.sequence, err: errors.New("boom")})
	if title := startDialogTitleText(app); !strings.Contains(title, t("tui.start_title_failed")) || strings.Contains(title, t("tui.start_confirm")) {
		test.Fatalf("failure title: %q", title)
	}
	app.StartConfirmation.message = t("tui.start_success", "start-task", "claude", "herdr", ":")
	if title := startDialogTitleText(app); !strings.Contains(title, t("tui.start_title_failed")) || strings.Contains(title, t("tui.start_title_started")) {
		test.Fatalf("failed flag ignored: %q", title)
	}
	app.StartConfirmation.failed = false
	app.StartConfirmation.message = t("tui.start_failed", "boom")
	if title := startDialogTitleText(app); !strings.Contains(title, t("tui.start_title_started")) || strings.Contains(title, t("tui.start_title_failed")) {
		test.Fatalf("success flag ignored: %q", title)
	}

	app.Width, app.Height = 12, 24
	for _, phase := range []startPhase{startLoading, startReady, startRunning, startFinished} {
		app.StartConfirmation.phase = phase
		_, popup := app.renderStartConfirmation()
		for _, line := range strings.Split(ansi.Strip(popup), "\n") {
			if ansi.StringWidth(line) > app.Width {
				test.Fatalf("overflow phase %d: %q", phase, line)
			}
		}
	}
}

func startDialogTitleText(app *App) string {
	_, popup := app.renderStartConfirmation()
	lines := strings.Split(ansi.Strip(popup), "\n")
	if len(lines) < 2 {
		return ""
	}
	return strings.TrimSpace(strings.Trim(lines[1], " │"))
}
