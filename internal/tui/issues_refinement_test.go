package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/launch"
)

func TestIssuesImportRefinementLaunchAndRetry(t *testing.T) {
	for _, key := range []string{"i", "I"} {
		t.Run(key, func(t *testing.T) {
			fake := newFakeIssues()
			fake.listResult = defaultPage(issuesListLimit)
			imports, starts := 0, 0
			app, _ := issuesImportApp(t, fake, nil, nil, func(_ context.Context, _ issue.Repository, _ int, o issue.ImportOptions) (issue.ImportResult, error) {
				imports++
				if !o.Comments {
					t.Fatal("comments omitted")
				}
				return issue.ImportResult{TaskID: "task-42", State: "backlog"}, nil
			})
			app.RefineIssue = func(_ context.Context, r issue.Repository, n int, o issue.TriageOptions) (issue.TriageOutcome, error) {
				starts++
				if imports != 1 || r != fake.repository || n != 42 || o.CardID != "task-42" || o.Agent != "claude" || o.Launcher != "tmux" {
					t.Fatalf("wrong order/target: %d %+v %d %+v", imports, r, n, o)
				}
				if starts == 1 {
					return issue.TriageOutcome{}, errors.New("launcher offline")
				}
				return issue.TriageOutcome{Agent: "claude", Address: "tmux:ready"}, nil
			}
			app.HandleKey("g")
			runPendingWork(t, app)
			app.HandleKey(key)
			app.HandleKey(key)
			if imports != 0 || starts != 0 {
				t.Fatal("work ran on UI thread")
			}
			runPendingWork(t, app)
			if imports != 1 || starts != 1 || !strings.Contains(app.Issues.notice, "launcher offline") {
				t.Fatalf("import=%d launch=%d notice=%q", imports, starts, app.Issues.notice)
			}
			card, bound := app.issuesSelectedBound()
			if !bound || card.TaskID != "task-42" {
				t.Fatal("binding lost after failure")
			}
			app.HandleKey("s")
			app.HandleKey("s")
			runPendingWork(t, app)
			if imports != 1 || starts != 2 || !strings.Contains(app.Issues.notice, "tmux:ready") {
				t.Fatalf("retry import=%d launch=%d notice=%q", imports, starts, app.Issues.notice)
			}
		})
	}
}

func TestIssuesImportFailureAndExistingNeverLaunch(t *testing.T) {
	for _, failed := range []bool{true, false} {
		fake := newFakeIssues()
		fake.listResult = defaultPage(issuesListLimit)
		app, _ := issuesImportApp(t, fake, nil, nil, func(context.Context, issue.Repository, int, issue.ImportOptions) (issue.ImportResult, error) {
			if failed {
				return issue.ImportResult{}, errors.New("fetch failed")
			}
			return issue.ImportResult{TaskID: "existing", State: "backlog", Existing: true}, nil
		})
		app.RefineIssue = func(context.Context, issue.Repository, int, issue.TriageOptions) (issue.TriageOutcome, error) {
			t.Fatal("unexpected launch")
			return issue.TriageOutcome{}, nil
		}
		app.HandleKey("g")
		runPendingWork(t, app)
		app.HandleKey("i")
		runPendingWork(t, app)
		if app.issuesImporting {
			t.Fatal("busy flag stuck")
		}
	}
}

func TestIssuesImportRejectsForegroundAfterKeepingCard(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	app, _ := issuesImportApp(t, fake, nil, nil, func(context.Context, issue.Repository, int, issue.ImportOptions) (issue.ImportResult, error) {
		return issue.ImportResult{TaskID: "saved", State: "backlog"}, nil
	})
	app.PrepareTriage = func() (launch.TriagePreview, error) { return launch.TriagePreview{Launcher: "foreground"}, nil }
	app.RefineIssue = func(context.Context, issue.Repository, int, issue.TriageOptions) (issue.TriageOutcome, error) {
		t.Fatal("foreground launched")
		return issue.TriageOutcome{}, nil
	}
	app.HandleKey("g")
	runPendingWork(t, app)
	app.HandleKey("i")
	runPendingWork(t, app)
	if !strings.Contains(app.Issues.notice, "foreground") {
		t.Fatal(app.Issues.notice)
	}
	if _, ok := app.issuesSelectedBound(); !ok {
		t.Fatal("saved card hidden")
	}
}

func TestIssuesImportCloseReopenKeepsOperationAndFencesReceipt(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	imports, starts := 0, 0
	app, _ := issuesImportApp(t, fake, nil, nil, func(context.Context, issue.Repository, int, issue.ImportOptions) (issue.ImportResult, error) {
		imports++
		return issue.ImportResult{TaskID: "saved", State: "backlog"}, nil
	})
	app.RefineIssue = func(context.Context, issue.Repository, int, issue.TriageOptions) (issue.TriageOutcome, error) {
		starts++
		return issue.TriageOutcome{Agent: "claude", Address: "tmux:original"}, nil
	}
	app.HandleKey("g")
	runPendingWork(t, app)
	app.HandleKey("i")
	work := app.pendingWork
	app.pendingWork = nil
	app.HandleKey("q")
	app.HandleKey("g")
	runPendingWork(t, app)
	app.HandleKey("i")
	app.HandleKey("s")
	if app.pendingWork != nil || app.Takeover != nil {
		t.Fatal("reopened overlay duplicated operation")
	}
	app.applyWork(work())
	if imports != 1 || starts != 1 || app.issuesImporting {
		t.Fatalf("import=%d launch=%d busy=%v", imports, starts, app.issuesImporting)
	}
	if app.Issues.notice != "" || !strings.Contains(app.CopyNotice, "tmux:original") {
		t.Fatalf("receipt on wrong overlay: %q / %q", app.Issues.notice, app.CopyNotice)
	}
}

func TestIssuesRefinementFailureRemainsVisibleAfterNavigation(t *testing.T) {
	for _, navigation := range []string{"down", "q", "reopen"} {
		t.Run(navigation, func(t *testing.T) {
			fake := newFakeIssues()
			fake.listResult = defaultPage(issuesListLimit)
			app, _ := issuesImportApp(t, fake, nil, nil, func(context.Context, issue.Repository, int, issue.ImportOptions) (issue.ImportResult, error) {
				return issue.ImportResult{TaskID: "saved", State: "backlog"}, nil
			})
			starts := 0
			app.RefineIssue = func(context.Context, issue.Repository, int, issue.TriageOptions) (issue.TriageOutcome, error) {
				starts++
				return issue.TriageOutcome{}, errors.New("offline")
			}
			app.HandleKey("g")
			runPendingWork(t, app)
			app.HandleKey("i")
			work := app.pendingWork
			app.pendingWork = nil
			if navigation == "reopen" {
				app.HandleKey("q")
				app.HandleKey("g")
				runPendingWork(t, app)
			} else {
				app.HandleKey(navigation)
			}
			notice := ""
			if app.Issues != nil {
				notice = app.Issues.notice
			}
			app.applyWork(work())
			if !strings.Contains(app.CopyNotice, "offline") || !strings.Contains(app.CopyNotice, "saved") {
				t.Fatalf("failure receipt lost: %q", app.CopyNotice)
			}
			if app.Issues != nil && app.Issues.notice != notice {
				t.Fatal("old result changed another selection/overlay notice")
			}
			if navigation == "q" {
				if !strings.Contains(app.CopyNotice, "g") || !strings.Contains(app.CopyNotice, "s") {
					t.Fatal("board receipt lacks reopen instructions", app.CopyNotice)
				}
			}
			if navigation == "reopen" {
				if card, ok := app.issuesSelectedBound(); !ok || card.TaskID != "saved" {
					t.Fatal("reopened overlay lost saved binding")
				}
				app.HandleKey("s")
				if app.Takeover != nil {
					t.Fatal("retry opened unbound takeover")
				}
				runPendingWork(t, app)
				if starts != 2 {
					t.Fatalf("retry launches=%d", starts)
				}
			}
		})
	}
}

func TestIssuesRefinementBindingSurvivesOlderListAndIndex(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	app, _ := issuesImportApp(t, fake, nil, nil, func(context.Context, issue.Repository, int, issue.ImportOptions) (issue.ImportResult, error) {
		return issue.ImportResult{TaskID: "saved", State: "backlog"}, nil
	})
	app.HandleKey("g")
	runPendingWork(t, app)
	app.HandleKey("i")
	importWork := app.pendingWork
	app.pendingWork = nil
	app.HandleKey("q")
	app.HandleKey("g")
	listWork := app.pendingWork
	app.pendingWork = nil
	oldList := listWork()
	oldIndex := issuesIndexResult{seq: app.issuesIndexSeq, index: issue.Index{}}
	app.applyWork(importWork())
	app.applyWork(oldList)
	app.applyIssuesIndex(oldIndex)
	if card, ok := app.issuesSelectedBound(); !ok || card.TaskID != "saved" {
		t.Fatal("old scan erased committed binding")
	}
	if app.Issues.importing {
		t.Fatal("reopened busy hint did not settle")
	}
}
