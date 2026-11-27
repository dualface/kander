package tui

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
)

// tuiPolicyValues are policy values with the given branch mode and issue
// completion handoff.
func tuiPolicyValues(t *testing.T, mode, result string) json.RawMessage {
	t.Helper()
	values := config.PolicyValuesOf(config.DefaultConfig())
	values.Git.BranchMode, values.Issue.ResultOnDone = mode, result
	raw, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func writeTUIPolicy(t *testing.T, p *client.Project, mode string) api.ProjectPolicy {
	t.Helper()
	current, err := p.Policy(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	written, err := p.PutPolicy(t.Context(), client.NewKey(), api.PolicyWrite{ExpectedRevision: current.Revision, Values: tuiPolicyValues(t, mode, "auto")})
	if err != nil {
		t.Fatal(err)
	}
	return written
}

// runPolicyRequest runs the queued policy request and applies its answer.
func runPolicyRequest(t *testing.T, app *App) {
	t.Helper()
	cmd := app.takePolicy()
	if cmd == nil {
		t.Fatal("no policy request was scheduled")
	}
	if next := app.applyWork(eventMessage(t, cmd).(workMsg).payload); next != nil && app.Options != nil {
		pumpPanel(app.Options, next)
	}
}

func TestPolicyEventRefreshesThePolicyOfTheProcess(t *testing.T) {
	fixture := newEventFixture(t)
	app, session := fixture.app, fixture.session
	cmd := app.takeBoardEvents()
	written := writeTUIPolicy(t, session.Project.(*client.Project), "main-only")
	app.applyWork(eventMessage(t, cmd).(workMsg).payload)
	runPolicyRequest(t, app)
	if app.policy.known.Revision != written.Revision || session.PolicyRevision() != written.Revision {
		t.Fatalf("after the event the process knows revision %d, the session %d", app.policy.known.Revision, session.PolicyRevision())
	}
	cache := config.ReadPolicyCache(session.Home, session.ServerURL, session.Info.ID)
	if cache.Snapshot.Revision != written.Revision || cache.Values.Git.BranchMode != "main-only" {
		t.Fatalf("the policy cache holds %+v", cache)
	}
	if app.takePolicy() != nil {
		t.Fatal("a second read was scheduled without a new event")
	}
}

func TestPolicyEventAtTheKnownRevisionReadsNothing(t *testing.T) {
	fixture := newEventFixture(t)
	app, session := fixture.app, fixture.session
	project := session.Project.(*client.Project)
	first := writeTUIPolicy(t, project, "main-only")
	app.policy.known = config.PolicySnapshot{Epoch: first.Epoch, Revision: 5}
	cmd := app.takeBoardEvents()
	writeTUIPolicy(t, project, "main-develop")
	app.applyWork(eventMessage(t, cmd).(workMsg).payload)
	if app.policy.readQueued || app.takePolicy() != nil {
		t.Fatal("an event at or below the known revision asked for a read")
	}
}

func TestPolicyResetReadsThePolicyAgain(t *testing.T) {
	fixture := newEventFixture(t)
	app := fixture.app
	app.Model.Cursor = "previous-service-epoch:0"
	app.applyWork(eventMessage(t, app.takeBoardEvents()).(workMsg).payload)
	if !app.policy.readQueued {
		t.Fatal("a reset did not ask for a policy read")
	}
	runPolicyRequest(t, app)
	if app.policy.readQueued || app.policy.inFlight != 0 {
		t.Fatal("the read after the reset did not settle")
	}
}

func TestPolicyReadFailureKeepsTheOldValues(t *testing.T) {
	app := newPanelApp(t)
	app.policy.known = config.PolicySnapshot{Epoch: "e1", Revision: 2}
	app.policy.read = func(context.Context) (config.PolicySnapshot, error) {
		return config.PolicySnapshot{}, errors.New("policy endpoint unavailable")
	}
	app.notePolicyPage(api.EventPage{Events: []api.Event{{Kind: api.EventProjectPolicyChanged, Revision: 3}}})
	runPolicyRequest(t, app)
	if app.policy.known.Revision != 2 || !app.Running {
		t.Fatalf("a failed read changed the known revision to %d", app.policy.known.Revision)
	}
	if !strings.Contains(app.CopyNotice, "policy endpoint unavailable") {
		t.Fatalf("no notice for the failed read: %q", app.CopyNotice)
	}
	// The next reset tries again.
	app.notePolicyPage(api.EventPage{Reset: true})
	if app.takePolicy() == nil {
		t.Fatal("the next reset did not read again")
	}
}

// policyPanel opens Options on page with the server policy at revision 2
// (main-only) and fake policy requests.
func policyPanel(t *testing.T, page string) (*App, *optionsPanel, *[]policyWriteRequest) {
	t.Helper()
	app, panel := openPanel(t)
	app.policy.known = config.PolicySnapshot{Epoch: "e1", Revision: 2, Values: tuiPolicyValues(t, "main-only", "ask")}
	panel.session.AttachPolicy(app.policy.known)
	writes := &[]policyWriteRequest{}
	app.policy.read = func(context.Context) (config.PolicySnapshot, error) { return app.policy.known, nil }
	app.policy.write = func(_ context.Context, expected uint64, values json.RawMessage) (config.PolicySnapshot, error) {
		*writes = append(*writes, policyWriteRequest{expected: expected, values: values})
		return config.PolicySnapshot{Epoch: "e1", Revision: expected + 1, Values: values}, nil
	}
	pumpPanel(panel, panel.dispatch(page))
	return app, panel, writes
}

func TestOptionsShowTheServerPolicyWithItsRevision(t *testing.T) {
	app, panel, _ := policyPanel(t, sectionRules)
	if *panel.bind.gitBranchMode != "main-only" || *panel.bind.issueResult != "ask" {
		t.Fatalf("the rules page shows %s / %s, not the server policy", *panel.bind.gitBranchMode, *panel.bind.issueResult)
	}
	if view := ansi.Strip(app.View()); !strings.Contains(view, catalogText("tui.policy_source", "2")) {
		t.Fatalf("the rules page does not name the policy revision:\n%s", view)
	}
}

func TestOptionsRefreshShowsTheNewPolicy(t *testing.T) {
	app, panel, _ := policyPanel(t, sectionRules)
	next := config.PolicySnapshot{Epoch: "e1", Revision: 3, Values: tuiPolicyValues(t, "main-develop", "off")}
	app.policy.read = func(context.Context) (config.PolicySnapshot, error) { return next, nil }
	app.policy.readQueued = true
	runPolicyRequest(t, app)
	if *panel.bind.gitBranchMode != "main-develop" || *panel.bind.issueResult != "off" || panel.session.PolicyRevision() != 3 {
		t.Fatalf("after the refresh the page shows %s / %s at revision %d", *panel.bind.gitBranchMode, *panel.bind.issueResult, panel.session.PolicyRevision())
	}
	if view := ansi.Strip(app.View()); !strings.Contains(view, catalogText("tui.policy_source", "3")) {
		t.Fatalf("the page does not name revision 3:\n%s", view)
	}
}

func TestOptionsSavePolicyThroughTheServerWithTheLoadedRevision(t *testing.T) {
	app, panel, writes := policyPanel(t, sectionRules)
	*panel.bind.gitBranchMode = "main-develop"
	panel.bind.apply(panel)
	if !panel.session.PolicyDirty() {
		t.Fatal("the branch mode edit is not a policy edit")
	}
	// The server changes while the edit is open: the edit and its base stay.
	app.policy.read = func(context.Context) (config.PolicySnapshot, error) {
		return config.PolicySnapshot{Epoch: "e1", Revision: 3, Values: tuiPolicyValues(t, "main-only", "auto")}, nil
	}
	app.policy.readQueued = true
	runPolicyRequest(t, app)
	if *panel.bind.gitBranchMode != "main-develop" || !strings.Contains(panel.policyNotice, "3") {
		t.Fatalf("a refresh during an edit: branch %s, notice %q", *panel.bind.gitBranchMode, panel.policyNotice)
	}
	drivePanel(panel, keyMsg("enter"))
	runPolicyRequest(t, app)
	if len(*writes) != 1 || (*writes)[0].expected != 2 || !strings.Contains(string((*writes)[0].values), `"branch_mode":"main-develop"`) {
		t.Fatalf("policy writes %+v, want one with expected revision 2", *writes)
	}
	saved, err := config.LoadScope(true)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Git.BranchMode != config.DefaultGit().BranchMode || saved.Issue.ResultOnDone != config.DefaultConfig().Issue.ResultOnDone {
		t.Fatalf("the policy edit reached config.json: %+v", config.PolicyValuesOf(saved))
	}
	if panel.session.PolicyDirty() || panel.session.PolicyRevision() != 3 || panel.dirty {
		t.Fatalf("after the write: dirty %v, revision %d", panel.session.PolicyDirty(), panel.session.PolicyRevision())
	}
}

func TestOptionsPolicyConflictShowsTheServerValues(t *testing.T) {
	app, panel, _ := policyPanel(t, sectionRules)
	app.policy.write = func(context.Context, uint64, json.RawMessage) (config.PolicySnapshot, error) {
		return config.PolicySnapshot{}, &api.Error{Code: api.CodePolicyRevisionConflict, Status: 409, Message: "conflict"}
	}
	*panel.bind.gitBranchMode = "main-develop"
	panel.bind.apply(panel)
	drivePanel(panel, keyMsg("enter"))
	runPolicyRequest(t, app)
	if panel.policyNotice != catalogText("tui.policy_conflict") {
		t.Fatalf("conflict notice %q", panel.policyNotice)
	}
	server := config.PolicySnapshot{Epoch: "e1", Revision: 4, Values: tuiPolicyValues(t, "main-only", "off")}
	app.policy.read = func(context.Context) (config.PolicySnapshot, error) { return server, nil }
	runPolicyRequest(t, app)
	if panel.session.PolicyDirty() || panel.session.PolicyRevision() != 4 || panel.session.Config.Git.BranchMode != "main-only" || panel.session.Config.Issue.ResultOnDone != "off" {
		t.Fatalf("after the conflict the panel shows %+v at revision %d", config.PolicyValuesOf(panel.session.Config), panel.session.PolicyRevision())
	}
}

func TestOptionsWithoutAPolicyPointAtPolicyInit(t *testing.T) {
	app, panel := openPanel(t)
	app.policy.read = func(context.Context) (config.PolicySnapshot, error) { return config.PolicySnapshot{}, nil }
	panel.session.AttachPolicy(config.PolicySnapshot{})
	pumpPanel(panel, panel.dispatch(sectionRules))
	if view := ansi.Strip(app.View()); !strings.Contains(view, "kander policy init") {
		t.Fatalf("the rules page of a project without a policy does not point at kander policy init:\n%s", view)
	}
	*panel.bind.gitBranchMode = "main-only"
	panel.bind.apply(panel)
	drivePanel(panel, keyMsg("enter"))
	if app.policy.writeQueued != nil {
		t.Fatal("an edit without a policy was sent to the server")
	}
	if saved, _ := config.LoadScope(true); saved.Git.BranchMode != "main-only" {
		t.Fatalf("config.json holds %q", saved.Git.BranchMode)
	}
}

// catalogText renders a catalog message the way the panel does.
func catalogText(key string, args ...any) string { return t(key, args...) }
