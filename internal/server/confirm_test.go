package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/server"
	publicserver "github.com/dualface/kander/v2/server"
)

const confirmPattern = "POST /api/v1/projects/{project}/tasks/{task}/confirm"

func TestConfirmRouteInventory(t *testing.T) {
	routes := publicserver.Routes()
	if len(routes) != 81 {
		t.Fatalf("routes = %d, want 81 (79 at the delivery base plus confirm and server gc)", len(routes))
	}
	for _, route := range routes {
		if route.Pattern == confirmPattern && route.Authorized {
			return
		}
	}
	t.Fatalf("missing authorized route %q", confirmPattern)
}

func confirmCard(t *testing.T, p *client.Project, slug, description, discussion string) api.Task {
	t.Helper()
	card, err := p.NewTask(t.Context(), "", api.NewTask{
		Type: "feature", Slug: slug, Title: "Confirmation fixture", Language: "en", Description: description,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(card.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\n"+discussion+"\n", 1)
	card, err = p.PutDocument(t.Context(), "", card.TaskID, api.Document{
		Path: "spec.md", Text: text, ExpectedRevision: card.Revision,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return card
}

func postConfirmation(t *testing.T, f *fixture, p *client.Project, id, key, body string, status int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/projects/"+p.ID()+"/tasks/"+id+"/confirm", strings.NewReader(body))
	r.Header.Set(api.HeaderClientID, "host-a")
	r.Header.Set(api.HeaderProtocol, strconv.Itoa(api.ProtocolVersion))
	r.Header.Set(api.HeaderIdempotencyKey, key)
	w := httptest.NewRecorder()
	f.srv.IndexDifferentialHandler(t).ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("confirmation returned HTTP %d, want %d: %.800s", w.Code, status, w.Body.String())
	}
	return w
}

func confirmationLines(text string) []string {
	body, _ := board.SectionBody(text, board.SectionConfirmations)
	return strings.FieldsFunc(body, func(r rune) bool { return r == '\n' })
}

func assertUnchangedCard(t *testing.T, p *client.Project, before api.Task) {
	t.Helper()
	after, err := p.Show(t.Context(), before.TaskID)
	if err != nil || after.State != before.State || after.Revision != before.Revision || after.Text != before.Text {
		t.Fatalf("card changed after rejection: state = %s, revision = %d, error = %v", after.State, after.Revision, err)
	}
}

const confirmReviews = "SELF_REVIEW: PASS\nCARD_REVIEW: PASS"

func TestConfirmCommitsRecordReceiptIndexAndOneEvent(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	ctx := t.Context()
	card := confirmCard(t, p, "confirm", "Original request", "  SUBMITTER_QUESTIONS: indented, not a marker\n- SUBMITTER_QUESTIONS: \t\n"+confirmReviews)
	if board.MetadataFrom(card.Text, board.FieldSize) != "small" || board.TaskGroupFrom(card.Text) != "" {
		t.Fatal("fixture is not a small standalone card")
	}
	before, err := p.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	const key = "confirm-receipt"
	body := fmt.Sprintf(`{"expected_revision":%d,"confirmed_by":" Alice "}`, card.Revision)
	w := postConfirmation(t, f, p, card.TaskID, key, body, http.StatusOK)
	var done api.Task
	if err := json.Unmarshal(w.Body.Bytes(), &done); err != nil {
		t.Fatal(err)
	}
	if done.TaskID != card.TaskID || done.State != "todo" || done.Revision != card.Revision+1 {
		t.Fatalf("confirmation gave %q at state %q and revision %d", done.TaskID, done.State, done.Revision)
	}
	lines := confirmationLines(done.Text)
	var record struct {
		At       string `json:"at"`
		By       string `json:"by"`
		Revision uint64 `json:"revision"`
	}
	if len(lines) != 1 || json.Unmarshal([]byte(lines[0]), &record) != nil || record.By != "Alice" || record.Revision != card.Revision || record.At != f.clock.now().UTC().Format(time.RFC3339) {
		t.Fatalf("unexpected confirmation record: %q", lines)
	}
	list, err := p.List(ctx, "todo")
	if err != nil || len(list.Tasks) != 1 || list.Tasks[0].TaskID != card.TaskID || list.Tasks[0].Revision != done.Revision {
		t.Fatalf("todo list after confirmation: %+v, %v", list, err)
	}

	f.clock.advance(time.Second)
	replay := postConfirmation(t, f, p, card.TaskID, key, body, http.StatusOK)
	if replay.Body.String() != w.Body.String() {
		t.Fatal("replay did not return the exact response")
	}
	mismatch := postConfirmation(t, f, p, card.TaskID, key, fmt.Sprintf(`{"expected_revision":%d,"confirmed_by":" Alice "}`, done.Revision), http.StatusUnprocessableEntity)
	assertRequestRecordError(t, mismatch.Body.Bytes(), api.CodeIdempotencyMismatch, "")
	assertUnchangedCard(t, p, done)
	page, err := p.Events(ctx, before.Cursor, 0)
	if err != nil || page.Reset || len(page.Events) != 1 || page.Events[0].Kind != api.EventTaskChanged ||
		page.Events[0].TaskID != done.TaskID || page.Events[0].Revision != done.Revision || page.Events[0].State != "todo" {
		t.Fatalf("expected exactly one task_changed, got %+v, %v", page, err)
	}
}

func TestConfirmChecksTheCardAndExpectedRevision(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	for _, tc := range []struct {
		slug, description, discussion, code, boardCode string
		status                                         int
	}{
		{"stale", "request", confirmReviews, api.CodeRevisionConflict, "board.transaction_conflict", http.StatusConflict},
		{"ordinary", "", confirmReviews, api.CodeBoardRejected, "board.confirm_requires_request_card", http.StatusUnprocessableEntity},
		{"not-backlog", "request", confirmReviews, api.CodeBoardRejected, "board.confirm_requires_backlog", http.StatusUnprocessableEntity},
		{"questions", "request", "- SUBMITTER_QUESTIONS \t: \tWhat is the limit?\n" + confirmReviews, api.CodeBoardRejected, "board.request_questions_open", http.StatusUnprocessableEntity},
		{"no-self-review", "request", "CARD_REVIEW: PASS", api.CodeBoardRejected, "board.task_requires_self_review_record", http.StatusUnprocessableEntity},
		{"no-card-review", "request", "SELF_REVIEW: PASS", api.CodeBoardRejected, "board.task_requires_card_review_record", http.StatusUnprocessableEntity},
		{"bad-actor", "request", confirmReviews, api.CodeInvalidRequest, "board.person_field_invalid", http.StatusBadRequest},
	} {
		t.Run(tc.slug, func(t *testing.T) {
			card := confirmCard(t, p, tc.slug, tc.description, tc.discussion)
			if tc.slug == "not-backlog" {
				var err error
				card, err = p.Move(t.Context(), "", card.TaskID, api.Move{State: "archived", Result: "cancelled", Reason: "Fixture", Decision: "fixture"}, nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			rev, actor := card.Revision, "Alice"
			if tc.slug == "stale" {
				rev--
			}
			if tc.slug == "bad-actor" {
				actor += "\nOther name"
			}
			body := fmt.Sprintf(`{"expected_revision":%d,"confirmed_by":%q}`, rev, actor)
			w := postConfirmation(t, f, p, card.TaskID, client.NewKey(), body, tc.status)
			assertRequestRecordError(t, w.Body.Bytes(), tc.code, tc.boardCode)
			assertUnchangedCard(t, p, card)
		})
	}
}

func TestConfirmRequiresRevisionAndClientIdentityWithoutHook(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	card := confirmCard(t, p, "missing-input", "request", confirmReviews)
	for _, raw := range []string{
		`{"confirmed_by":"Alice"}`, `{"expected_revision":0,"confirmed_by":"Alice"}`,
		fmt.Sprintf(`{"expected_revision":%d}`, card.Revision),
	} {
		w := postConfirmation(t, f, p, card.TaskID, client.NewKey(), raw, http.StatusBadRequest)
		assertRequestRecordError(t, w.Body.Bytes(), api.CodeInvalidRequest, "")
	}
	assertUnchangedCard(t, p, card)
}

func TestConfirmAuthorizationAndAttribution(t *testing.T) {
	var allowed atomic.Bool
	var actor atomic.Value
	actor.Store("Alice from host")
	var scoped atomic.Pointer[server.Request]
	f := newFixtureWithHooks(t, server.Hooks{
		Authorize: func(_ context.Context, req server.Request) error {
			if req.Pattern == confirmPattern {
				scoped.Store(&req)
				if !allowed.Load() {
					return errors.New("private host policy")
				}
			}
			return nil
		},
		Attribution: func(context.Context) (string, bool) {
			value := actor.Load().(string)
			return value, value != "absent"
		},
	})
	p := f.project("host-a")
	card := confirmCard(t, p, "hooked", "request", confirmReviews)
	const key = "confirmation-host-policy"
	claim := api.Confirm{ExpectedRevision: card.Revision, ConfirmedBy: "Client name"}
	_, err := p.Confirm(t.Context(), key, card.TaskID, claim)
	expectCode(t, err, api.CodeForbidden)
	req := scoped.Load()
	if req == nil || req.Task != card.TaskID || req.Pattern != confirmPattern {
		t.Fatalf("unexpected route scope: %+v", req)
	}
	assertUnchangedCard(t, p, card)
	allowed.Store(true)
	claim.ConfirmedBy = "Changed client name"
	for _, value := range []string{"absent", "", " \t"} {
		actor.Store(value)
		_, err := p.Confirm(t.Context(), key, card.TaskID, claim)
		expectCode(t, err, api.CodeAttributionRequired)
		assertUnchangedCard(t, p, card)
	}
	actor.Store("Alice from host")
	// The same key with different input succeeds because denial never reserved it.
	confirmed, err := p.Confirm(t.Context(), key, card.TaskID, claim)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := board.LastConfirmer(confirmed.Text); !ok || got != "Alice from host" {
		t.Fatalf("stored actor = %q, want the host actor", got)
	}
	actor.Store("Different host actor")
	_, err = p.Confirm(t.Context(), key, card.TaskID, claim)
	expectCode(t, err, api.CodeIdempotencyMismatch)
}

func TestRequestCardNeedsFreshConfirmationAndProtectsTheHistory(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	ctx := t.Context()
	card := confirmCard(t, p, "confirm-again", "request", confirmReviews)
	rejectSpec := func(before api.Task, text string) {
		t.Helper()
		_, err := p.PutDocument(ctx, "", before.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: before.Revision}, nil)
		var failure *api.Error
		if !errors.As(err, &failure) || failure.Status != http.StatusUnprocessableEntity || failure.Code != api.CodeBoardRejected || failure.BoardCode != "board.transaction_managed" {
			t.Fatalf("tampering returned %v", err)
		}
		assertUnchangedCard(t, p, before)
	}
	rejectMove := func(before api.Task) {
		t.Helper()
		_, err := p.Move(ctx, "", before.TaskID, api.Move{State: "todo"}, nil)
		var failure *api.Error
		if !errors.As(err, &failure) || failure.Status != http.StatusConflict || failure.Code != api.CodeConfirmationRequired {
			t.Fatalf("move returned %v", err)
		}
		assertUnchangedCard(t, p, before)
	}
	rejectMove(card)
	rejectSpec(card, card.Text+"\n## CONFIRMATIONS\n\n{\"by\":\"Fabricated\"}\n")
	first, err := p.Confirm(ctx, "", card.TaskID, api.Confirm{ExpectedRevision: card.Revision, ConfirmedBy: "Alice"})
	if err != nil {
		t.Fatal(err)
	}
	backlog, err := p.Move(ctx, "", first.TaskID, api.Move{State: "backlog"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rejectMove(backlog)
	withoutRecord, _, _ := strings.Cut(backlog.Text, "## CONFIRMATIONS\n")
	rejectSpec(backlog, withoutRecord)
	rejectSpec(backlog, strings.Replace(backlog.Text, `"by":"Alice"`, `"by":"Someone else"`, 1))
	second, err := p.Confirm(ctx, "", backlog.TaskID, api.Confirm{ExpectedRevision: backlog.Revision, ConfirmedBy: "Alice again"})
	if err != nil {
		t.Fatal(err)
	}
	firstLines, newLines := confirmationLines(first.Text), confirmationLines(second.Text)
	if len(firstLines) != 1 || len(newLines) != 2 || firstLines[0] != newLines[0] {
		t.Fatalf("confirmation history = %q, before %q", newLines, firstLines)
	}
	grant, err := claim(t, p, second.TaskID, "host-a")
	if err != nil || grant.Task.State != "working" {
		t.Fatalf("claim after confirmation: state = %q, err = %v", grant.Task.State, err)
	}
}

func TestRequestCardCanBeTrashedAndOrdinaryCardCanMoveToTodo(t *testing.T) {
	p := newFixture(t).project("host-a")
	for _, tc := range []struct {
		description string
		move        api.Move
	}{
		{"Original request", api.Move{State: "trash", Result: "trashed", Reason: "Fixture", Decision: "fixture"}},
		{"", api.Move{State: "todo"}},
	} {
		card := confirmCard(t, p, tc.move.State, tc.description, "SELF_REVIEW: PASS")
		after, err := p.Move(t.Context(), "", card.TaskID, tc.move, nil)
		if err != nil || after.State != tc.move.State {
			t.Fatalf("move to %s: state = %q, err = %v", tc.move.State, after.State, err)
		}
	}
}
