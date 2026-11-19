package server_test

import (
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

// propose sends a fix proposal that binds the current card version.
func (m *reviewMember) propose(t *testing.T, key, dispatchID string) api.CollabRequest {
	t.Helper()
	input := board.DispatchInput{ID: dispatchID, TaskID: m.task, Kind: "fix", Message: "Fix the reviewed finding", Base: strings.Repeat("a", 40)}
	request, err := m.sender.CreateRequest(t.Context(), key, m.task, api.NewCollabRequest{Kind: api.RequestFix, Message: input.Message, Dispatch: &input, CardRevision: m.card.Revision, TargetGeneration: m.grant.Claim.Generation, AcceptWithin: api.DurationOf(120 * time.Second)})
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

// A holder that never prepares a delivered fix proposal moves the card out of
// review with a plain move. Before the guard the move was accepted and left
// the request as it was, with no dispatch behind it.
func TestBareMoveBypassesAPendingProposalReproduction(t *testing.T) {
	for _, status := range []string{api.RequestAccepted, api.RequestOpen} {
		t.Run(status, func(t *testing.T) {
			m := newReviewMember(t, "bypass-"+status, "20261119-move-group")
			proposal := m.propose(t, "proposal", "bypass-fix")
			if status == api.RequestAccepted {
				m.answer(t, "deliver", proposal.ID, api.RequestAccepted)
			}
			moved, err := m.bareMove("bare")
			if err != nil || moved.State != "working" {
				t.Fatalf("bare move: %+v %v", moved, err)
			}
			if got := m.request(t, proposal.ID); got.Status != status {
				t.Fatalf("the request is %s, want %s", got.Status, status)
			}
			if d, err := m.holder.Dispatch(t.Context(), m.task, "bypass-fix"); err == nil {
				t.Fatalf("a dispatch record exists: %+v", d)
			}
			if strings.Contains(moved.Text, "DISPATCH_ID") {
				t.Fatal("the card is bound to a dispatch")
			}
		})
	}
}
