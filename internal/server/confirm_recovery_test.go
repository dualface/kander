package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
)

func postRecoveryConfirmation(t *testing.T, s *Server, project, key string, card api.Task) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"expected_revision":%d,"confirmed_by":"Alice"}`, card.Revision)
	url := "/api/v1/projects/" + project + "/tasks/" + card.TaskID + "/confirm"
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader(body))
	r.Header.Set(api.HeaderProtocol, strconv.Itoa(api.ProtocolVersion))
	r.Header.Set(api.HeaderClientID, "host-a")
	r.Header.Set(api.HeaderIdempotencyKey, key)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func prepareRequestCard(t *testing.T, url string) (*client.Project, api.Task) {
	t.Helper()
	c, err := client.New(url, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	info, err := c.Register(t.Context(), "demo", "")
	if err != nil {
		t.Fatal(err)
	}
	p := c.Project(info.ID)
	card, err := p.NewTask(t.Context(), "", api.NewTask{
		Type: "feature", Slug: "confirm-recovery", Title: "Confirmation recovery", Description: "Original request",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(card.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\nCARD_REVIEW: PASS\n", 1)
	card, err = p.PutDocument(t.Context(), "", card.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: card.Revision}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return p, card
}

func TestRequestMoveDoesNotReplayAnIndependentConfirmation(t *testing.T) {
	s, h := openTest(t, t.TempDir(), nil)
	t.Cleanup(func() { h.Close(); _ = s.Close() })
	p, card := prepareRequestCard(t, h.URL)
	proj, err := s.project(p.ID())
	if err != nil {
		t.Fatal(err)
	}
	const key = "interrupted-request-move"
	op := "move:" + card.TaskID
	move := api.Move{State: "todo", ExpectedRevision: &card.Revision}
	if err := writeJSON(proj.root, receiptPath(key), receipt{
		Schema: receiptSchema, Key: key, Operation: op, Digest: requestDigest(op, move), Status: receiptPending,
	}); err != nil {
		t.Fatal(err)
	}
	confirmed, err := p.Confirm(t.Context(), "independent-confirmation", card.TaskID, api.Confirm{ExpectedRevision: card.Revision, ConfirmedBy: "Alice"})
	if err != nil || confirmed.State != "todo" {
		t.Fatalf("independent confirmation returned state %s, %v", confirmed.State, err)
	}
	_, err = p.Move(t.Context(), key, card.TaskID, move, nil)
	if !api.IsCode(err, api.CodeRevisionConflict) {
		t.Fatalf("pending move retry returned %v, want the stale revision error", err)
	}
	after, err := p.Show(t.Context(), card.TaskID)
	if err != nil || after.OperationID != confirmed.OperationID || after.Revision != confirmed.Revision || after.Text != confirmed.Text {
		t.Fatalf("retry changed the confirmation: operation=%q, revision=%d, err=%v", after.OperationID, after.Revision, err)
	}
}

func TestConfirmPublishesOnceWhenReceiptReadFails(t *testing.T) {
	s, h := openTest(t, t.TempDir(), nil)
	t.Cleanup(func() { h.Close(); _ = s.Close() })
	p, card := prepareRequestCard(t, h.URL)
	proj, err := s.project(p.ID())
	if err != nil {
		t.Fatal(err)
	}
	cursor := proj.events.cursor()
	const key = "confirm-with-uncertain-response"
	rel := receiptPath(key)
	var injected bool
	proj.receiptReadFault = func(path string) error {
		if path == rel && !injected {
			injected = true
			return io.ErrUnexpectedEOF
		}
		return nil
	}
	first := postRecoveryConfirmation(t, s, p.ID(), key, card)
	var failure api.Error
	if !injected || first.Code != http.StatusInternalServerError || json.Unmarshal(first.Body.Bytes(), &failure) != nil || failure.Code != api.CodeInternal {
		t.Fatalf("fault=%t, confirmation returned HTTP %d: %.200s, want 500 internal", injected, first.Code, first.Body.String())
	}
	var saved receipt
	if found, err := readJSON(proj.root, rel, &saved); err != nil || !found || saved.Status != receiptDone || saved.HTTP != http.StatusOK {
		t.Fatalf("confirmation must have committed its success receipt: status=%q, HTTP=%d, found=%t, err=%v", saved.Status, saved.HTTP, found, err)
	}
	checkEvent := func(phase string) {
		t.Helper()
		page, err := p.Events(t.Context(), cursor, 0)
		if err != nil || page.Reset || len(page.Events) != 1 {
			t.Errorf("%s: old cursor did not see exactly one event: %+v, %v", phase, page, err)
			return
		}
		e := page.Events[0]
		if e.Kind != api.EventTaskChanged || e.TaskID != card.TaskID || e.State != "todo" || e.Revision != card.Revision+1 {
			t.Errorf("%s: wrong confirmation event: %+v", phase, e)
		}
	}
	checkEvent("after the response error")
	retry := postRecoveryConfirmation(t, s, p.ID(), key, card)
	if retry.Code != http.StatusOK || retry.Body.String() != string(saved.Body)+"\n" {
		t.Fatalf("same-process retry did not return the original response: HTTP %d: %.200s", retry.Code, retry.Body.String())
	}
	checkEvent("after replay")
}
