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
	"time"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/server"
)

func TestCardCreationSelectsSubmitter(t *testing.T) {
	now := time.Now()
	snapshot, err := issue.BuildImportSnapshot(issue.IssueSnapshot{
		Repository: issue.Repository{Host: "github.com", Owner: "test", Name: "repo", URL: "https://github.com/test/repo"},
		Number:     42, Title: "Import demo", State: "open", CreatedAt: now, UpdatedAt: now, FetchedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := issue.MarshalImportSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, reported, want, code string
		hook                       func(context.Context) (string, bool)
	}{
		{name: "no hook", reported: "  Reported User ", want: "Reported User"},
		{name: "blank without hook"},
		{
			name: "host overrides invalid client value", reported: "client\nforgery", want: "Host User",
			hook: func(context.Context) (string, bool) { return "Host User", true },
		},
		{
			name: "host did not identify caller", reported: "Forged User", code: api.CodeAttributionRequired,
			hook: func(context.Context) (string, bool) { return "ignored", false },
		},
		{name: "invalid reported value", reported: "client\nforgery", code: api.CodeInvalidRequest},
		{
			name: "invalid attributed value", reported: "Reported User", code: api.CodeInvalidRequest,
			hook: func(context.Context) (string, bool) { return "bad\x7fvalue", true },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newFixtureWithHooks(t, server.Hooks{Attribution: tc.hook}).project("host-a")
			ctx := t.Context()
			created, createErr := p.NewTask(ctx, "", api.NewTask{
				Type: "chore", Slug: "attributed-new", Title: "New card", Language: "en", Submitter: tc.reported,
			})
			raw, importErr := p.ImportIssue(ctx, "", api.IssueImport{Snapshot: encoded, Language: "en", Submitter: tc.reported})
			list, err := p.List(ctx, "")
			if err != nil {
				t.Fatal(err)
			}
			if tc.code != "" {
				expectCode(t, createErr, tc.code)
				expectCode(t, importErr, tc.code)
				if len(list.Tasks) != 0 {
					t.Fatalf("rejected request created a card: %+v", list.Tasks)
				}
				return
			}
			if createErr != nil || importErr != nil {
				t.Fatalf("new error = %v, import error = %v", createErr, importErr)
			}
			var imported issue.ImportResult
			if err := json.Unmarshal(raw, &imported); err != nil {
				t.Fatal(err)
			}
			card, err := p.Show(ctx, imported.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			line := "\n" + board.RenderField(board.FieldSubmitter, tc.want) + "\n- ASSIGNEE:\n- OWNER:"
			for _, task := range []api.Task{created, card} {
				if !strings.Contains(board.CardHeader(task.Text), line) {
					t.Errorf("%s does not have initial person fields:\n%s", task.TaskID, board.CardHeader(task.Text))
				}
			}
			if len(list.Tasks) != 2 {
				t.Fatalf("got %d cards, want 2", len(list.Tasks))
			}
			for _, item := range list.Tasks {
				if item.Submitter != tc.want || item.Assignee != "" || item.Owner != "" {
					t.Errorf("incorrect summary for %s: %+v", item.TaskID, item)
				}
			}
		})
	}
}

func TestAssignmentCASAndReplayOnClaimedCard(t *testing.T) {
	f := newFixture(t)
	executor, manager := f.project("host-a"), f.project("host-b")
	card := todoTask(t, executor, "assign-claimed")
	ctx := t.Context()
	grant, err := claim(t, executor, card.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	list, err := manager.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	id, revision := card.TaskID, grant.Task.Revision
	req := api.Assign{Assignee: "  Alice  ", ExpectedRevision: &revision}
	assigned, err := manager.Assign(ctx, "assign-once", id, req)
	wantText := strings.Replace(grant.Task.Text, "\n- ASSIGNEE:\n", "\n- ASSIGNEE: Alice\n", 1)
	if err != nil || assigned.Revision != revision+1 || assigned.Text != wantText || !reflect.DeepEqual(assigned.Claim, grant.Task.Claim) {
		t.Fatalf("assignment did not change only its field and revision: %+v, %v", assigned, err)
	}
	page, err := manager.Events(ctx, list.Cursor, 0)
	if err != nil || page.Reset || len(page.Events) != 1 {
		t.Fatalf("want one assignment event: %+v, %v", page, err)
	}
	event := page.Events[0]
	if event.Kind != api.EventTaskChanged || event.TaskID != id || event.Revision != assigned.Revision || event.State != "working" || event.Generation != grant.Claim.Generation {
		t.Fatalf("incorrect assignment event: %+v", event)
	}
	list, err = manager.List(ctx, "")
	if err != nil || len(list.Tasks) != 1 || list.Tasks[0].Owner != "claude" || list.Tasks[0].Assignee != "Alice" {
		t.Fatalf("executing agent and person were not distinct: %+v, %v", list, err)
	}
	_, err = manager.Assign(ctx, "", id, api.Assign{ExpectedRevision: &revision})
	expectCode(t, err, api.CodeRevisionConflict)
	_, err = manager.Assign(ctx, "assign-once", id, api.Assign{Assignee: "Bob", ExpectedRevision: &revision})
	expectCode(t, err, api.CodeIdempotencyMismatch)
	_, err = manager.Assign(ctx, "", id, api.Assign{Assignee: strings.Repeat("界", 86)})
	expectCode(t, err, api.CodeInvalidRequest)
	cleared, err := manager.Assign(ctx, "", id, api.Assign{})
	if err != nil || cleared.Revision != assigned.Revision+1 || cleared.Text != grant.Task.Text {
		t.Fatalf("assignment not cleared: %+v, %v", cleared, err)
	}
	list, err = manager.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := manager.Assign(ctx, "assign-once", id, req)
	if err != nil || !reflect.DeepEqual(assigned, replay) {
		t.Fatalf("replay did not return original revision and text: %+v, %v", replay, err)
	}
	shown, err := manager.Show(ctx, id)
	if err != nil || shown.Revision != cleared.Revision || shown.Text != cleared.Text {
		t.Fatalf("replay changed the card: %+v, %v", shown, err)
	}
	page, err = manager.Events(ctx, list.Cursor, 0)
	if err != nil || page.Reset || len(page.Events) != 0 {
		t.Fatalf("replay emitted an event: %+v, %v", page, err)
	}
}

func TestAssignmentAuthorization(t *testing.T) {
	requests := make(chan server.Request, 1)
	f := newFixtureWithHooks(t, server.Hooks{Authorize: func(_ context.Context, req server.Request) error {
		if strings.HasSuffix(req.Pattern, "/{task}/assignee") && !strings.HasSuffix(req.Task, ".md") {
			requests <- req
			return errors.New("private policy")
		}
		return nil
	}})
	p := f.project("host-a")
	card := todoTask(t, p, "assign-denied")
	_, err := p.Assign(t.Context(), "", card.TaskID, api.Assign{Assignee: "Alice"})
	expectCode(t, err, api.CodeForbidden)
	select {
	case req := <-requests:
		if req.Method != http.MethodPost || req.ProjectID != p.ID() || req.Project != "demo" || req.Task != card.TaskID {
			t.Fatalf("wrong authorization scope: %+v", req)
		}
	default:
		t.Fatal("assignment did not call authorization hook")
	}
	_, err = p.Assign(t.Context(), "", card.TaskID+".md", api.Assign{Assignee: "Alice"})
	expectCode(t, err, api.CodeInvalidRequest)
	shown, err := p.Show(t.Context(), card.TaskID)
	if err != nil || shown.Revision != card.Revision || shown.Text != card.Text {
		t.Fatalf("denied assignment changed the card: %+v, %v", shown, err)
	}
}

func TestAssignmentRejectsFinalStates(t *testing.T) {
	p := newFixture(t).project("host-a")
	for _, tc := range []struct{ state, result string }{{"archived", "cancelled"}, {"trash", "trashed"}} {
		t.Run(tc.state, func(t *testing.T) {
			card := todoTask(t, p, "assign-"+tc.state)
			moved, err := p.Move(t.Context(), "", card.TaskID, api.Move{
				State: tc.state, Result: tc.result, Reason: "test decision", Decision: "test",
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = p.Assign(t.Context(), "", card.TaskID, api.Assign{Assignee: "Alice"})
			expectCode(t, err, api.CodeBoardRejected)
			shown, err := p.Show(t.Context(), card.TaskID)
			if err != nil || shown.Revision != moved.Revision || shown.Text != moved.Text {
				t.Fatalf("rejected assignment changed the card: %+v, %v", shown, err)
			}
		})
	}
}

func TestOrdinaryDocumentWriteCannotChangePeople(t *testing.T) {
	p := newFixture(t).project("host-a")
	ctx := t.Context()
	card := todoTask(t, p, "protected-people")
	grant, err := claim(t, p, card.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	card = grant.Task
	proof := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	for _, field := range []string{board.FieldSubmitter, board.FieldAssignee} {
		t.Run(field, func(t *testing.T) {
			text := strings.Replace(card.Text, "\n- "+field+":\n", "\n- "+field+": Alice\n", 1)
			_, err := p.PutDocument(ctx, "", card.TaskID, api.Document{
				Path: "spec.md", Text: text, ExpectedRevision: card.Revision, ContractDecision: "does not allow managed fields",
			}, &proof)
			var failure *api.Error
			if !errors.As(err, &failure) || failure.Code != api.CodeBoardRejected || failure.BoardCode != "board.transaction_managed" {
				t.Fatalf("managed field write error = %v", err)
			}
		})
	}
	shown, err := p.Show(ctx, card.TaskID)
	if err != nil || shown.Revision != card.Revision || shown.Text != card.Text {
		t.Fatalf("rejected update changed the card: %+v, %v", shown, err)
	}
}

func TestOldCardWithoutPeopleFieldsStillWorks(t *testing.T) {
	f := newFixture(t)
	f.project("host-a")
	f.stop()
	// Seed the historical header offline. Startup takes it into the index.
	root := filepath.Join(f.dir, "projects", api.ProjectDirName("demo"), "board")
	path, err := board.NewTask(root, "chore", "old-people", "Historical card", "en", false)
	if err != nil {
		t.Fatal(err)
	}
	id := filepath.Base(path)
	err = board.WithTransaction(root, board.LockScope{Tasks: []string{id}}, func(tx *board.Transaction) error {
		card, err := tx.Snapshot(id)
		if err != nil {
			return err
		}
		return tx.Put(id, "spec.md", strings.Replace(card.Text, "- SUBMITTER:\n- ASSIGNEE:\n", "", 1))
	})
	if err != nil {
		t.Fatal(err)
	}
	f = start(t, f.dir, f.clock)
	p := f.project("host-a")
	ctx := t.Context()
	shown, err := p.Show(ctx, id)
	if err != nil || strings.Contains(shown.Text, "SUBMITTER:") || strings.Contains(shown.Text, "ASSIGNEE:") {
		t.Fatalf("show rewrote the historical header: %+v, %v", shown, err)
	}
	list, err := p.List(ctx, "")
	if err != nil || len(list.Tasks) != 1 || list.Tasks[0].Submitter != "" || list.Tasks[0].Assignee != "" {
		t.Fatalf("historical row did not read as empty: %+v, %v", list, err)
	}
	text := strings.ReplaceAll(shown.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
	_, err = p.PutDocument(ctx, "", id, api.Document{Path: "spec.md", Text: text, ExpectedRevision: shown.Revision}, nil)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := p.Move(ctx, "", id, api.Move{State: "todo"}, nil)
	if err != nil || moved.State != "todo" || moved.Text != text {
		t.Fatalf("historical card could not move unchanged: %+v, %v", moved, err)
	}
}
