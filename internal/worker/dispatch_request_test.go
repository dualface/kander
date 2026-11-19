package worker

import (
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
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

func TestRequestLineKeepsUnicodeSeparatorsOutOfTheTerminal(t *testing.T) {
	line := requestLine("task", api.CollabRequest{ID: "request", Kind: api.RequestSync, Message: "one\u2028two\u2029three\u0085four", FromClient: "peer"})
	if strings.ContainsAny(line, "\u2028\u2029\u0085\n\r") || !strings.Contains(line, "kander request show") {
		t.Fatalf("unsafe or incomplete request line: %q", line)
	}
}

const noteLineWarning = "no dispatch is attached: handle the message within the card contract; do not run kander dispatch prepare for it and do not use it as the grant of a dispatch-bound move"

func TestRequestLineOfANoteStatesThatNoDispatchIsAttached(t *testing.T) {
	note := requestLine("task", api.CollabRequest{ID: "req-1", Kind: api.RequestNote, Message: "group branch advanced", FromClient: "peer"})
	if !strings.Contains(note, "# kander-request req-1 note for task from peer: group branch advanced; "+noteLineWarning+"; inspect the full request") || strings.Contains(note, "this is information") {
		t.Fatalf("note line: %q", note)
	}
	// Every other kind keeps the line it had, apart from the kind itself.
	want := requestLine("task", api.CollabRequest{ID: "req-1", Kind: api.RequestSync, Message: "group branch advanced", FromClient: "peer"})
	for _, kind := range []string{api.RequestFix, api.RequestSync, api.RequestWrapUp, api.RequestReview, api.RequestStop} {
		line := requestLine("task", api.CollabRequest{ID: "req-1", Kind: kind, Message: "group branch advanced", FromClient: "peer"})
		if strings.Contains(line, "no dispatch is attached") || strings.Replace(line, " "+kind+" for ", " sync for ", 1) != want {
			t.Errorf("%s line differs beyond the kind: %q", kind, line)
		}
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
	if len(h.lines) != 1 || !strings.Contains(h.lines[0], "# kander-request "+r.ID+" note for ") || !strings.Contains(h.lines[0], noteLineWarning) {
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
