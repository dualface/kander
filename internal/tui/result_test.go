package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/issue"
)

func TestIssueResultDoneDispatchAndConfirmation(t *testing.T) {
	app, _, triageCalls := takeoverListApp(t, func(context.Context, issue.Repository, int, issue.TriageOptions) (issue.TriageOutcome, error) {
		return issue.TriageOutcome{}, errors.New("wrong path")
	})
	repository := *app.issuesRepository()
	key, _ := repository.IssueSourceKey(42)
	app.Issues.index = issue.Index{key: {TaskID: "completed", State: "done"}}
	calls := 0
	app.ResultIssue = func(_ context.Context, got issue.Repository, n int, options issue.TriageOptions) (issue.TriageOutcome, error) {
		calls++
		if got != repository || n != 42 || options.CardID != "completed" || options.Agent != "claude" {
			t.Fatalf("wrong target/options: %+v", options)
		}
		return issue.TriageOutcome{}, errors.New("simulated launch failure")
	}
	if app.issuesActionHint() != config.Text("tui.issues_hint_done") {
		t.Fatal("done hint missing")
	}
	for _, key := range []string{"i", "I"} {
		app.HandleKey(key)
		if app.pendingWork != nil {
			t.Fatal("hidden import ran")
		}
	}
	app.HandleKey("s")
	runPendingWork(t, app)
	if app.Takeover == nil || app.Takeover.cardID != "completed" {
		t.Fatal("missing result dialog")
	}
	_, body := app.renderTakeover()
	if !strings.Contains(body, "confirmation") && !strings.Contains(body, "确认") {
		t.Fatalf("missing separate consent: %s", body)
	}
	app.HandleKey("y")
	app.HandleKey("s")
	runPendingWork(t, app)
	if calls != 1 || len(*triageCalls) != 0 || !app.Takeover.failed {
		t.Fatalf("calls=%d dialog=%+v", calls, app.Takeover)
	}
}

func TestIssueResultChangedTargetNeverLaunches(t *testing.T) {
	for _, change := range []string{"state", "binding", "selection", "repository"} {
		t.Run(change, func(t *testing.T) {
			app, _, _ := takeoverListApp(t, func(context.Context, issue.Repository, int, issue.TriageOptions) (issue.TriageOutcome, error) {
				t.Fatal("triage called")
				return issue.TriageOutcome{}, nil
			})
			key, _ := app.issuesRepository().IssueSourceKey(42)
			app.Issues.index = issue.Index{key: {TaskID: "completed", State: "done"}}
			app.ResultIssue = func(context.Context, issue.Repository, int, issue.TriageOptions) (issue.TriageOutcome, error) {
				t.Fatal("stale result launched")
				return issue.TriageOutcome{}, nil
			}
			app.HandleKey("s")
			runPendingWork(t, app)
			switch change {
			case "state":
				app.Issues.index[key] = issue.LocalCard{TaskID: "completed", State: "archived"}
			case "binding":
				app.Issues.index[key] = issue.LocalCard{TaskID: "other", State: "done"}
			case "selection":
				app.Takeover.number = 99
			case "repository":
				app.Takeover.repository.Name = "other"
			}
			app.HandleKey("y")
			if app.Takeover != nil || app.pendingWork != nil {
				t.Fatal("stale dialog retained/launched")
			}
		})
	}
}
