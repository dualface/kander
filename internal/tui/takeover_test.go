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

	view := ansi.Strip(app.View())
	if !strings.Contains(view, "i import") {
		t.Fatalf("unbound hint missing:\n%s", view)
	}
	if strings.Contains(view, "g jump to card") {
		t.Fatalf("bound hint shown for an unbound issue:\n%s", view)
	}
	app.HandleKey("g")
	if app.Issues == nil {
		t.Fatal("g must not jump when the issue is unbound")
	}

	app.HandleKey("s")
	dialog := app.Takeover
	if dialog == nil || dialog.phase != confirmLoading {
		t.Fatalf("dialog=%+v", dialog)
	}
	if dialog.number != 42 {
		t.Fatalf("dialog=%+v", dialog)
	}
	if !strings.Contains(ansi.Strip(app.View()), config.Text("tui.issues_takeover_loading")) {
		t.Fatalf("loading dialog missing\n%s", ansi.Strip(app.View()))
	}
	if len(*calls) != 0 {
		t.Fatal("the session must not start before the confirmation")
	}

	runPendingWork(t, app)
	if dialog.phase != confirmReady || dialog.agent != "claude" || dialog.launcher != "tmux" {
		t.Fatalf("dialog=%+v", dialog)
	}
	view = ansi.Strip(app.View())
	for _, want := range []string{
		config.Text("tui.issues_takeover_title", "42"),
		"dualface/kander#42",
		config.Text("dialog.settings", "claude", "tmux"),
		config.Text("dialog.keys"),
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("dialog missing %q:\n%s", want, view)
		}
	}

	app.HandleKey("y")
	if dialog.phase != confirmRunning {
		t.Fatalf("dialog=%+v", dialog)
	}
	runPendingWork(t, app)
	if dialog.phase != confirmFinished || dialog.failed {
		t.Fatalf("dialog=%+v", dialog)
	}
	want := config.Text("tui.issues_takeover_started", "claude", "tmux", "session:win:pane")
	if dialog.message != want {
		t.Fatalf("message=%q want=%q", dialog.message, want)
	}
	if !strings.Contains(ansi.Strip(app.View()), config.Text("dialog.title_done")) {
		t.Fatalf("result dialog missing:\n%s", ansi.Strip(app.View()))
	}
	if len(*calls) != 1 {
		t.Fatalf("calls=%+v", *calls)
	}
	call := (*calls)[0]
	// The start uses exactly the agent and launcher the dialog showed.
	if call.number != 42 || call.options.CardID != "" || call.repository.Name != fake.repository.Name ||
		call.options.Agent != "claude" || call.options.Launcher != "tmux" {
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

func TestIssuesBoundSelectionUsesJumpKeyOnly(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	task := Task{TaskID: "task-1", Title: "Task", State: "backlog", Document: "- WINDOW: herdr:w1:t2:w1:p3\n"}
	index := importTestIndex(fake.repository, 42, "task-1",
		time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 10, 1, 0, 0, 0, time.UTC))
	app, _ := takeoverApp(t, fake, []Task{task}, index, func(context.Context, issue.Repository, int, issue.TriageOptions) (issue.TriageOutcome, error) {
		t.Fatal("bound issues must not start takeover from the overlay")
		return issue.TriageOutcome{}, nil
	})
	app.HandleKey("g")
	runPendingWork(t, app)

	keys := map[string]string{}
	for _, entry := range app.issuesHelpEntries() {
		keys[entry.Keys] = entry.Desc
	}
	if keys["g"] != config.Text("tui.jump_to_local_card") {
		t.Fatalf("help missing jump key: %+v", keys)
	}
	for _, unexpected := range []string{"i", "I", "s"} {
		if _, ok := keys[unexpected]; ok {
			t.Fatalf("help still shows %s: %+v", unexpected, keys)
		}
	}
	if !strings.Contains(ansi.Strip(app.View()), "g jump to card") {
		t.Fatalf("bound footer hint missing:\n%s", ansi.Strip(app.View()))
	}
}

func TestIssuesHintFollowsSelection(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = func(_ int, query issue.IssueQuery) (issue.IssuePage, error) {
		return issue.IssuePage{Limit: query.Limit, Issues: []issue.IssueSummary{
			{Number: 41, Title: "Unbound", State: "open", UpdatedAt: time.Now()},
			{Number: 42, Title: "Bound", State: "open", UpdatedAt: time.Now()},
		}}, nil
	}
	task := Task{TaskID: "task-1", Title: "Task", State: "working"}
	index := importTestIndex(fake.repository, 42, "task-1",
		time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 10, 1, 0, 0, 0, time.UTC))
	app, _ := issuesImportApp(t, fake, []Task{task}, index, nil)
	app.HandleKey("g")
	runPendingWork(t, app)

	view := ansi.Strip(app.View())
	if !strings.Contains(view, "i import") {
		t.Fatalf("first unbound row should show import keys:\n%s", view)
	}
	app.HandleKey("j")
	view = ansi.Strip(app.View())
	if !strings.Contains(view, "g jump to card") {
		t.Fatalf("bound row should show jump key:\n%s", view)
	}
	app.HandleKey("k")
	view = ansi.Strip(app.View())
	if !strings.Contains(view, "i import") {
		t.Fatalf("returning to unbound should restore import keys:\n%s", view)
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
	if dialog == nil || dialog.phase != confirmFinished || !dialog.failed {
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
	if dialog == nil || dialog.phase != confirmFinished || dialog.message != config.Text("tui.issues_takeover_unavailable") {
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
	app.applyTakeoverPreview(confirmWork{sequence: stalePreview, payload: launch.TriagePreview{Agent: "old", Launcher: "old"}})
	if dialog.agent != "" || dialog.launcher != "" {
		t.Fatalf("stale preview landed: %+v", dialog)
	}
	runPendingWork(t, app)
	app.applyTakeoverResult(confirmWork{sequence: stalePreview, payload: issue.TriageOutcome{Agent: "old"}})
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
