package tui

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/issue"
)

// importTestIndex builds the board mapping the TUI shows beside the issue list.
func importTestIndex(repository issue.Repository, number int, taskID string, updatedAt, fetchedAt time.Time) issue.Index {
	key, err := repository.IssueSourceKey(number)
	if err != nil {
		panic(err)
	}
	return issue.Index{key: issue.LocalCard{
		TaskID: taskID, State: "backlog", Path: "/board/backlog/" + taskID,
		IssueUpdatedAt: updatedAt, FetchedAt: fetchedAt,
	}}
}

type importCall struct {
	repository issue.Repository
	number     int
	options    issue.ImportOptions
}

// issuesImportApp wires the overlay to a fake importer and a fixed index loader.
func issuesImportApp(t *testing.T, fake *fakeIssues, tasks []Task, index issue.Index, importer func(context.Context, issue.Repository, int, issue.ImportOptions) (issue.ImportResult, error)) (*App, *[]importCall) {
	t.Helper()
	app := newApp(true, 30, tuiPageContext(), func() (BoardPayload, error) {
		return BoardPayload{Tasks: tasks}, nil
	}, func(string) (Task, error) { return Task{}, errors.New("no detail") }, "dark", 1, nil, nil)
	app.Width, app.Height = 120, 30
	app.IssueProvider = func() issue.IssueProvider { return fake }
	app.ImportIndex = func() (issue.Index, error) { return index, nil }
	calls := &[]importCall{}
	app.ImportIssue = importer
	if importer != nil {
		original := importer
		app.ImportIssue = func(ctx context.Context, repository issue.Repository, number int, options issue.ImportOptions) (issue.ImportResult, error) {
			*calls = append(*calls, importCall{repository: repository, number: number, options: options})
			return original(ctx, repository, number, options)
		}
	}
	app.refreshBoard()
	return app, calls
}

func TestIssuesImportJumpsToTheBoundCard(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	task := Task{TaskID: "task-1", Title: "Task", State: "working", Document: "- WINDOW: herdr:w1:t2:w1:p3\n"}
	index := importTestIndex(fake.repository, 42, "task-1",
		time.Date(2026, 9, 11, 2, 3, 0, 0, time.UTC), time.Date(2026, 9, 11, 3, 0, 0, 0, time.UTC))
	app, calls := issuesImportApp(t, fake, []Task{task}, index, func(context.Context, issue.Repository, int, issue.ImportOptions) (issue.ImportResult, error) {
		t.Fatal("a bound issue must not be imported again")
		return issue.ImportResult{}, nil
	})
	app.HandleKey("g")
	runPendingWork(t, app)
	view := ansi.Strip(app.View())
	if !strings.Contains(view, "task-1") {
		t.Fatalf("list does not mark the imported card:\n%s", view)
	}
	app.HandleKey("i")
	if app.Issues != nil {
		t.Fatal("jump did not close the overlay")
	}
	if app.Model.CurrentState() != "working" {
		t.Fatalf("focus is on %s", app.Model.CurrentState())
	}
	if selected := app.Model.SelectedTask(); selected == nil || selected.TaskID != "task-1" {
		t.Fatalf("selection %+v", selected)
	}
	if len(*calls) != 0 {
		t.Fatalf("jump imported the issue: %+v", *calls)
	}
}

func TestIssuesImportJumpsIntoTheArchivedColumn(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	tasks := []Task{
		{TaskID: "task-1", Title: "Active", State: "working"},
		{TaskID: "task-9", Title: "Old", State: "archived"},
	}
	index := importTestIndex(fake.repository, 42, "task-9",
		time.Date(2026, 9, 11, 2, 3, 0, 0, time.UTC), time.Date(2026, 9, 11, 3, 0, 0, 0, time.UTC))
	app, _ := issuesImportApp(t, fake, tasks, index, nil)
	app.HandleKey("g")
	runPendingWork(t, app)
	app.HandleKey("i")
	if app.Issues != nil {
		t.Fatal("jump did not close the overlay")
	}
	if !app.Model.ShowArchived || app.Model.CurrentState() != "archived" {
		t.Fatalf("archived column not opened: show=%v state=%s", app.Model.ShowArchived, app.Model.CurrentState())
	}
	if selected := app.Model.SelectedTask(); selected == nil || selected.TaskID != "task-9" {
		t.Fatalf("selection %+v", selected)
	}
}

func TestIssuesImportReportsMissingCard(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	index := importTestIndex(fake.repository, 42, "task-gone",
		time.Date(2026, 9, 11, 2, 3, 0, 0, time.UTC), time.Date(2026, 9, 11, 3, 0, 0, 0, time.UTC))
	app, _ := issuesImportApp(t, fake, []Task{{TaskID: "task-1", Title: "Task", State: "working"}}, index, nil)
	app.HandleKey("g")
	runPendingWork(t, app)
	app.HandleKey("i")
	if app.Issues == nil || !strings.Contains(app.Issues.notice, "task-gone") {
		t.Fatalf("notice %q", app.Issues.notice)
	}
}

func TestIssuesImportCreatesTheCardThroughPendingWork(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	index := issue.Index{}
	app, calls := issuesImportApp(t, fake, []Task{{TaskID: "task-1", Title: "Task", State: "working"}}, index,
		func(_ context.Context, repository issue.Repository, number int, options issue.ImportOptions) (issue.ImportResult, error) {
			return issue.ImportResult{
				TaskID: "20260911-gh-dualface-kander-42-task", State: "backlog", Existing: false,
				SourceKey: "github://github.com/dualface/kander/issues/42",
				SourceURL: "https://github.com/dualface/kander/issues/42",
			}, nil
		})
	app.HandleKey("g")
	runPendingWork(t, app)
	app.HandleKey("i")
	if len(*calls) != 0 {
		t.Fatal("import ran on the UI thread")
	}
	if app.Issues == nil || !app.Issues.importing {
		t.Fatalf("importing flag %+v", app.Issues)
	}
	runPendingWork(t, app)
	if len(*calls) != 1 {
		t.Fatalf("calls %+v", *calls)
	}
	call := (*calls)[0]
	if call.number != 42 || call.repository.Owner != "dualface" || call.options.Comments {
		t.Fatalf("call %+v", call)
	}
	if app.Issues.importing {
		t.Fatal("importing flag was not cleared")
	}
	if !strings.Contains(app.Issues.notice, "20260911-gh-dualface-kander-42-task") {
		t.Fatalf("notice %q", app.Issues.notice)
	}
}

func TestIssuesImportWithCommentsAndFailure(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	app, calls := issuesImportApp(t, fake, []Task{{TaskID: "task-1", Title: "Task", State: "working"}}, issue.Index{},
		func(_ context.Context, _ issue.Repository, number int, options issue.ImportOptions) (issue.ImportResult, error) {
			if number == 42 {
				if !options.Comments {
					t.Fatal("I must request comments")
				}
				return issue.ImportResult{TaskID: "task-42", State: "backlog", CommentsLoaded: true}, nil
			}
			return issue.ImportResult{}, errors.New("gh: rate limited\x1b[2J")
		})
	app.HandleKey("g")
	runPendingWork(t, app)
	app.HandleKey("I")
	runPendingWork(t, app)
	if len(*calls) != 1 || !(*calls)[0].options.Comments {
		t.Fatalf("calls %+v", *calls)
	}
	app.HandleKey("down")
	app.HandleKey("i")
	runPendingWork(t, app)
	notice := app.Issues.notice
	if notice == "" || strings.ContainsAny(notice, "\x1b") || !strings.Contains(notice, config.Text("tui.issues_import_failed")) {
		t.Fatalf("notice %q", notice)
	}
	if app.Issues.importing {
		t.Fatal("failed import left the importing flag set")
	}
	if !strings.Contains(ansi.Strip(app.View()), config.Text("tui.issues_import_failed")) {
		t.Fatal("the notice is not rendered")
	}
}

func TestIssuesImportStaleResultIsDropped(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	app, calls := issuesImportApp(t, fake, []Task{{TaskID: "task-1", Title: "Task", State: "working"}}, issue.Index{},
		func(_ context.Context, _ issue.Repository, _ int, _ issue.ImportOptions) (issue.ImportResult, error) {
			return issue.ImportResult{TaskID: "task-42", State: "backlog"}, nil
		})
	app.HandleKey("g")
	runPendingWork(t, app)
	app.HandleKey("i")
	stale := app.pendingWork
	if stale == nil {
		t.Fatal("no import queued")
	}
	// Select another issue before the result lands.
	app.HandleKey("down")
	pending := app.Issues.notice
	payload := stale()
	app.applyWork(payload)
	if len(*calls) != 1 {
		t.Fatalf("calls %+v", *calls)
	}
	if app.Issues.notice != pending {
		t.Fatalf("stale result changed the notice: %q", app.Issues.notice)
	}
	if strings.Contains(app.Issues.notice, "task-42") {
		t.Fatalf("stale result reported another issue: %q", app.Issues.notice)
	}
	if app.Issues.importing {
		t.Fatal("stale result left the importing flag set")
	}
}

func TestIssuesImportUpdateMarker(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	index := importTestIndex(fake.repository, 42, "task-1",
		time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 10, 1, 0, 0, 0, time.UTC))
	app, _ := issuesImportApp(t, fake, []Task{{TaskID: "task-1", Title: "Task", State: "working"}}, index, nil)
	app.HandleKey("g")
	runPendingWork(t, app)
	view := ansi.Strip(app.View())
	if !strings.Contains(view, config.Text("tui.issues_imported", "task-1", config.Text("tui.backlog"))) || !strings.Contains(view, config.Text("tui.issues_import_update")) {
		t.Fatalf("update marker missing:\n%s", view)
	}
	// Without a newer remote revision the marker stays quiet.
	fresh := newFakeIssues()
	fresh.listResult = defaultPage(issuesListLimit)
	freshIndex := importTestIndex(fresh.repository, 42, "task-1",
		time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 11, 3, 0, 0, 0, time.UTC))
	freshApp, _ := issuesImportApp(t, fresh, []Task{{TaskID: "task-1", Title: "Task", State: "working"}}, freshIndex, nil)
	freshApp.HandleKey("g")
	runPendingWork(t, freshApp)
	freshView := ansi.Strip(freshApp.View())
	if strings.Contains(freshView, config.Text("tui.issues_import_update")) {
		t.Fatalf("fresh card reported an update:\n%s", freshView)
	}
}

func TestIssuesImportWritesNothingToTheTerminal(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	app, _ := issuesImportApp(t, fake, []Task{{TaskID: "task-1", Title: "Task", State: "working"}}, issue.Index{},
		func(_ context.Context, _ issue.Repository, _ int, _ issue.ImportOptions) (issue.ImportResult, error) {
			return issue.ImportResult{TaskID: "task-42", State: "backlog"}, nil
		})
	app.HandleKey("g")
	runPendingWork(t, app)
	app.HandleKey("i")
	cmd := app.takePending()
	if cmd == nil {
		t.Fatal("no import queued")
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout, oldStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = writer, writer
	message := cmd()
	os.Stdout, os.Stderr = oldStdout, oldStderr
	writer.Close()
	written, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 0 {
		t.Fatalf("background import wrote to the terminal: %q", written)
	}
	work, ok := message.(workMsg)
	if !ok {
		t.Fatalf("unexpected message %T", message)
	}
	app.applyWork(work.payload)
	if app.Issues == nil || app.Issues.notice == "" {
		t.Fatal("result was not applied")
	}
}

func TestIssuesImportWithoutImporterReportsFailure(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	app, _ := issuesImportApp(t, fake, []Task{{TaskID: "task-1", Title: "Task", State: "working"}}, issue.Index{}, nil)
	app.HandleKey("g")
	runPendingWork(t, app)
	app.HandleKey("i")
	if app.Issues == nil || !strings.Contains(app.Issues.notice, config.Text("tui.issues_import_failed")) {
		t.Fatalf("notice %q", app.Issues.notice)
	}
}

// tuiImportBoard creates an isolated board and configuration for the one test
// that drives the real import service instead of a fake.
func tuiImportBoard(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, state := range board.States {
		if err := os.Mkdir(filepath.Join(root, state), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(board.EnvBoardDir, root)
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Language = "cn"
	cfg.AgentLanguage = "zh-CN"
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestIssuesImportSharesTheCLIService(t *testing.T) {
	root := tuiImportBoard(t)
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	fake.getResult = func(_ int, number int, _ bool) (issue.IssueSnapshot, error) {
		snapshot := defaultSnapshot(t)
		snapshot.Number = number
		snapshot.Repository = fake.repository
		snapshot.URL = "https://github.com/dualface/kander/issues/42"
		return snapshot, nil
	}
	app := newApp(true, 30, tuiPageContext(), func() (BoardPayload, error) {
		return BoardPayload{Tasks: []Task{{TaskID: "task-1", Title: "Task", State: "working"}}}, nil
	}, func(string) (Task, error) { return Task{}, errors.New("no detail") }, "dark", 1, nil, nil)
	app.Width, app.Height = 120, 30
	app.IssueProvider = func() issue.IssueProvider { return fake }
	app.ImportIndex = func() (issue.Index, error) { return issue.LoadIndex(root) }
	app.ImportIssue = func(ctx context.Context, repository issue.Repository, number int, options issue.ImportOptions) (issue.ImportResult, error) {
		if strings.TrimSpace(options.Language) == "" {
			options.Language = configuredAgentLanguage()
		}
		return issue.Import(ctx, fake, root, repository, number, options)
	}
	app.refreshBoard()

	app.HandleKey("g")
	runPendingWork(t, app)
	app.HandleKey("i")
	runPendingWork(t, app)
	if app.Issues == nil || app.Issues.notice == "" {
		t.Fatal("the import result was not reported")
	}
	index, err := issue.LoadIndex(root)
	if err != nil {
		t.Fatal(err)
	}
	key, err := fake.repository.IssueSourceKey(42)
	if err != nil {
		t.Fatal(err)
	}
	local, ok := index[key]
	if !ok {
		t.Fatalf("index after the TUI import: %+v", index)
	}
	// The CLI entry point resolves to the same card without a second fetch.
	before := fake.getCalls
	result, err := issue.Import(context.Background(), fake, root, fake.repository, 42, issue.ImportOptions{Language: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Existing || result.TaskID != local.TaskID {
		t.Fatalf("CLI import %+v vs TUI card %+v", result, local)
	}
	if fake.getCalls != before {
		t.Fatal("the repeated import fetched the issue again")
	}
	data, err := os.ReadFile(filepath.Join(root, "backlog", local.TaskID, "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"- SIZE: small", "## ACCEPTANCE_CRITERIA", "source/github-issue.md"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("card missing %q:\n%s", want, data)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "backlog", local.TaskID, "source", "github-issue.json")); err != nil {
		t.Fatal(err)
	}
}
