package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/v2/api"
)

const labelClientID = "c-42f1216e5b1c-center5900"

func labelTestApp() *App {
	app := refreshTestApp(nil, nil)
	app.Context.Unassigned = "unassigned"
	return app
}

func claimedTask(owner, status, client string) Task {
	return Task{TaskID: "20261127-label-task", Title: "label", State: "working", Owner: owner, ClaimStatus: status, ClaimClientID: client}
}

func TestExecutionLabel(t *testing.T) {
	app := labelTestApp()
	for _, tc := range []struct {
		name string
		task Task
		want string
	}{
		{"active", claimedTask("claude", api.ClaimActive, labelClientID), "claude@center5900"},
		{"lapsed", claimedTask("codex", api.ClaimLapsed, labelClientID), "codex@center5900"},
		{"lost", claimedTask("claude", api.ClaimLost, labelClientID), "claude@center5900"},
		{"no host in id", claimedTask("claude", api.ClaimActive, "c-42f1216e5b1c"), "claude@c-42f1216e5b1c"},
		{"foreign id", claimedTask("claude", api.ClaimActive, "worker-7"), "claude@worker-7"},
		{"empty owner", claimedTask("", api.ClaimActive, labelClientID), "unassigned@center5900"},
		{"released", claimedTask("claude", api.ClaimReleased, labelClientID), "claude"},
		{"no claim", claimedTask("claude", "", ""), "claude"},
		{"empty client id", claimedTask("claude", api.ClaimActive, ""), "claude"},
		{"no claim no owner", claimedTask("", "", ""), "unassigned"},
	} {
		if got := app.executionLabel(tc.task); got != tc.want {
			t.Errorf("%s: executionLabel = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestCardMetaLineShowsExecutionHost(t *testing.T) {
	app := labelTestApp()
	claimed := claimedTask("claude", api.ClaimActive, labelClientID)
	if line := app.cardMetaLine(claimed); !strings.Contains(line, " claude@center5900 ") {
		t.Fatalf("card meta line %q has no execution host", line)
	}
	plain := claimedTask("claude", "", "")
	if line := app.cardMetaLine(plain); !strings.Contains(line, " claude ") || strings.Contains(line, "@") {
		t.Fatalf("card meta line without claim changed: %q", line)
	}
}

func TestDetailMetaShowsExecutionHost(t *testing.T) {
	app := labelTestApp()
	task := claimedTask("claude", api.ClaimActive, labelClientID)
	app.Detail = &task
	if view := ansi.Strip(app.renderDetailView()); !strings.Contains(view, "claude@center5900") {
		t.Fatalf("detail view has no execution host:\n%s", view)
	}
	released := claimedTask("claude", api.ClaimReleased, labelClientID)
	app.Detail = &released
	if view := ansi.Strip(app.renderDetailView()); strings.Contains(view, "@center5900") {
		t.Fatalf("detail view shows the host of a released claim:\n%s", view)
	}
}
