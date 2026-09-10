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
)

type fakeIssues struct {
	repository issue.Repository
	resolveErr error

	listResult func(call int, query issue.IssueQuery) (issue.IssuePage, error)
	getResult  func(call int, number int, withComments bool) (issue.IssueSnapshot, error)

	resolveCalls int
	listCalls    int
	getCalls     int
	queries      []issue.IssueQuery
	numbers      []int
	withComments []bool
}

func newFakeIssues() *fakeIssues {
	return &fakeIssues{repository: issue.Repository{
		Host: "github.com", Owner: "dualface", Name: "kander",
		URL: "https://github.com/dualface/kander", Remote: "origin",
	}}
}

func (f *fakeIssues) ResolveRepository(context.Context, string, string) (issue.Repository, error) {
	f.resolveCalls++
	return f.repository, f.resolveErr
}

func (f *fakeIssues) ListIssues(_ context.Context, _ issue.Repository, query issue.IssueQuery) (issue.IssuePage, error) {
	f.listCalls++
	f.queries = append(f.queries, query)
	if f.listResult == nil {
		return issue.IssuePage{Repository: f.repository, Limit: query.Limit}, nil
	}
	return f.listResult(f.listCalls, query)
}

func (f *fakeIssues) GetIssue(_ context.Context, _ issue.Repository, number int, withComments bool) (issue.IssueSnapshot, error) {
	f.getCalls++
	f.numbers = append(f.numbers, number)
	f.withComments = append(f.withComments, withComments)
	if f.getResult == nil {
		return issue.IssueSnapshot{}, nil
	}
	return f.getResult(f.getCalls, number, withComments)
}

func issuesTestApp(t *testing.T, provider issue.IssueProvider, width, height int) *App {
	t.Helper()
	task := Task{TaskID: "task-1", Title: "Task", State: "working", Document: "- WINDOW: herdr:w1:t2:w1:p3\n"}
	app := newApp(true, 30, tuiPageContext(), func() (BoardPayload, error) {
		return BoardPayload{Tasks: []Task{task}}, nil
	}, func(string) (Task, error) { return task, nil }, "dark", 1, nil, nil)
	app.Width, app.Height = width, height
	app.IssueProvider = func() issue.IssueProvider { return provider }
	app.OpenBrowser = func(string) error { return nil }
	app.refreshBoard()
	return app
}

func defaultPage(limit int) func(int, issue.IssueQuery) (issue.IssuePage, error) {
	return func(_ int, query issue.IssueQuery) (issue.IssuePage, error) {
		return issue.IssuePage{Limit: query.Limit, Issues: []issue.IssueSummary{
			{Number: 42, Title: "Fix the widget crash", State: "open", Labels: []string{"bug"}, UpdatedAt: time.Date(2026, 9, 11, 2, 3, 0, 0, time.UTC)},
			{Number: 41, Title: "Docs update", State: "open", Labels: []string{"docs"}, UpdatedAt: time.Date(2026, 9, 10, 1, 0, 0, 0, time.UTC)},
		}}, nil
	}
}

func defaultSnapshot(t *testing.T) issue.IssueSnapshot {
	t.Helper()
	return issue.IssueSnapshot{
		Number: 42, Title: "Fix the widget crash", State: "open", Author: "alice",
		URL:            "https://github.com/dualface/kander/issues/42",
		Body:           "Steps to reproduce:\n\n1. open the board\n",
		CreatedAt:      time.Date(2026, 9, 10, 1, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2026, 9, 11, 2, 3, 0, 0, time.UTC),
		FetchedAt:      time.Date(2026, 9, 11, 3, 0, 0, 0, time.UTC),
		CommentsLoaded: true,
		Comments: []issue.IssueComment{
			{Author: "carol", Body: "Confirmed on Linux.", CreatedAt: time.Date(2026, 9, 11, 2, 10, 0, 0, time.UTC)},
		},
	}
}

// runPendingWork executes the queued background task and applies its result,
// exactly like program.Update does for a workMsg.
func runPendingWork(t *testing.T, app *App) {
	t.Helper()
	cmd := app.takePending()
	if cmd == nil {
		t.Fatal("no pending work was queued")
	}
	message := cmd()
	work, ok := message.(workMsg)
	if !ok {
		t.Fatalf("unexpected message %T", message)
	}
	app.applyWork(work.payload)
}

func TestIssuesOpenLoadsListAndClosesCleanly(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	app := issuesTestApp(t, fake, 120, 30)
	app.Model.SelectTaskIndex("working", 0)
	selected, scroll := app.Model.SelectedIndexes["working"], app.Model.Scrolls["working"]

	app.HandleKey("g")
	if app.Issues == nil || !app.Issues.loading || app.Issues.state != issue.IssueStateOpen {
		t.Fatalf("overlay did not open for loading: %+v", app.Issues)
	}
	if fake.listCalls != 0 {
		t.Fatal("list request must run in the background")
	}
	runPendingWork(t, app)
	if len(app.Issues.items) != 2 || app.Issues.loading {
		t.Fatalf("items=%d loading=%v", len(app.Issues.items), app.Issues.loading)
	}
	if fake.queries[0].Limit != issuesListLimit || fake.queries[0].State != issue.IssueStateOpen {
		t.Fatalf("query=%+v", fake.queries[0])
	}
	view := ansi.Strip(app.View())
	for _, want := range []string{"#42", "Fix the widget crash", "bug"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, app.Context.issueStateLabel(issue.IssueStateOpen)) {
		t.Fatalf("view missing the state filter:\n%s", view)
	}

	app.HandleKey("esc")
	if app.Issues != nil {
		t.Fatal("esc must close the overlay")
	}
	if app.Model.SelectedIndexes["working"] != selected || app.Model.Scrolls["working"] != scroll {
		t.Fatalf("board state changed: selected=%d/%d scroll=%d/%d", app.Model.SelectedIndexes["working"], selected, app.Model.Scrolls["working"], scroll)
	}
	if strings.Contains(ansi.Strip(app.View()), "#42") {
		t.Fatal("issues popup survived closing")
	}
}

func TestIssuesStateCycleAndFilters(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	app := issuesTestApp(t, fake, 120, 30)
	app.HandleKey("g")
	runPendingWork(t, app)

	app.HandleKey("tab")
	runPendingWork(t, app)
	if fake.queries[len(fake.queries)-1].State != issue.IssueStateClosed {
		t.Fatalf("tab must move to closed: %+v", fake.queries)
	}
	app.HandleKey("tab")
	runPendingWork(t, app)
	if fake.queries[len(fake.queries)-1].State != issue.IssueStateAll {
		t.Fatalf("tab must move to all: %+v", fake.queries)
	}
	app.HandleKey("tab")
	runPendingWork(t, app)
	if fake.queries[len(fake.queries)-1].State != issue.IssueStateOpen {
		t.Fatalf("tab must wrap to open: %+v", fake.queries)
	}

	app.HandleKey("/")
	for _, key := range []string{"c", "r", "a", "s", "h"} {
		app.HandleKey(key)
	}
	app.HandleKey("enter")
	runPendingWork(t, app)
	if got := fake.queries[len(fake.queries)-1].Search; got != "crash" {
		t.Fatalf("search=%q", got)
	}

	app.HandleKey("l")
	app.HandleKey("b")
	app.HandleKey("u")
	app.HandleKey("g")
	app.HandleKey("enter")
	runPendingWork(t, app)
	last := fake.queries[len(fake.queries)-1]
	if len(last.Labels) != 1 || last.Labels[0] != "bug" {
		t.Fatalf("labels=%v", last.Labels)
	}

	// Clearing the label restores the unfiltered query.
	app.HandleKey("l")
	app.HandleKey("backspace")
	app.HandleKey("backspace")
	app.HandleKey("backspace")
	app.HandleKey("enter")
	runPendingWork(t, app)
	if len(fake.queries[len(fake.queries)-1].Labels) != 0 {
		t.Fatalf("labels=%v", fake.queries[len(fake.queries)-1].Labels)
	}
	// Escaping the input must not apply the typed value.
	app.HandleKey("l")
	app.HandleKey("d")
	app.HandleKey("esc")
	app.HandleKey("r")
	runPendingWork(t, app)
	if len(fake.queries[len(fake.queries)-1].Labels) != 0 {
		t.Fatalf("esc applied a label: %v", fake.queries[len(fake.queries)-1].Labels)
	}
}

func TestIssuesRefreshKeepsFilters(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	app := issuesTestApp(t, fake, 120, 30)
	app.HandleKey("g")
	runPendingWork(t, app)
	app.HandleKey("r")
	runPendingWork(t, app)
	if fake.listCalls != 2 {
		t.Fatalf("refresh did not reload: %d", fake.listCalls)
	}
}

func TestIssuesEnterLoadsDetailWithComments(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	fake.getResult = func(_ int, number int, withComments bool) (issue.IssueSnapshot, error) {
		if number != 42 || !withComments {
			t.Fatalf("number=%d withComments=%v", number, withComments)
		}
		return defaultSnapshot(t), nil
	}
	app := issuesTestApp(t, fake, 120, 30)
	app.HandleKey("g")
	runPendingWork(t, app)
	app.HandleKey("enter")
	if fake.getCalls != 0 {
		t.Fatal("detail request must run in the background")
	}
	runPendingWork(t, app)
	if app.Issues.detail == nil || app.Issues.detail.Number != 42 {
		t.Fatalf("detail=%+v", app.Issues.detail)
	}
	view := ansi.Strip(app.View())
	for _, want := range []string{"Fix the widget crash", "Steps to reproduce", "Confirmed on Linux.", "carol"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestIssuesStaleResultsAreDropped(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = func(call int, query issue.IssueQuery) (issue.IssuePage, error) {
		title := "first"
		if call > 1 {
			title = "second"
		}
		return issue.IssuePage{Limit: query.Limit, Issues: []issue.IssueSummary{
			{Number: call, Title: title, State: "open", UpdatedAt: time.Now()},
		}}, nil
	}
	app := issuesTestApp(t, fake, 120, 30)
	app.HandleKey("g")
	stale := app.pendingWork
	app.HandleKey("tab")
	fresh := app.pendingWork
	if stale == nil || fresh == nil {
		t.Fatal("missing queued work")
	}
	// Run the stale request first and then the fresh one; only the newest may land.
	stalePayload := stale()
	freshPayload := fresh()
	app.applyWork(freshPayload)
	app.applyWork(stalePayload)
	if len(app.Issues.items) != 1 || app.Issues.items[0].Title != "second" {
		t.Fatalf("items=%+v", app.Issues.items)
	}

	// A result that lands after the overlay closed must not resurrect it.
	app.HandleKey("esc")
	app.applyWork(stalePayload)
	if app.Issues != nil {
		t.Fatal("closed overlay was resurrected by a stale result")
	}
}

func TestIssuesStaleDetailDropped(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	fake.getResult = func(call int, number int, _ bool) (issue.IssueSnapshot, error) {
		snapshot := defaultSnapshot(t)
		snapshot.Number = number
		snapshot.Title = "detail call " + itoa(call)
		return snapshot, nil
	}
	app := issuesTestApp(t, fake, 120, 30)
	app.HandleKey("g")
	runPendingWork(t, app)
	app.HandleKey("enter")
	stale := app.pendingWork
	app.HandleKey("down")
	app.HandleKey("enter")
	fresh := app.pendingWork
	stalePayload := stale()
	freshPayload := fresh()
	// The stale detail belongs to issue 42; the fresh one to issue 41.
	app.applyWork(stalePayload)
	if app.Issues.detail != nil {
		t.Fatalf("stale detail landed: %+v", app.Issues.detail)
	}
	app.applyWork(freshPayload)
	if app.Issues.detail == nil || app.Issues.detail.Number != 41 {
		t.Fatalf("fresh detail missing: %+v", app.Issues.detail)
	}
}

func TestIssuesDetailErrorThenRefresh(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	failing := true
	fake.getResult = func(_ int, _ int, _ bool) (issue.IssueSnapshot, error) {
		if failing {
			return issue.IssueSnapshot{}, &issue.Error{Kind: issue.ErrorTimeout}
		}
		return defaultSnapshot(t), nil
	}
	app := issuesTestApp(t, fake, 120, 30)
	app.HandleKey("g")
	runPendingWork(t, app)
	app.HandleKey("enter")
	runPendingWork(t, app)
	if app.Issues.detailErr == "" || app.Issues.detail != nil {
		t.Fatalf("detailErr=%q detail=%v", app.Issues.detailErr, app.Issues.detail)
	}
	if !strings.Contains(app.Issues.detailErr, "超时") && !strings.Contains(app.Issues.detailErr, "timed out") {
		t.Fatalf("detail error not localized: %q", app.Issues.detailErr)
	}
	failing = false
	app.HandleKey("r")
	runPendingWork(t, app)
	runPendingWork(t, app)
	if app.Issues.detail == nil || app.Issues.detailErr != "" {
		t.Fatalf("refresh did not recover: %+v", app.Issues)
	}
}

func TestIssuesLayoutWideAndNarrow(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	fake.getResult = func(int, int, bool) (issue.IssueSnapshot, error) { return defaultSnapshot(t), nil }

	wide := issuesTestApp(t, fake, 160, 34)
	wide.HandleKey("g")
	runPendingWork(t, wide)
	if !wide.issuesLayout().wide {
		t.Fatal("wide terminal must use two columns")
	}
	wide.HandleKey("enter")
	runPendingWork(t, wide)
	if wide.Issues.showDetail && !wide.issuesLayout().wide {
		t.Fatal("wide layout must keep the list visible next to the detail")
	}
	view := ansi.Strip(wide.View())
	for _, want := range []string{"#42", "Fix the widget crash", "Steps to reproduce"} {
		if !strings.Contains(view, want) {
			t.Fatalf("wide view missing %q:\n%s", want, view)
		}
	}

	narrow := issuesTestApp(t, fake, 80, 34)
	narrow.HandleKey("g")
	runPendingWork(t, narrow)
	if narrow.issuesLayout().wide {
		t.Fatal("narrow terminal must use pages")
	}
	firstPage := ansi.Strip(narrow.View())
	if !strings.Contains(firstPage, "#42") || strings.Contains(firstPage, "Steps to reproduce") {
		t.Fatalf("narrow list page wrong:\n%s", firstPage)
	}
	narrow.HandleKey("enter")
	runPendingWork(t, narrow)
	detailPage := ansi.Strip(narrow.View())
	if !strings.Contains(detailPage, "Steps to reproduce") {
		t.Fatalf("narrow detail page missing body:\n%s", detailPage)
	}
	narrow.HandleKey("esc")
	if narrow.Issues == nil || narrow.Issues.showDetail {
		t.Fatal("esc must return to the list page before closing")
	}
	narrow.HandleKey("esc")
	if narrow.Issues != nil {
		t.Fatal("second esc must close the overlay")
	}
}

func TestIssuesMouseSelectsAndOpens(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	fake.getResult = func(int, int, bool) (issue.IssueSnapshot, error) { return defaultSnapshot(t), nil }
	app := issuesTestApp(t, fake, 160, 34)
	app.HandleKey("g")
	runPendingWork(t, app)
	layout := app.issuesLayout()
	secondRow := layout.body.Y + issuesListHeader + issuesItemLines
	app.HandleMouse(layout.body.X+2, secondRow, mouseBtn1Clicked)
	if app.Issues.selected != 1 {
		t.Fatalf("selected=%d", app.Issues.selected)
	}
	app.HandleMouse(layout.body.X+2, secondRow, mouseBtn1Clicked|mouseBtn1Double)
	if fake.getCalls == 0 && app.pendingWork == nil {
		t.Fatal("double click must request the detail")
	}
	runPendingWork(t, app)
	if app.Issues.detailNumber != 41 {
		t.Fatalf("detail number=%d", app.Issues.detailNumber)
	}
	// The wheel scrolls the list when the pointer is over it.
	app.HandleMouse(layout.body.X+2, layout.body.Y+2, mouseBtn4Pressed)
	if app.Issues.selected != 0 {
		t.Fatalf("wheel up must move the selection: %d", app.Issues.selected)
	}
}

func TestIssuesBrowserUsesCanonicalURL(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	app := issuesTestApp(t, fake, 120, 30)
	var opened string
	app.OpenBrowser = func(target string) error {
		opened = target
		return nil
	}
	app.HandleKey("g")
	runPendingWork(t, app)
	app.HandleKey("o")
	if opened != "https://github.com/dualface/kander/issues/42" {
		t.Fatalf("opened=%q", opened)
	}
	if app.Issues.notice != app.Context.IssuesBrowserOpened {
		t.Fatalf("notice=%q want %q", app.Issues.notice, app.Context.IssuesBrowserOpened)
	}
	if !strings.Contains(ansi.Strip(app.View()), app.Context.IssuesBrowserOpened) {
		t.Fatal("browser notice missing from the view")
	}

	// A remote identity that does not match its URL is never opened.
	fake.repository.URL = "https://evil.example/dualface/kander"
	opened = ""
	app2 := issuesTestApp(t, fake, 120, 30)
	app2.OpenBrowser = func(target string) error { opened = target; return nil }
	app2.HandleKey("g")
	runPendingWork(t, app2)
	app2.HandleKey("o")
	if opened != "" {
		t.Fatalf("untrusted URL was opened: %q", opened)
	}
	if app2.Issues.notice == "" {
		t.Fatal("rejected browser open must be reported")
	}
}

func TestIssuesBrowserFailureIsReported(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	app := issuesTestApp(t, fake, 120, 30)
	app.OpenBrowser = func(string) error { return errors.New("no opener") }
	app.HandleKey("g")
	runPendingWork(t, app)
	app.HandleKey("o")
	if !strings.Contains(app.Issues.notice, "no opener") {
		t.Fatalf("notice=%q", app.Issues.notice)
	}
}

func TestIssuesHostileRemoteTextIsSanitized(t *testing.T) {
	fake := newFakeIssues()
	hostile := "\x1b[31mred\x1b[0m \x1b]8;;https://evil.example\x07link\x1b]8;;\x07 \u202eevil\u202c"
	fake.listResult = func(_ int, query issue.IssueQuery) (issue.IssuePage, error) {
		return issue.IssuePage{Limit: query.Limit, Issues: []issue.IssueSummary{
			{Number: 42, Title: "hostile " + hostile, State: "open", Labels: []string{hostile}, UpdatedAt: time.Now()},
		}}, nil
	}
	app := issuesTestApp(t, fake, 120, 30)
	app.HandleKey("g")
	runPendingWork(t, app)
	view := ansi.Strip(app.View())
	if strings.Contains(view, "evil.example") || strings.ContainsRune(view, '\u202e') {
		t.Fatalf("hostile remote text reached the screen:\n%q", view)
	}
}

func TestIssuesBoardKeysAndHelp(t *testing.T) {
	entries := map[string]string{}
	for _, entry := range boardHelpGroups()[0].Entries {
		entries[entry.Keys] = entry.Desc
	}
	if _, ok := entries["g"]; !ok {
		t.Fatal("board help must document g for issues")
	}
	if _, ok := entries["f"]; !ok {
		t.Fatal("board help must document f for the agent window")
	}
	groups := boardHelpGroups()
	foundIssues := false
	for _, group := range groups {
		if group.Title == config.Text("tui.issues_overlay") {
			foundIssues = true
		}
	}
	if !foundIssues {
		t.Fatal("help must document the issues overlay")
	}

	fake := newFakeIssues()
	app := issuesTestApp(t, fake, 120, 30)
	app.HandleKey("g")
	if app.Issues == nil {
		t.Fatal("g must open the issues overlay")
	}
	app.HandleKey("?")
	if !app.Help || app.Issues == nil {
		t.Fatal("? must open help above the issues overlay")
	}
	if !strings.Contains(ansi.Strip(app.View()), config.Text("tui.key_bindings")) {
		t.Fatal("help overlay missing")
	}
	app.HandleKey("x")
	if app.Help || app.Issues == nil {
		t.Fatal("closing help must return to the issues overlay")
	}
}

func TestIssuesUsesNoProviderWhenUnbound(t *testing.T) {
	app := issuesTestApp(t, nil, 120, 30)
	app.IssueProvider = nil
	app.HandleKey("g")
	runPendingWork(t, app)
	if app.Issues == nil || app.Issues.listErr == "" {
		t.Fatalf("missing provider must be reported: %+v", app.Issues)
	}
}

func TestIssuesRendersInLightAndDarkThemes(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	fake.getResult = func(int, int, bool) (issue.IssueSnapshot, error) {
		snapshot := defaultSnapshot(t)
		snapshot.Body = "[link](https://evil.example) and plain text"
		return snapshot, nil
	}
	for _, theme := range []string{"dark", "light"} {
		t.Run(theme, func(t *testing.T) {
			app := issuesTestApp(t, fake, 120, 30)
			app.Theme = theme
			app.HandleKey("g")
			runPendingWork(t, app)
			app.HandleKey("enter")
			runPendingWork(t, app)
			raw := app.View()
			if strings.Contains(raw, "\x1b]") {
				t.Fatal("an OSC sequence (including terminal hyperlinks) reached the screen")
			}
			plain := ansi.Strip(raw)
			for _, want := range []string{"#42", "Fix the widget crash", "plain text", "Confirmed on Linux."} {
				if !strings.Contains(plain, want) {
					t.Fatalf("%s view missing %q:\n%s", theme, want, plain)
				}
			}
		})
	}
}
