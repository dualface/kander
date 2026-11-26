package server

import (
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
)

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
