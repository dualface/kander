package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/launch"
)

type takeoverCall struct {
	repository issue.Repository
	number     int
	options    issue.TriageOptions
}

// takeoverApp builds an overlay app with the takeover bindings of the TUI: the
// preview comes from the injected launch layer and the start path is recorded.
func takeoverApp(t *testing.T, fake *fakeIssues, tasks []Task, index issue.Index, runner func(context.Context, issue.Repository, int, issue.TriageOptions) (issue.TriageOutcome, error)) (*App, *[]takeoverCall) {
	t.Helper()
	app := newApp(true, 30, tuiPageContext(), func() (BoardPayload, error) {
		return BoardPayload{Tasks: tasks}, nil
	}, func(string) (Task, error) { return Task{}, errors.New("no detail") }, "dark", 1, nil, nil)
	app.Width, app.Height = 120, 30
	app.IssueProvider = func() issue.IssueProvider { return fake }
	app.ImportIndex = func() (issue.Index, error) { return index, nil }
	app.PrepareTriage = func() (launch.TriagePreview, error) {
		return launch.TriagePreview{Agent: "claude", Launcher: "tmux"}, nil
	}
	calls := &[]takeoverCall{}
	app.TriageIssue = func(ctx context.Context, repository issue.Repository, number int, options issue.TriageOptions) (issue.TriageOutcome, error) {
		*calls = append(*calls, takeoverCall{repository: repository, number: number, options: options})
		return runner(ctx, repository, number, options)
	}
	app.refreshBoard()
	return app, calls
}

func takeoverListApp(t *testing.T, runner func(context.Context, issue.Repository, int, issue.TriageOptions) (issue.TriageOutcome, error)) (*App, *fakeIssues, *[]takeoverCall) {
	t.Helper()
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	app, calls := takeoverApp(t, fake, nil, nil, runner)
	app.HandleKey("g")
	runPendingWork(t, app)
	return app, fake, calls
}

func TestIssuesTakeoverUnboundStartsThroughTheSharedPath(t *testing.T) {
	app, fake, calls := takeoverListApp(t, func(context.Context, issue.Repository, int, issue.TriageOptions) (issue.TriageOutcome, error) {
		return issue.TriageOutcome{Agent: "claude", Launcher: "tmux", Address: "session:win:pane"}, nil
	})

	app.HandleKey("s")
	dialog := app.Takeover
	if dialog == nil || dialog.phase != takeoverLoading {
		t.Fatalf("dialog=%+v", dialog)
	}
	if dialog.number != 42 || dialog.cardID != "" {
		t.Fatalf("dialog=%+v", dialog)
	}
	if !strings.Contains(ansi.Strip(app.View()), config.Text("tui.issues_takeover_loading")) {
		t.Fatalf("loading dialog missing\n%s", ansi.Strip(app.View()))
	}
	if len(*calls) != 0 {
		t.Fatal("the session must not start before the confirmation")
	}

	runPendingWork(t, app)
	if dialog.phase != takeoverReady || dialog.agent != "claude" || dialog.launcher != "tmux" {
		t.Fatalf("dialog=%+v", dialog)
	}
	view := ansi.Strip(app.View())
	for _, want := range []string{
		config.Text("tui.issues_takeover_title", "42"),
		"dualface/kander#42",
		config.Text("tui.start_settings", "claude", "tmux"),
		config.Text("tui.issues_takeover_keys"),
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("dialog missing %q:\n%s", want, view)
		}
	}

	app.HandleKey("y")
	if dialog.phase != takeoverRunning {
		t.Fatalf("dialog=%+v", dialog)
	}
	runPendingWork(t, app)
	if dialog.phase != takeoverFinished || dialog.failed {
		t.Fatalf("dialog=%+v", dialog)
	}
	want := config.Text("tui.issues_takeover_started", "claude", "tmux", "session:win:pane")
	if dialog.message != want {
		t.Fatalf("message=%q want=%q", dialog.message, want)
	}
	if !strings.Contains(ansi.Strip(app.View()), config.Text("tui.start_title_started")) {
		t.Fatalf("result dialog missing:\n%s", ansi.Strip(app.View()))
	}
	if len(*calls) != 1 {
		t.Fatalf("calls=%+v", *calls)
	}
	call := (*calls)[0]
	if call.number != 42 || call.options.CardID != "" || call.repository.Name != fake.repository.Name {
		t.Fatalf("call=%+v", call)
	}
	app.HandleKey("x")
	if app.Takeover != nil || app.Issues == nil {
		t.Fatalf("any key must close the result and keep the overlay: %+v", app.Takeover)
	}
}

func TestIssuesTakeoverCancelKeepsTheBoard(t *testing.T) {
	app, _, calls := takeoverListApp(t, func(context.Context, issue.Repository, int, issue.TriageOptions) (issue.TriageOutcome, error) {
		return issue.TriageOutcome{}, nil
	})
	app.HandleKey("s")
	runPendingWork(t, app)
	app.HandleKey("n")
	if app.Takeover != nil || app.Issues == nil {
		t.Fatalf("cancel must only close the dialog: %+v", app.Takeover)
	}
	if len(*calls) != 0 {
		t.Fatalf("cancel started a session: %+v", *calls)
	}
}

func TestIssuesTakeoverBoundBacklogOffersJumpAndContract(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	task := Task{TaskID: "task-1", Title: "Task", State: "backlog", Document: "- WINDOW: herdr:w1:t2:w1:p3\n"}
	index := importTestIndex(fake.repository, 42, "task-1",
		time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 10, 1, 0, 0, 0, time.UTC))
	app, calls := takeoverApp(t, fake, []Task{task}, index, func(context.Context, issue.Repository, int, issue.TriageOptions) (issue.TriageOutcome, error) {
		return issue.TriageOutcome{Agent: "claude", Launcher: "tmux", Address: "session:win:pane"}, nil
	})
	app.HandleKey("g")
	runPendingWork(t, app)

	app.HandleKey("s")
	dialog := app.Takeover
	if dialog == nil || dialog.cardID != "task-1" {
		t.Fatalf("dialog=%+v", dialog)
	}
	runPendingWork(t, app)
	view := ansi.Strip(app.View())
	for _, want := range []string{
		config.Text("tui.issues_contract_title", "42", "task-1"),
		config.Text("tui.issues_contract_jump"),
		config.Text("tui.issues_contract_start"),
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("dialog missing %q:\n%s", want, view)
		}
	}

	// y/Enter is the default exit of a bound card: jump to it.
	app.HandleKey("y")
	if app.Takeover != nil || app.Issues != nil {
		t.Fatalf("jump must close the dialog and the overlay: %+v", app.Takeover)
	}
	if len(*calls) != 0 {
		t.Fatalf("jump started a session: %+v", *calls)
	}
	selected := app.Model.SelectedTask()
	if selected == nil || selected.TaskID != "task-1" {
		t.Fatalf("board selection=%+v", selected)
	}
	if !strings.Contains(app.CopyNotice, config.Text("tui.issues_bound_state", "task-1", config.Text("tui.backlog"))) {
		t.Fatalf("notice=%q", app.CopyNotice)
	}

	// s starts the contract session for that card.
	app.HandleKey("g")
	runPendingWork(t, app)
	app.HandleKey("s")
	runPendingWork(t, app)
	app.HandleKey("s")
	result := app.Takeover
	if result == nil || result.phase != takeoverRunning {
		t.Fatalf("dialog=%+v", result)
	}
	runPendingWork(t, app)
	if len(*calls) != 1 || (*calls)[0].options.CardID != "task-1" {
		t.Fatalf("calls=%+v", *calls)
	}
	if result.phase != takeoverFinished || result.failed {
		t.Fatalf("dialog=%+v", result)
	}
}

func TestIssuesTakeoverBoundOtherStateJumpsDirectly(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	task := Task{TaskID: "task-1", Title: "Task", State: "working", Document: "- WINDOW: herdr:w1:t2:w1:p3\n"}
	key, err := fake.repository.IssueSourceKey(42)
	if err != nil {
		t.Fatal(err)
	}
	index := issue.Index{key: issue.LocalCard{
		TaskID: "task-1", State: "working", Path: "/board/working/task-1",
		IssueUpdatedAt: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC),
		FetchedAt:      time.Date(2026, 9, 10, 1, 0, 0, 0, time.UTC),
	}}
	app, calls := takeoverApp(t, fake, []Task{task}, index, func(context.Context, issue.Repository, int, issue.TriageOptions) (issue.TriageOutcome, error) {
		return issue.TriageOutcome{}, nil
	})
	app.HandleKey("g")
	runPendingWork(t, app)
	app.HandleKey("s")
	if app.Takeover != nil {
		t.Fatalf("a non-backlog card must not open the dialog: %+v", app.Takeover)
	}
	if app.Issues != nil {
		t.Fatal("the overlay should close on the jump")
	}
	if !strings.Contains(app.CopyNotice, config.Text("tui.issues_bound_state", "task-1", config.Text("tui.working"))) {
		t.Fatalf("notice=%q", app.CopyNotice)
	}
	if len(*calls) != 0 {
		t.Fatalf("calls=%+v", *calls)
	}
}

func TestIssuesTakeoverRefusesForegroundLaunchers(t *testing.T) {
	app, _, calls := takeoverListApp(t, func(context.Context, issue.Repository, int, issue.TriageOptions) (issue.TriageOutcome, error) {
		return issue.TriageOutcome{}, nil
	})
	app.PrepareTriage = func() (launch.TriagePreview, error) {
		return launch.TriagePreview{Agent: "claude", Launcher: "foreground"}, nil
	}
	app.HandleKey("s")
	runPendingWork(t, app)
	if app.Takeover != nil {
		t.Fatalf("foreground must not open a startable dialog: %+v", app.Takeover)
	}
	if app.Issues == nil || app.Issues.notice != config.Text("tui.start_use_cli", "foreground") {
		t.Fatalf("notice=%q", app.Issues.notice)
	}
	if len(*calls) != 0 {
		t.Fatalf("calls=%+v", *calls)
	}
}

func TestIssuesTakeoverReportsPreviewAndStartFailures(t *testing.T) {
	app, _, _ := takeoverListApp(t, func(context.Context, issue.Repository, int, issue.TriageOptions) (issue.TriageOutcome, error) {
		return issue.TriageOutcome{}, errors.New("launch boom")
	})
	app.PrepareTriage = func() (launch.TriagePreview, error) {
		return launch.TriagePreview{}, errors.New("no config")
	}
	app.HandleKey("s")
	runPendingWork(t, app)
	if app.Takeover != nil || app.Issues.notice != config.Text("tui.issues_takeover_preview_failed", "no config") {
		t.Fatalf("takeover=%+v notice=%q", app.Takeover, app.Issues.notice)
	}

	app.PrepareTriage = func() (launch.TriagePreview, error) {
		return launch.TriagePreview{Agent: "claude", Launcher: "tmux"}, nil
	}
	app.HandleKey("s")
	runPendingWork(t, app)
	app.HandleKey("y")
	runPendingWork(t, app)
	dialog := app.Takeover
	if dialog == nil || dialog.phase != takeoverFinished || !dialog.failed {
		t.Fatalf("dialog=%+v", dialog)
	}
	if want := config.Text("tui.issues_takeover_failed", "launch boom"); dialog.message != want {
		t.Fatalf("message=%q want=%q", dialog.message, want)
	}
}

func TestIssuesTakeoverWithoutRunnerExplainsItself(t *testing.T) {
	app, _, _ := takeoverListApp(t, func(context.Context, issue.Repository, int, issue.TriageOptions) (issue.TriageOutcome, error) {
		return issue.TriageOutcome{}, nil
	})
	app.TriageIssue = nil
	app.HandleKey("s")
	runPendingWork(t, app)
	app.HandleKey("y")
	dialog := app.Takeover
	if dialog == nil || dialog.phase != takeoverFinished || dialog.message != config.Text("tui.issues_takeover_unavailable") {
		t.Fatalf("dialog=%+v", dialog)
	}
}

func TestIssuesTakeoverDropsStaleResults(t *testing.T) {
	app, _, _ := takeoverListApp(t, func(context.Context, issue.Repository, int, issue.TriageOptions) (issue.TriageOutcome, error) {
		return issue.TriageOutcome{Agent: "claude", Launcher: "tmux", Address: "a:b:c"}, nil
	})
	app.HandleKey("s")
	stalePreview := app.Takeover.sequence
	app.Takeover = nil
	app.HandleKey("s")
	dialog := app.Takeover
	if dialog.sequence == stalePreview {
		t.Fatal("the second dialog reused the sequence")
	}
	app.applyTakeoverPreview(takeoverPreviewResult{sequence: stalePreview, preview: launch.TriagePreview{Agent: "old", Launcher: "old"}})
	if dialog.agent != "" || dialog.launcher != "" {
		t.Fatalf("stale preview landed: %+v", dialog)
	}
	runPendingWork(t, app)
	app.applyTakeoverResult(takeoverResult{sequence: stalePreview, outcome: issue.TriageOutcome{Agent: "old"}})
	if dialog.failed || dialog.message != "" {
		t.Fatalf("stale result landed: %+v", dialog)
	}
}

func TestIssuesIndexRefreshIsThrottled(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	app, _ := takeoverApp(t, fake, nil, nil, func(context.Context, issue.Repository, int, issue.TriageOptions) (issue.TriageOutcome, error) {
		return issue.TriageOutcome{}, nil
	})
	now := time.Date(2026, 9, 11, 4, 0, 0, 0, time.UTC)
	app.Now = func() time.Time { return now }
	scans := 0
	app.ImportIndex = func() (issue.Index, error) {
		scans++
		return issue.Index{}, nil
	}
	app.HandleKey("g")
	runPendingWork(t, app)
	if scans != 1 {
		t.Fatalf("the list load must read the index once: %d", scans)
	}

	app.issuesTick()
	if app.pendingWork != nil {
		t.Fatal("no request is due on the very first tick")
	}

	// The armed content refresh is served by the debounce grid, and it must not
	// turn into an index scan.
	now = now.Add(uiTickInterval)
	app.issuesTick()
	if app.pendingWork == nil {
		t.Fatal("the content refresh was not served")
	}
	runPendingWork(t, app)

	now = now.Add(issuesIndexInterval - uiTickInterval - time.Millisecond)
	app.issuesTick()
	if app.pendingWork != nil {
		t.Fatal("a tick below the throttle window queued a scan")
	}

	now = now.Add(2 * time.Millisecond)
	app.issuesTick()
	if app.pendingWork == nil {
		t.Fatal("the index scan was not queued after the throttle window")
	}
	runPendingWork(t, app)
	if scans != 2 {
		t.Fatalf("scans=%d", scans)
	}
}
