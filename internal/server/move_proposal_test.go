package server_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/server"
)

// reviewMember is a card held by host-a and waiting in review, with a sender
// that proposes dispatches to its holder.
type reviewMember struct {
	f              *fixture
	holder, sender *client.Project
	grant          api.ClaimGrant
	proof          client.Claim
	task           string
	card           api.Task
}

// newReviewMember delivers a card and moves it into review. A non-empty group
// makes the card a task group member.
func newReviewMember(t *testing.T, slug string, group ...string) *reviewMember {
	t.Helper()
	// The evidence of a fix proposal is not the subject of these tests.
	t.Cleanup(server.SetFixProposalCheck(func(*board.Transaction, board.DispatchInput) error { return nil }))
	f := newFixture(t)
	m := &reviewMember{f: f, holder: f.project("host-a"), sender: f.project("host-b")}
	card := todoTask(t, m.holder, slug, group...)
	text := strings.Replace(card.Text, "- TASK_BRANCH:", "- TASK_BRANCH: task/"+slug, 1)
	card, err := m.holder.PutDocument(t.Context(), "", card.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: card.Revision}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.grant, err = claim(t, m.holder, card.TaskID, "host-a"); err != nil {
		t.Fatal(err)
	}
	m.proof = client.Claim{Token: m.grant.Token, Generation: m.grant.Claim.Generation}
	m.task = card.TaskID
	commit := strings.Repeat("b", 40)
	if _, err := m.holder.PublishDelivery(t.Context(), "", api.DeliveryEvidence{Purpose: "delivery", TaskID: m.task, ExecutionID: m.grant.Claim.ExecutionID, Generation: m.grant.Claim.Generation, CardRevision: m.grant.Task.Revision, Commit: commit, Ref: api.DeliveryRef(m.holder.ID(), m.task, m.grant.Claim.Generation, commit), Remote: "git@example.invalid:demo.git", ObservedAt: f.clock.now()}, m.proof); err != nil {
		t.Fatal(err)
	}
	if m.card, err = m.holder.Move(t.Context(), "", m.task, api.Move{State: "review", DeliveryCommit: commit}, &m.proof); err != nil {
		t.Fatal(err)
	}
	return m
}

// propose sends a proposal of kind (fix or sync) that binds the current card
// version.
func (m *reviewMember) propose(t *testing.T, kind, key, dispatchID string) api.CollabRequest {
	t.Helper()
	input := board.DispatchInput{ID: dispatchID, TaskID: m.task, Kind: kind, Message: "Rework the delivered card", Base: strings.Repeat("a", 40)}
	request, err := m.sender.CreateRequest(t.Context(), key, m.task, api.NewCollabRequest{Kind: kind, Message: input.Message, Dispatch: &input, CardRevision: m.card.Revision, TargetGeneration: m.grant.Claim.Generation, AcceptWithin: api.DurationOf(120 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func (m *reviewMember) answer(t *testing.T, key, id, status string) {
	t.Helper()
	if _, err := m.holder.AnswerRequest(t.Context(), key, m.task, id, api.CollabAnswer{Status: status}, m.proof); err != nil {
		t.Fatal(err)
	}
}

func (m *reviewMember) request(t *testing.T, id string) api.CollabRequest {
	t.Helper()
	requests, err := m.holder.Requests(t.Context(), m.task)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range requests {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("request %s does not exist", id)
	return api.CollabRequest{}
}

// bareMove is the move a holder makes after it ignored the proposal.
func (m *reviewMember) bareMove(key string) (api.Task, error) {
	return m.holder.Move(m.f.t.Context(), key, m.task, api.Move{State: "working"}, &m.proof)
}

// seed rewrites a request record the way a state the API cannot reach looks.
func (m *reviewMember) seed(t *testing.T, request api.CollabRequest, change func(*api.CollabRequest)) api.CollabRequest {
	t.Helper()
	change(&request)
	request.Revision++
	if err := m.f.srv.SeedRequest(m.holder.ID(), request); err != nil {
		t.Fatal(err)
	}
	return request
}

// requireRefusal asserts the refusal of a plain move: the error code, a message
// that names every request and dispatch and both ways out, and a card and
// requests that did not change.
func (m *reviewMember) requireRefusal(t *testing.T, before []api.CollabRequest, requests []api.CollabRequest, err error) {
	t.Helper()
	refusal := apiError(t, err)
	if refusal.Code != api.CodeEvidenceIncomplete {
		t.Fatalf("refusal %+v", refusal)
	}
	for _, request := range requests {
		for _, want := range []string{request.ID, request.Dispatch.ID} {
			if !strings.Contains(refusal.Message, want) {
				t.Fatalf("refusal lacks %q:\n%s", want, refusal.Message)
			}
		}
	}
	for _, want := range []string{"kander dispatch prepare", "kander request answer " + m.task, "rejected"} {
		if !strings.Contains(refusal.Message, want) {
			t.Fatalf("refusal lacks %q:\n%s", want, refusal.Message)
		}
	}
	current, err := m.holder.Show(t.Context(), m.task)
	if err != nil || current.State != "review" || current.Revision != m.card.Revision {
		t.Fatalf("card after the refusal: %+v %v, want review at revision %d", current, err, m.card.Revision)
	}
	after, err := m.holder.Requests(t.Context(), m.task)
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("requests after the refusal: %+v %v, want %+v", after, err, before)
	}
}

func (m *reviewMember) requests(t *testing.T) []api.CollabRequest {
	t.Helper()
	requests, err := m.holder.Requests(t.Context(), m.task)
	if err != nil {
		t.Fatal(err)
	}
	return requests
}

// requireMoved asserts that a plain move was accepted: the card is in working
// and the requests are as before.
func (m *reviewMember) requireMoved(t *testing.T, before []api.CollabRequest, moved api.Task, err error) {
	t.Helper()
	if err != nil || moved.State != "working" {
		t.Fatalf("bare move: %+v %v", moved, err)
	}
	if after := m.requests(t); !reflect.DeepEqual(after, before) {
		t.Fatalf("requests after the move: %+v, want %+v", after, before)
	}
}

// A plain move out of review is refused while a task group card has a proposal
// that the holder could still prepare, whether the worker already delivered it
// (accepted) or not (open). The refusal names the way out and changes nothing.
func TestBareMoveIsRefusedWhileAProposalIsPending(t *testing.T) {
	for _, status := range []string{api.RequestAccepted, api.RequestOpen} {
		t.Run(status, func(t *testing.T) {
			m := newReviewMember(t, "refuse-"+status, "20261119-move-group")
			proposal := m.propose(t, api.RequestFix, "proposal", "refuse-fix")
			if status == api.RequestAccepted {
				m.answer(t, "deliver", proposal.ID, api.RequestAccepted)
			}
			before := m.requests(t)
			_, err := m.bareMove("bare")
			m.requireRefusal(t, before, before, err)
			if got := m.request(t, proposal.ID); got.Status != status {
				t.Fatalf("the request is %s, want %s", got.Status, status)
			}
			if d, err := m.holder.Dispatch(t.Context(), m.task, "refuse-fix"); err == nil {
				t.Fatalf("a dispatch record exists: %+v", d)
			}
		})
	}
}

// Every pending proposal is named, in the order the requests were created.
func TestBareMoveRefusalNamesEveryPendingProposal(t *testing.T) {
	m := newReviewMember(t, "refuse-two", "20261119-move-group")
	first := m.propose(t, api.RequestFix, "first", "refuse-first")
	m.f.clock.advance(time.Second)
	second := m.propose(t, api.RequestSync, "second", "refuse-second")
	before := m.requests(t)
	_, err := m.bareMove("bare")
	m.requireRefusal(t, before, before, err)
	message := apiError(t, err).Message
	if i, j := strings.Index(message, first.ID), strings.Index(message, second.ID); i < 0 || j < i {
		t.Fatalf("the requests are not named in creation order:\n%s", message)
	}
}

// Nothing checks the dispatch ID of a proposal when it is created, and no
// preparation can take an ID that is not a dispatch ID. The kernel still accepts
// the proposal and no dispatch record names it, so the move is refused like any
// other; the holder answers the request "rejected" and asks the sender to
// propose again with a valid ID.
func TestBareMoveIsRefusedForAProposalWithAnInvalidDispatchID(t *testing.T) {
	m := newReviewMember(t, "invalid-dispatch-id", "20261119-move-group")
	m.propose(t, api.RequestSync, "proposal", "Not A Dispatch ID")
	before := m.requests(t)
	_, err := m.bareMove("bare")
	m.requireRefusal(t, before, before, err)
}

// The guard only refuses a proposal the holder could still prepare. Every other
// plain move keeps the behavior it had, so a holder is never stuck on a
// proposal it cannot prepare.
func TestBareMoveKeepsItsBehaviorWithoutAPreparableProposal(t *testing.T) {
	const group = "20261119-move-group"
	for _, status := range []string{api.RequestDone, api.RequestSuperseded} {
		t.Run("settled-"+status, func(t *testing.T) {
			m := newReviewMember(t, "settled-"+status, group)
			proposal := m.propose(t, api.RequestFix, "proposal", "settled-fix")
			if status == api.RequestSuperseded {
				// Only a handoff supersedes a request, and it changes the generation.
				m.seed(t, proposal, func(r *api.CollabRequest) { r.Status = status })
			} else {
				m.answer(t, "settle", proposal.ID, status)
			}
			before := m.requests(t)
			moved, err := m.bareMove("bare")
			m.requireMoved(t, before, moved, err)
		})
	}
	for _, status := range []string{api.RequestOpen, api.RequestAccepted} {
		t.Run("other-generation-"+status, func(t *testing.T) {
			m := newReviewMember(t, "other-generation-"+status, group)
			proposal := m.propose(t, api.RequestFix, "proposal", "other-generation-fix")
			m.seed(t, proposal, func(r *api.CollabRequest) { r.Status, r.TargetGeneration = status, r.TargetGeneration+1 })
			before := m.requests(t)
			moved, err := m.bareMove("bare")
			m.requireMoved(t, before, moved, err)
		})
	}
	t.Run("window-elapsed", func(t *testing.T) {
		m := newReviewMember(t, "window-elapsed", group)
		m.propose(t, api.RequestFix, "proposal", "window-fix")
		// The lease outlives the window only when it is renewed in between.
		m.f.clock.advance(61 * time.Second)
		if _, err := m.holder.Renew(t.Context(), m.task, m.proof); err != nil {
			t.Fatal(err)
		}
		m.f.clock.advance(60 * time.Second)
		before := m.requests(t)
		moved, err := m.bareMove("bare")
		m.requireMoved(t, before, moved, err)
	})
	t.Run("card-version-changed", func(t *testing.T) {
		m := newReviewMember(t, "card-changed", group)
		m.propose(t, api.RequestFix, "proposal", "card-fix")
		changed, err := m.holder.PutDocument(t.Context(), "progress", m.task, api.Document{Path: "progress.md", Text: "later content", ExpectedRevision: m.card.Revision}, &m.proof)
		if err != nil {
			t.Fatal(err)
		}
		m.card = changed
		before := m.requests(t)
		moved, err := m.bareMove("bare")
		m.requireMoved(t, before, moved, err)
	})
	t.Run("no-proposal", func(t *testing.T) {
		m := newReviewMember(t, "no-proposal", group)
		moved, err := m.bareMove("bare")
		m.requireMoved(t, m.requests(t), moved, err)
	})
	t.Run("note-request", func(t *testing.T) {
		m := newReviewMember(t, "note-request", group)
		if _, err := m.sender.CreateRequest(t.Context(), "note", m.task, api.NewCollabRequest{Kind: api.RequestNote, Message: "a remark", TargetGeneration: m.grant.Claim.Generation}); err != nil {
			t.Fatal(err)
		}
		before := m.requests(t)
		moved, err := m.bareMove("bare")
		m.requireMoved(t, before, moved, err)
	})
	t.Run("not-a-group-card", func(t *testing.T) {
		m := newReviewMember(t, "not-group")
		proposal := m.propose(t, api.RequestFix, "proposal", "not-group-fix")
		m.answer(t, "deliver", proposal.ID, api.RequestAccepted)
		before := m.requests(t)
		moved, err := m.bareMove("bare")
		m.requireMoved(t, before, moved, err)
	})
	for _, start := range []string{api.RequestOpen, api.RequestAccepted} {
		t.Run("rejected-by-the-holder-from-"+start, func(t *testing.T) {
			m := newReviewMember(t, "answered-"+start, group)
			proposal := m.propose(t, api.RequestFix, "proposal", "answered-fix")
			if start == api.RequestAccepted {
				m.answer(t, "deliver", proposal.ID, api.RequestAccepted)
			}
			m.answer(t, "reject", proposal.ID, api.RequestRejected)
			before := m.requests(t)
			moved, err := m.bareMove("bare")
			m.requireMoved(t, before, moved, err)
		})
	}
}

// A move with the authorization of a prepared dispatch is not a plain move, and
// a card that already carries a dispatch binding keeps the refusal of the
// dispatch receipt: the guard does not answer for it.
func TestBareMoveGuardLeavesDispatchBoundMovesAlone(t *testing.T) {
	const group = "20261119-move-group"
	t.Run("authorized-move", func(t *testing.T) {
		m := newReviewMember(t, "authorized", group)
		// The server clock of the fixture is not the clock of the board, which
		// stamps and expires a dispatch itself, so the holder prepares a dispatch
		// of its own while the proposal is pending.
		m.propose(t, api.RequestSync, "proposal", "pending-sync")
		input := board.DispatchInput{ID: "authorized-sync", TaskID: m.task, Kind: "sync", Message: "Synchronize the verified target commit", Base: strings.Repeat("a", 40)}
		prepared, err := m.holder.DispatchAction(t.Context(), "prepare", m.task, "prepare", api.DispatchCommand{Input: &input, CardRevision: m.card.Revision}, m.proof)
		if err != nil {
			t.Fatal(err)
		}
		moved, err := m.holder.Move(t.Context(), "authorized-move", m.task, api.Move{State: "working", Authorization: prepared.Authorization}, &m.proof)
		if err != nil || moved.State != "working" {
			t.Fatalf("authorized move: %+v %v", moved, err)
		}
	})
	t.Run("bound-card", func(t *testing.T) {
		m := newReviewMember(t, "bound", group)
		first := m.propose(t, api.RequestSync, "first", "bound-first")
		input := *first.Dispatch
		if _, err := m.holder.DispatchAction(t.Context(), "prepare", m.task, "prepare", api.DispatchCommand{Input: &input, CardRevision: m.card.Revision}, m.proof); err != nil {
			t.Fatal(err)
		}
		bound, err := m.holder.Show(t.Context(), m.task)
		if err != nil || !strings.Contains(bound.Text, "DISPATCH_ID: bound-first") {
			t.Fatalf("the card is not bound: %v\n%s", err, bound.Text)
		}
		m.card = bound
		m.propose(t, api.RequestSync, "second", "bound-second")
		_, err = m.bareMove("bare")
		refusal := apiError(t, err)
		if refusal.Code != api.CodeBoardRejected || refusal.BoardCode != "board.dispatch_conflict" {
			t.Fatalf("the refusal of a bound card is not the dispatch receipt's: %+v", refusal)
		}
		if current, err := m.holder.Show(t.Context(), m.task); err != nil || current.State != "review" {
			t.Fatalf("card after the refusal: %+v %v", current, err)
		}
	})
}
