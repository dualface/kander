package server

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
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
	parts := make([]string, len(pending))
	for i, request := range pending {
		parts[i] = fmt.Sprintf("request %s proposes dispatch %s", request.ID, request.Dispatch.ID)
	}
	return apiError(api.CodeEvidenceIncomplete, fmt.Sprintf("move into working needs the dispatch of a pending proposal: %s. Next step: run kander dispatch prepare for it and move with --dispatch-id and --execution-epoch, or answer it with kander request answer %s %s rejected", strings.Join(parts, "; "), task, pending[0].ID))
}

// preparableProposals are the proposals of a task that the request kernel
// accepts a Prepare for, with the holder of claim, the card at cardRevision, and
// no dispatch record of this card yet, in creation order. The project-wide
// registry of dispatch IDs is not read: an ID that another card took still shows
// as pending, and the error then offers the answer "rejected" as the way out.
// The Prepare event is the one the formal preparation builds; its material is
// true because the holder prepares exactly the material the proposal froze.
func (p *project) preparableProposals(tx *board.Transaction, task string, claim claimRecord, cardRevision uint64) ([]api.CollabRequest, error) {
	requests, err := p.readRequests(task)
	if err != nil {
		return nil, err
	}
	var pending []api.CollabRequest
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
		case errors.Is(err, os.ErrNotExist), errors.As(err, &refusal) && refusal.Code == "board.dispatch_conflict":
			// No record of this card names the ID. An ID that is not a dispatch ID
			// reads as a dispatch conflict and counts here: the proposal stays
			// pending, and the holder answers it "rejected" to move on.
			pending = append(pending, request)
		default:
			return nil, err
		}
	}
	return pending, nil
}
