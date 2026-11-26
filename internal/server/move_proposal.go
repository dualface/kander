package server

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
)

// refuseBareMoveOfProposal is the guard of a plain move from review into
// working. A task group card in review takes its rework through a durable
// dispatch; a holder that moves the card without one leaves the proposal that
// asked for the rework without an epoch fence and without a receipt. While the
// request kernel would still accept the Prepare of a proposal, the move is
// refused and names it. A proposal the kernel no longer accepts (an elapsed
// window, another card version or generation, a settled request) never blocks,
// so the holder is not stuck on a proposal it cannot prepare. The guard only asks
// the kernel; it never steps it, and the move changes no request.
//
// tx.Snapshot reads the committed card, also in the callback that runs after the
// move was staged, so both calls decide on the same card version. Callers hold
// p.mu, which createRequest and answerRequest hold as well: no proposal appears
// between this check and the move.
func (p *project) refuseBareMoveOfProposal(tx *board.Transaction, task string, claim claimRecord) error {
	snapshot, err := tx.Snapshot(task)
	if err != nil {
		return err
	}
	if board.TaskGroupFrom(snapshot.Text) == "" || board.MetadataFrom(snapshot.Text, "DISPATCH_ID") != "" {
		return nil
	}
	pending, err := p.preparableProposals(tx, task, claim, snapshot.Revision)
	if err != nil || len(pending) == 0 {
		return err
	}
	var parts, answers []string
	usable := false
	for _, proposal := range pending {
		part := fmt.Sprintf("request %s proposes dispatch %s", proposal.request.ID, proposal.request.Dispatch.ID)
		if proposal.unusable {
			part += " (no dispatch can be prepared with that ID: ask the sender to propose again with a valid dispatch ID)"
		} else {
			usable = true
		}
		parts = append(parts, part)
		answers = append(answers, fmt.Sprintf("kander request answer %s %s rejected", task, proposal.request.ID))
	}
	message := "move into working needs the dispatch of a pending proposal: " + strings.Join(parts, "; ") + "."
	if usable {
		message += " Run kander dispatch prepare for a proposal that can be prepared and move with --dispatch-id and --execution-epoch."
	}
	// Each command to run stands on a line of its own, so it can be copied whole.
	return apiError(api.CodeEvidenceIncomplete, message+" To reject a proposal instead, answer its request:\n"+strings.Join(answers, "\n"))
}

// pendingProposal is a proposal that holds a plain move. unusable says that the
// card cannot read a dispatch under the proposal's dispatch ID (the ID is not a
// dispatch ID, or its record does not match it), so a preparation fails on it.
type pendingProposal struct {
	request  api.CollabRequest
	unusable bool
}

// preparableProposals are the proposals of a task that the request kernel
// accepts a Prepare for, with the holder of claim, the card at cardRevision, and
// no dispatch record of this card yet, in creation order. The project-wide
// registry of dispatch IDs is not read: an ID that another card took still shows
// as pending, and the error then offers the answer "rejected" as the way out.
// The Prepare event is the one the formal preparation builds; its material is
// true because the holder prepares exactly the material the proposal froze.
func (p *project) preparableProposals(tx *board.Transaction, task string, claim claimRecord, cardRevision uint64) ([]pendingProposal, error) {
	requests, err := p.readRequests(task)
	if err != nil {
		return nil, err
	}
	var pending []pendingProposal
	for _, request := range requests {
		if request.Dispatch == nil {
			continue
		}
		frame, event := p.prepareEvent(request, len(proposalsOf(requests, request.Dispatch.ID)), claim, cardRevision, true)
		if !newRequestKernel(frame, &request).accepts(event) {
			continue
		}
		var refusal *board.Error
		switch _, err := tx.StagedDispatch(task, request.Dispatch.ID); {
		case err == nil:
		case errors.Is(err, os.ErrNotExist):
			pending = append(pending, pendingProposal{request: request})
		case errors.As(err, &refusal) && refusal.Code == "board.dispatch_conflict":
			// No dispatch of this card can be read under the ID, so the proposal
			// stays pending and the holder answers it "rejected" to move on.
			pending = append(pending, pendingProposal{request: request, unusable: true})
		default:
			return nil, err
		}
	}
	return pending, nil
}
