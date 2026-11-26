package worker

import (
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
)

func TestWorkerRejectsStaleDispatchBeforeAnyPaneEffect(t *testing.T) {
	h := newHarness(t)
	p, credential := h.claimed("stale-dispatch-request", true)
	card, err := p.Show(t.Context(), credential.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	sender := h.project("host-b")
	input := board.DispatchInput{ID: "stale-request", TaskID: card.TaskID, Kind: "sync", Base: strings.Repeat("a", 40), Message: "Inspect this version"}
	request, err := sender.CreateRequest(t.Context(), "proposal", card.TaskID, api.NewCollabRequest{Kind: api.RequestSync, Message: input.Message, Dispatch: &input, CardRevision: card.Revision, TargetGeneration: credential.Generation})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.PutDocument(t.Context(), "progress", card.TaskID, api.Document{Path: "progress.md", Text: "Later executor version", ExpectedRevision: card.Revision}, credential.Claim()); err != nil {
		t.Fatal(err)
	}
	h.advance(16 * time.Second)
	if _, err := h.pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(h.lines) != 0 {
		t.Fatalf("stale proposal reached a pane: %q", h.lines)
	}
	requests, err := sender.Requests(t.Context(), card.TaskID)
	if err != nil || len(requests) != 1 || requests[0].ID != request.ID || requests[0].Status != api.RequestRejected {
		t.Fatalf("stale outcome: %+v %v", requests, err)
	}
}

const noteWarning = "no dispatch is attached: handle the message within the card contract; do not run kander dispatch prepare for it and do not use it as the grant of a dispatch-bound move"
const proposalWarning = "this is a proposal of dispatch dispatch-7: do not run a bare kander move; first write the dispatch object of the request unchanged to an intent file, run kander dispatch prepare on it, then run kander move with --dispatch-id and --execution-epoch from its output"

func TestRequestFileKeepsDispatchInstructions(t *testing.T) {
	for _, tc := range []struct {
		name     string
		kind     string
		dispatch *board.DispatchInput
		warning  string
	}{
		{"note", api.RequestNote, nil, noteWarning},
		{"proposal", api.RequestSync, &board.DispatchInput{ID: "dispatch-7"}, proposalWarning},
		{"manual sync", api.RequestSync, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := api.CollabRequest{ID: "req-1", Kind: tc.kind, Message: "one\u2028two\u2029three\u0085four\nfive\rsix", Dispatch: tc.dispatch}
			body := requestBody("task", r, newAckMarker())
			if !strings.Contains(body, "\n\n"+r.Message+"\n\n") ||
				strings.Contains(body, noteWarning) != (tc.warning == noteWarning) ||
				strings.Contains(body, proposalWarning) != (tc.warning == proposalWarning) {
				t.Fatalf("request body changed the message or dispatch instructions: %q", body)
			}
		})
	}
}

func TestWorkerTypesTheNoteLineIntoThePane(t *testing.T) {
	h := newHarness(t)
	_, c := h.claimed("note-delivery", true)
	r, err := h.project("host-b").CreateRequest(t.Context(), "", c.TaskID, api.NewCollabRequest{Kind: api.RequestNote, Message: "the group branch advanced", TargetGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	h.advance(16 * time.Second)
	if _, err := h.pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(h.lines) != 1 || !strings.Contains(h.lines[0], "# kander-request "+r.ID+" note for ") || strings.Contains(h.lines[0], r.Message) {
		t.Fatalf("note delivery: %q", h.lines)
	}
}

func TestWorkerNeverRoutesToAChangedServerPane(t *testing.T) {
	h := newHarness(t)
	p, credential := h.claimed("changed-routing", true)
	if _, err := p.Execution(t.Context(), "changed-window", credential.TaskID, api.ExecutionUpdate{Action: api.ExecutionWindow, Window: "tmux:$1:@1:%99"}, *credential.Claim()); err != nil {
		t.Fatal(err)
	}
	sender := h.project("host-b")
	if _, err := sender.CreateRequest(t.Context(), "new-message", credential.TaskID, api.NewCollabRequest{Kind: api.RequestSync, TargetGeneration: credential.Generation, Message: "Inspect current work"}); err != nil {
		t.Fatal(err)
	}
	h.advance(16 * time.Second)
	if _, err := h.pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(h.lines) != 0 {
		t.Fatalf("server address replaced local routing identity: %q", h.lines)
	}
	requests, err := sender.Requests(t.Context(), credential.TaskID)
	if err != nil || len(requests) != 1 || requests[0].Status != api.RequestOpen {
		t.Fatalf("request was reserved without a local target: %+v %v", requests, err)
	}
}
