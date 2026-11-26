package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/server"
)

func requestRecordPost(t *testing.T, f *fixture, project, key, body string, status int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/projects/"+project+"/tasks", strings.NewReader(body))
	req.Header.Set(api.HeaderClientID, "host-a")
	req.Header.Set(api.HeaderProtocol, strconv.Itoa(api.ProtocolVersion))
	req.Header.Set(api.HeaderIdempotencyKey, key)
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, req)
	if w.Code != status {
		t.Fatalf("create status = %d, want %d: %.800s", w.Code, status, w.Body.String())
	}
	return w
}

func TestRequestRecordCreationReceiptEventAndProjection(t *testing.T) {
	f := newFixtureWithHooks(t, server.Hooks{Attribution: func(context.Context) (string, bool) { return "Alice", true }})
	p := f.project("host-a")
	ctx := t.Context()
	ordinary, err := p.NewTask(ctx, "", api.NewTask{Type: "feature", Slug: "ordinary", Title: "Ordinary"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := p.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	const original = " 用户原话 \r\n## DISCUSSION\r\nSELF_REVIEW: x\r\n- OWNER: x\r\n```\r\n\ttext\r\n``` 最后一行 "
	input := api.NewTask{Type: "feature", Slug: "request", Title: "Request", Submitter: "ignored", Description: original}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	const key = "request-creation"
	res := requestRecordPost(t, f, p.ID(), key, string(raw), http.StatusCreated)
	var created api.Task
	if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.State != "backlog" || created.Revision != 1 ||
		!strings.Contains(board.CardHeader(created.Text), "- SUBMITTER: Alice\n- REQUEST: request.md\n") {
		t.Fatalf("unexpected created card: %+v", created)
	}
	headings := regexp.MustCompile(`(?m)^## [^\n]+`)
	discussion, _ := board.SectionBody(created.Text, board.SectionDiscussion)
	if !slices.Equal(headings.FindAllString(created.Text, -1), headings.FindAllString(ordinary.Text, -1)) ||
		strings.Contains(discussion, "SELF_REVIEW") || board.MetadataFrom(created.Text, board.FieldOwner) != "" {
		t.Fatalf("request text affected the contract:\n%s", created.Text)
	}
	doc, err := p.Document(ctx, created.TaskID, "request.md")
	if err != nil || doc.Text != strings.ReplaceAll(original, "\r\n", "\n") || doc.ExpectedRevision != created.Revision {
		t.Fatalf("request document = %+v, %v", doc, err)
	}

	replay := requestRecordPost(t, f, p.ID(), key, string(raw), http.StatusCreated)
	if replay.Body.String() != res.Body.String() {
		t.Fatalf("replayed response changed: %s -> %s", res.Body, replay.Body)
	}
	input.Description += "different"
	raw, err = json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	mismatch := requestRecordPost(t, f, p.ID(), key, string(raw), http.StatusUnprocessableEntity)
	assertRequestRecordError(t, mismatch.Body.Bytes(), api.CodeIdempotencyMismatch, "")

	list, err := p.List(ctx, "")
	if err != nil || len(list.Tasks) != 2 {
		t.Fatalf("list after replay: %+v, %v", list, err)
	}
	want := map[string]bool{ordinary.TaskID: false, created.TaskID: true}
	for _, item := range list.Tasks {
		isRequest, ok := want[item.TaskID]
		if !ok || item.Request != isRequest {
			t.Fatalf("unexpected list row: %+v", item)
		}
		access, err := f.srv.TaskAccess(ctx, p.ID(), item.TaskID)
		if err != nil || access.Request != isRequest {
			t.Fatalf("unexpected task access: %+v, %v", access, err)
		}
	}
	page, err := p.Events(ctx, before.Cursor, 0)
	if err != nil || page.Reset || len(page.Events) != 1 || page.Events[0].Kind != api.EventTaskChanged ||
		page.Events[0].TaskID != created.TaskID || page.Events[0].Revision != created.Revision {
		t.Fatalf("expected exactly one task_changed: %+v, %v", page, err)
	}
}

func assertRequestRecordError(t *testing.T, body []byte, code, boardCode string) {
	t.Helper()
	var result api.Error
	if err := json.Unmarshal(body, &result); err != nil || result.Code != code || result.BoardCode != boardCode {
		t.Fatalf("expected error %s (%s), got %s, %v", code, boardCode, body, err)
	}
}

func TestRequestDescriptionValidation(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	for _, tc := range []struct{ name, description string }{
		{"blank", " \t\r\n\u3000"},
		{"large", strings.Repeat("界", 21846) + "\r\n"},
		{"nul", "NUL\x00"},
		{"escape", "ESC\x1b"},
		{"carriage-return", "CR\rtext"},
		{"delete", "DEL\x7f"},
		{"c1", "C1\u0085"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(api.NewTask{Type: "feature", Slug: tc.name, Title: tc.name, Description: tc.description})
			if err != nil {
				t.Fatal(err)
			}
			res := requestRecordPost(t, f, p.ID(), tc.name, string(raw), http.StatusBadRequest)
			assertRequestRecordError(t, res.Body.Bytes(), api.CodeInvalidRequest, "board.request_description_invalid")
		})
	}
	invalidUTF8 := requestRecordPost(t, f, p.ID(), "invalid-utf8",
		"{\"type\":\"feature\",\"slug\":\"invalid-utf8\",\"title\":\"Invalid UTF-8\",\"description\":\"text\xff\"}", http.StatusBadRequest)
	assertRequestRecordError(t, invalidUTF8.Body.Bytes(), api.CodeInvalidRequest, "")
	if list, err := p.List(t.Context(), ""); err != nil || len(list.Tasks) != 0 {
		t.Fatalf("invalid requests created cards: %+v, %v", list, err)
	}

	normalized := "\t" + strings.Repeat("x", 65534) + "\n"
	raw, err := json.Marshal(api.NewTask{Type: "feature", Slug: "boundary", Title: "Boundary", Description: strings.ReplaceAll(normalized, "\n", "\r\n")})
	if err != nil {
		t.Fatal(err)
	}
	res := requestRecordPost(t, f, p.ID(), "boundary", string(raw), http.StatusCreated)
	var task api.Task
	if err := json.Unmarshal(res.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	if doc, err := p.Document(t.Context(), task.TaskID, "request.md"); err != nil || doc.Text != normalized {
		t.Fatalf("record at limit: %d bytes, %v", len(doc.Text), err)
	}
}

func TestRequestRecordIsManagedAcrossUpdatesAndMoves(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	ctx := t.Context()
	newCard := func(slug, description string) api.Task {
		t.Helper()
		task, err := p.NewTask(ctx, "", api.NewTask{Type: "feature", Slug: slug, Title: slug, Description: description})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	const (
		marker   = "- REQUEST: request.md\n"
		original = "Original request"
	)
	task := newCard("protected", original)
	plain := newCard("ordinary", "")
	assertManaged := func(task api.Task, path, text, decision string) {
		t.Helper()
		_, err := p.PutDocument(ctx, "", task.TaskID, api.Document{
			Path: path, Text: text, ExpectedRevision: task.Revision, ContractDecision: decision,
		}, nil)
		var apiErr *api.Error
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnprocessableEntity ||
			apiErr.Code != api.CodeBoardRejected || apiErr.BoardCode != "board.transaction_managed" {
			t.Fatalf("PUT %s, with decision=%t: %v", path, decision != "", err)
		}
		shown, err := p.Show(ctx, task.TaskID)
		if err != nil || shown.Revision != task.Revision || shown.Text != task.Text {
			t.Fatalf("rejected update changed card: %+v, %v", shown, err)
		}
	}
	for _, decision := range []string{"", "Approve the contract change"} {
		assertManaged(task, "spec.md", strings.Replace(task.Text, marker, "", 1), decision)
		assertManaged(task, "spec.md", strings.Replace(task.Text, marker, "- REQUEST: other.md\n", 1), decision)
		assertManaged(task, "request.md", "replacement", decision)
	}
	assertManaged(task, "request.md", "", "")
	assertManaged(task, "REQUEST.MD", "case variant", "")
	assertManaged(plain, "spec.md", strings.Replace(plain.Text, "- ASSIGNEE:", marker+"- ASSIGNEE:", 1), "")

	if _, err := p.PutDocument(ctx, "", plain.TaskID, api.Document{Path: "request.md", Text: "ordinary attachment", ExpectedRevision: plain.Revision}, nil); err != nil {
		t.Fatal(err)
	}
	if doc, err := p.Document(ctx, plain.TaskID, "request.md"); err != nil || doc.Text != "ordinary attachment" {
		t.Fatalf("ordinary attachment: %+v, %v", doc, err)
	}
	replacement := strings.Replace(task.Text, "# protected", "# Renamed", 1)
	updated, err := p.PutDocument(ctx, "", task.TaskID, api.Document{Path: "spec.md", Text: replacement, ExpectedRevision: task.Revision}, nil)
	if err != nil || updated.Text != replacement {
		t.Fatalf("update preserving REQUEST: %+v, %v", updated, err)
	}
	if doc, err := p.Document(ctx, task.TaskID, "request.md"); err != nil || doc.Text != original {
		t.Fatalf("record changed after updates: %+v, %v", doc, err)
	}

	for _, tc := range []struct {
		task          api.Task
		state, result string
	}{
		{updated, "archived", "cancelled"},
		{newCard("trash", original), "trash", "trashed"},
	} {
		moved, err := p.Move(ctx, "", tc.task.TaskID, api.Move{State: tc.state, Result: tc.result, Reason: "Test move", Decision: "test-decision"}, nil)
		if err != nil || moved.State != tc.state || !strings.Contains(moved.Text, marker) {
			t.Fatalf("move into %s: %+v, %v", tc.state, moved, err)
		}
		doc, err := p.Document(ctx, moved.TaskID, "request.md")
		if err != nil || doc.Text != original {
			t.Fatalf("record after move into %s: %+v, %v", tc.state, doc, err)
		}
	}
}
