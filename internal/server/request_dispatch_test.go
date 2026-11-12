package server_test

import (
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

func TestDispatchRequestIdentityIsUniqueAndUsesServiceDeadline(t *testing.T) {
	f := newFixture(t)
	holder, sender := f.project("host-a"), f.project("host-b")
	card := todoTask(t, holder, "unique-dispatch-proposal")
	grant, err := claim(t, holder, card.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	intent := board.DispatchInput{ID: "unique-proposal", TaskID: card.TaskID, Kind: "sync", Base: strings.Repeat("a", 40), Message: "Inspect the synchronized commit"}
	input := api.NewCollabRequest{Kind: api.RequestSync, Message: intent.Message, Dispatch: &intent, CardRevision: grant.Task.Revision, TargetGeneration: grant.Claim.Generation, AcceptWithin: api.DurationOf(120 * time.Second)}
	first, err := sender.CreateRequest(t.Context(), "first-http-key", card.TaskID, input)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Dispatch.CreatedAt.Equal(f.clock.now()) || !first.Dispatch.ConfirmBy.Equal(f.clock.now().Add(120*time.Second)) {
		t.Fatalf("client clock determined deadline: %+v", first)
	}
	second, err := sender.CreateRequest(t.Context(), "second-http-key", card.TaskID, input)
	if err != nil || second.ID != first.ID {
		t.Fatalf("retry duplicated proposal: %+v %v", second, err)
	}
	changed := intent
	changed.Message = "Different proposed work"
	other := input
	other.Dispatch, other.Message = &changed, changed.Message
	_, err = sender.CreateRequest(t.Context(), "different-material", card.TaskID, other)
	expectCode(t, err, api.CodeIdempotencyMismatch)
	invalid := input
	invalid.AcceptWithin = 1 << 62
	_, err = sender.CreateRequest(t.Context(), "overflow-window", card.TaskID, invalid)
	expectCode(t, err, api.CodeInvalidRequest)
	f.clock.advance(61 * time.Second)
	proof := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	if _, err := holder.Renew(t.Context(), card.TaskID, proof); err != nil {
		t.Fatal(err)
	}
	f.clock.advance(60 * time.Second)
	_, err = holder.AnswerRequest(t.Context(), "late-reservation", card.TaskID, first.ID, api.CollabAnswer{Status: api.RequestAccepted, ExpectedRevision: first.Revision}, proof)
	expectCode(t, err, api.CodeEvidenceIncomplete)
	requests, err := sender.Requests(t.Context(), card.TaskID)
	if err != nil || len(requests) != 1 {
		t.Fatalf("proposal uniqueness lost: %+v %v", requests, err)
	}
}
