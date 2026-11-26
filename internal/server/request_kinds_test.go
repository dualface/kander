package server_test

import (
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
)

// The server stores and lists the kinds a client sends: a note without a
// dispatch, a wrap-up proposal, and the contradictions it refuses.
func TestServerCreatesNoteAndWrapUpRequests(t *testing.T) {
	f := newFixture(t)
	holder, sender := f.project("host-a"), f.project("host-b")
	card := todoTask(t, holder, "note-and-wrap-up")
	grant, err := claim(t, holder, card.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	gen := grant.Claim.Generation
	note, err := sender.CreateRequest(t.Context(), "note-key", card.TaskID, api.NewCollabRequest{Kind: api.RequestNote, Message: "group branch advanced", TargetGeneration: gen})
	if err != nil || note.Kind != api.RequestNote || note.Dispatch != nil {
		t.Fatalf("note: %+v %v", note, err)
	}
	wrapUp := board.DispatchInput{ID: "wrap-up-kind", TaskID: card.TaskID, Kind: "wrap-up", Base: strings.Repeat("a", 40), Message: "Wrap up the delivery"}
	proposal := api.NewCollabRequest{Kind: api.RequestWrapUp, Message: wrapUp.Message, Dispatch: &wrapUp, CardRevision: grant.Task.Revision, TargetGeneration: gen, AcceptWithin: api.DurationOf(120 * time.Second)}
	created, err := sender.CreateRequest(t.Context(), "wrap-up-key", card.TaskID, proposal)
	if err != nil || created.Kind != api.RequestWrapUp || created.Dispatch == nil || created.Dispatch.Kind != "wrap-up" {
		t.Fatalf("wrap-up proposal: %+v %v", created, err)
	}

	// The same dispatch ID with another kind returns the first request.
	other := proposal
	other.Kind = api.RequestSync
	again, err := sender.CreateRequest(t.Context(), "wrap-up-retry", card.TaskID, other)
	if err != nil || again.ID != created.ID || again.Kind != api.RequestWrapUp {
		t.Fatalf("retry with another kind: %+v %v", again, err)
	}

	withDispatch := proposal
	withDispatch.Kind = api.RequestNote
	_, err = sender.CreateRequest(t.Context(), "note-with-dispatch", card.TaskID, withDispatch)
	expectCode(t, err, api.CodeInvalidRequest)
	if !strings.Contains(err.Error(), "note request carries no dispatch") {
		t.Fatalf("note with dispatch: %v", err)
	}
	_, err = sender.CreateRequest(t.Context(), "wrap-up-bare", card.TaskID, api.NewCollabRequest{Kind: api.RequestWrapUp, Message: "bare", TargetGeneration: gen})
	expectCode(t, err, api.CodeInvalidRequest)
	_, err = sender.CreateRequest(t.Context(), "unknown-kind", card.TaskID, api.NewCollabRequest{Kind: "surprise", Message: "x", TargetGeneration: gen})
	expectCode(t, err, api.CodeInvalidRequest)
	if !strings.Contains(err.Error(), `"surprise"`) {
		t.Fatalf("unknown kind is not named: %v", err)
	}

	listed, err := sender.Requests(t.Context(), card.TaskID)
	kinds := map[string]bool{}
	for _, r := range listed {
		kinds[r.Kind] = true
	}
	if err != nil || len(listed) != 2 || !kinds[api.RequestNote] || !kinds[api.RequestWrapUp] {
		t.Fatalf("listed requests: %+v %v", listed, err)
	}
}
