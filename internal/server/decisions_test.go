package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/server"
)

func humanDecision() api.DecisionInput {
	return api.DecisionInput{
		Question: "Which target?", Options: []api.DecisionOption{{Key: "staging", Label: "Staging"}},
		Answer: "staging", Channel: "web", RecordedBy: "reported-account",
	}
}

func leaderDecision() api.DecisionInput {
	input := humanDecision()
	input.Channel = "pane"
	input.Authorization = &api.DecisionAuthorization{GrantedBy: "project-owner", Wording: "Choose the test target.\nUse the recommended option.", Date: "2026-11-25"}
	return input
}

func TestDecisionRoutesAreIndependentlyAuthorized(t *testing.T) {
	requests := make(chan server.Request, 4)
	f := newFixtureWithHooks(t, server.Hooks{Authorize: func(_ context.Context, req server.Request) error {
		if strings.Contains(req.Pattern, "/{task}/decision") {
			requests <- req
			return errors.New("private denial")
		}
		return nil
	}})
	p := f.project("host-a")
	task := todoTask(t, p, "auth-decisions")
	ctx := t.Context()
	id := task.TaskID
	_, err := p.RecordDecision(ctx, "", id, api.DecisionHuman, humanDecision())
	expectCode(t, err, api.CodeForbidden)
	_, err = p.RecordDecision(ctx, "", id, api.DecisionLeader, leaderDecision())
	expectCode(t, err, api.CodeForbidden)
	_, err = p.Decisions(ctx, id)
	expectCode(t, err, api.CodeForbidden)
	_, err = p.SetDecisionMode(ctx, "", id, api.SetDecisionMode{Mode: api.DecisionModeAssignee})
	expectCode(t, err, api.CodeForbidden)
	const path = "/api/v1/projects/{project}/tasks/{task}"
	for _, want := range []struct{ method, suffix string }{
		{http.MethodPost, "/decisions/human"}, {http.MethodPost, "/decisions/leader"},
		{http.MethodGet, "/decisions"}, {http.MethodPost, "/decision-mode"},
	} {
		select {
		case req := <-requests:
			if req.Method != want.method || req.Pattern != want.method+" "+path+want.suffix ||
				req.ProjectID != p.ID() || req.Project != "demo" || req.Task != id {
				t.Fatalf("wrong decision authorization scope: %+v", req)
			}
		default:
			t.Fatalf("missing authorization for %s", want.suffix)
		}
	}
	shown, err := p.Show(ctx, id)
	if err != nil || shown.Revision != task.Revision || shown.Text != task.Text {
		t.Fatalf("denied decisions changed the card: %+v, %v", shown, err)
	}
}

func TestDecisionAttribution(t *testing.T) {
	for _, tc := range []struct {
		name, reported, want, code string
		hook                       func(context.Context) (string, bool)
	}{
		{name: "no hook", reported: "Reported User", want: "Reported User"},
		{
			name: "host overrides client", reported: "invalid\nignored value", want: "Host User",
			hook: func(context.Context) (string, bool) { return "Host User", true },
		},
		{
			name: "host did not identify caller", reported: "Forged User", code: api.CodeAttributionRequired,
			hook: func(context.Context) (string, bool) { return "ignored", false },
		},
		{
			name: "blank host attribution", reported: "Forged User", code: api.CodeAttributionRequired,
			hook: func(context.Context) (string, bool) { return "", true },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			task := todoTask(t, f.project("host-a"), "attribution")
			f.stop()
			p := startWithHooks(t, f.dir, f.clock, server.Hooks{Attribution: tc.hook}).project("host-a")
			ctx := t.Context()
			input := humanDecision()
			input.RecordedBy = tc.reported
			recorded, err := p.RecordDecision(ctx, "", task.TaskID, api.DecisionHuman, input)
			if tc.code == "" {
				if err != nil || recorded.RecordedBy != tc.want {
					t.Fatalf("record attribution = %q, err = %v", recorded.RecordedBy, err)
				}
				return
			}
			expectCode(t, err, tc.code)
			list, err := p.Decisions(ctx, task.TaskID)
			if err != nil || len(list.Decisions) != 0 {
				t.Fatalf("rejected write created a decision: %+v, %v", list, err)
			}
			shown, err := p.Show(ctx, task.TaskID)
			if err != nil || shown.Revision != task.Revision || shown.Text != task.Text {
				t.Fatalf("rejected write changed task: %+v, %v", shown, err)
			}
		})
	}
}

func TestDecisionWriteRevisionEventReceiptAndManagedStorage(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	ctx := t.Context()
	task := todoTask(t, p, "decision-evidence")
	id := task.TaskID
	before, err := p.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	input := leaderDecision()
	record, err := p.RecordDecision(ctx, "decision-once", id, api.DecisionLeader, input)
	if err != nil || record.ID == "" || record.Schema != 1 || record.TaskID != id || record.Kind != api.DecisionLeader ||
		!record.CreatedAt.Equal(f.clock.now()) || record.CreatedAt.Location().String() != "UTC" || !reflect.DeepEqual(record.DecisionInput, input) {
		t.Fatalf("wrong decision: %+v, %v", record, err)
	}
	shown, err := p.Show(ctx, id)
	if err != nil || shown.Revision != task.Revision+1 {
		t.Fatalf("wrong revision: %d, %v", shown.Revision, err)
	}
	path := "decisions/" + record.ID + ".json"
	section, ok := board.SectionBody(shown.Text, board.SectionDecisions)
	var indexed struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	}
	if !ok || json.Unmarshal([]byte(section), &indexed) != nil || indexed.ID != record.ID || indexed.Path != path {
		t.Fatalf("wrong managed index: %s", section)
	}
	readDecisionDocument(t, p, record)
	for _, doc := range []api.Document{
		{Path: path, Text: "{}"},
		{Path: "DeCiSiOnS/other.json", Text: "{}"},
		{Path: "spec.md", Text: strings.Replace(shown.Text, section, "{}", 1)},
		{Path: "spec.md", Text: strings.Replace(shown.Text, "DECISION_MODE: leader", "DECISION_MODE: assignee", 1)},
	} {
		doc.ExpectedRevision, doc.ContractDecision = shown.Revision, "does not allow managed fields"
		_, err := p.PutDocument(ctx, "", id, doc, nil)
		var failure *api.Error
		if !errors.As(err, &failure) || failure.Code != api.CodeBoardRejected || failure.BoardCode != "board.transaction_managed" {
			t.Fatalf("write to %s got %v, want managed rejection", doc.Path, err)
		}
	}
	replayed, err := p.RecordDecision(ctx, "decision-once", id, api.DecisionLeader, input)
	if err != nil || !reflect.DeepEqual(record, replayed) {
		t.Fatalf("replay returned %+v, %v", replayed, err)
	}
	changedInput := input
	changedInput.Answer = "another option"
	_, err = p.RecordDecision(ctx, "decision-once", id, api.DecisionLeader, changedInput)
	expectCode(t, err, api.CodeIdempotencyMismatch)
	page, err := p.Events(ctx, before.Cursor, 0)
	if err != nil || page.Reset || len(page.Events) != 1 {
		t.Fatalf("want a single decision event: %+v, %v", page, err)
	}
	event := page.Events[0]
	if event.Kind != api.EventTaskChanged || event.TaskID != id || event.Revision != shown.Revision || event.State != "todo" {
		t.Fatalf("wrong decision event: %+v", event)
	}
	f.stop()
	p = start(t, f.dir, f.clock).project("host-a")
	replayed, err = p.RecordDecision(ctx, "decision-once", id, api.DecisionLeader, input)
	if err != nil || !reflect.DeepEqual(record, replayed) {
		t.Fatalf("durable replay returned %+v, %v", replayed, err)
	}
	if list, err := p.Decisions(ctx, id); err != nil || !reflect.DeepEqual(list.Decisions, []api.DecisionRecord{record}) {
		t.Fatalf("unexpected decision list after restart: %+v, %v", list, err)
	}
}

func readDecisionDocument(t *testing.T, p *client.Project, record api.DecisionRecord) {
	t.Helper()
	doc, err := p.Document(t.Context(), record.TaskID, "decisions/"+record.ID+".json")
	var stored api.DecisionRecord
	if err != nil || json.Unmarshal([]byte(doc.Text), &stored) != nil || !reflect.DeepEqual(record, stored) {
		t.Fatalf("stored decision differs: %s, %v", doc.Text, err)
	}
}

func TestLeaderDecisionRequiresCompleteAuthorization(t *testing.T) {
	p := newFixture(t).project("host-a")
	task := todoTask(t, p, "authorization-fields")
	for _, tc := range []struct {
		field  string
		change func(*api.DecisionInput)
	}{
		{"authorization", func(in *api.DecisionInput) { in.Authorization = nil }},
		{"granted_by", func(in *api.DecisionInput) { in.Authorization.GrantedBy = "" }},
		{"wording", func(in *api.DecisionInput) { in.Authorization.Wording = "" }},
		{"date", func(in *api.DecisionInput) { in.Authorization.Date = "" }},
		{"calendar date", func(in *api.DecisionInput) { in.Authorization.Date = "2026-02-30" }},
	} {
		t.Run(tc.field, func(t *testing.T) {
			input := leaderDecision()
			tc.change(&input)
			_, err := p.RecordDecision(t.Context(), "", task.TaskID, api.DecisionLeader, input)
			expectCode(t, err, api.CodeInvalidRequest)
		})
	}
	got, err := p.Show(t.Context(), task.TaskID)
	if err != nil || got.Revision != task.Revision {
		t.Fatalf("rejected writes changed revision: %+v, %v", got, err)
	}
}

func TestDecisionModeOnFrozenCardAndArchivedDecisionRetention(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	ctx := t.Context()
	task := todoTask(t, p, "assignee-decision")
	id := task.TaskID
	before, err := p.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	mode := api.SetDecisionMode{Mode: api.DecisionModeAssignee}
	changed, err := p.SetDecisionMode(ctx, "mode-once", id, mode)
	if err != nil || changed != (api.DecisionModeResult{TaskID: id, Mode: mode.Mode, Revision: task.Revision + 1}) {
		t.Fatalf("mode change = %+v, %v", changed, err)
	}
	replayed, err := p.SetDecisionMode(ctx, "mode-once", id, mode)
	if err != nil || replayed != changed {
		t.Fatalf("mode replay = %+v, %v", replayed, err)
	}
	page, err := p.Events(ctx, before.Cursor, 0)
	if err != nil || len(page.Events) != 1 || page.Events[0].Kind != api.EventTaskChanged || page.Events[0].Revision != changed.Revision {
		t.Fatalf("wrong mode event: %+v, %v", page, err)
	}
	access, err := f.srv.TaskAccess(ctx, p.ID(), id)
	if err != nil || access.DecisionMode != mode.Mode || access.Revision != changed.Revision {
		t.Fatalf("access = %+v, %v", access, err)
	}
	_, err = p.RecordDecision(ctx, "", id, api.DecisionLeader, leaderDecision())
	expectCode(t, err, api.CodeDecisionReserved)
	human, err := p.RecordDecision(ctx, "", id, api.DecisionHuman, humanDecision())
	if err != nil || human.Kind != api.DecisionHuman || human.Authorization != nil {
		t.Fatalf("human decision was not accepted: %+v, %v", human, err)
	}
	archived, err := p.Move(ctx, "", id, api.Move{State: "archived", Result: "cancelled", Reason: "test", Decision: "fixture"}, nil)
	if err != nil || archived.State != "archived" {
		t.Fatalf("archive: %+v, %v", archived, err)
	}
	// Re-run cleanup explicitly so this also covers subsequent archive sweeps.
	root := filepath.Join(f.dir, "projects", api.ProjectDirName("demo"), "board")
	if report, err := board.PruneArchived(root); err != nil || report.Err != nil {
		t.Fatalf("archive cleanup: %+v, %v", report, err)
	}
	readDecisionDocument(t, p, human)
	list, err := p.Decisions(ctx, id)
	if err != nil || !reflect.DeepEqual(list.Decisions, []api.DecisionRecord{human}) {
		t.Fatalf("archive removed the decision: %+v, %v", list, err)
	}
	_, err = p.RecordDecision(ctx, "", id, api.DecisionHuman, humanDecision())
	expectCode(t, err, api.CodeBoardRejected)
	_, err = p.SetDecisionMode(ctx, "", id, api.SetDecisionMode{Mode: api.DecisionModeLeader})
	expectCode(t, err, api.CodeBoardRejected)

	trash := todoTask(t, p, "trashed-decision")
	if _, err := p.Move(ctx, "", trash.TaskID, api.Move{State: "trash", Result: "trashed", Reason: "test", Decision: "fixture"}, nil); err != nil {
		t.Fatal(err)
	}
	_, err = p.RecordDecision(ctx, "", trash.TaskID, api.DecisionHuman, humanDecision())
	expectCode(t, err, api.CodeBoardRejected)
	_, err = p.SetDecisionMode(ctx, "", trash.TaskID, mode)
	expectCode(t, err, api.CodeBoardRejected)
}
