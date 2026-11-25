package server

import (
	"encoding/json"
	"fmt"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/statemachine/serverproposal"
	"github.com/dualface/kander/internal/statemachine/serverrequest"
)

// dispatchProposal is the proposal a formal dispatch preparation inspected: the
// request as the inspection read it, and the kernel event the inspection
// accepted, which the staging runs again.
type dispatchProposal struct {
	request api.CollabRequest
	kernel  *requestKernel
	event   serverrequest.Prepare
}

// inspectDispatchProposal binds formal preparation to the exact material a
// collaborator proposed. A later card version needs a new proposal identity.
func (p *project) inspectDispatchProposal(tx *board.Transaction, task string, command api.DispatchCommand, h holder) (*dispatchProposal, error) {
	if command.Input == nil {
		return nil, nil
	}
	requests, err := p.readRequests(task)
	if err != nil {
		return nil, err
	}
	matches := proposalsOf(requests, command.Input.ID)
	if len(matches) == 0 {
		return nil, nil
	}
	match := matches[0]
	var claim claimRecord
	var cardRevision uint64
	if proposalUnique(len(matches)) {
		if claim, err = p.requireHolder(task, h); err != nil {
			return nil, err
		}
		snapshot, err := tx.Snapshot(task)
		if err != nil {
			return nil, err
		}
		cardRevision = snapshot.Revision
	}
	left, _ := json.Marshal(match.Dispatch)
	right, _ := json.Marshal(command.Input)
	frame, event := p.prepareEvent(match, len(matches), claim, cardRevision, string(left) == string(right))
	kernel := newRequestKernel(frame, &match)
	if !kernel.accepts(event) {
		return nil, p.proposalError(tx, task, command, match, claim, event, kernel.state)
	}
	return &dispatchProposal{request: match, kernel: kernel, event: event}, nil
}

// proposalUnique says that the proposals one dispatch ID names are exactly one,
// which the slot kernel decides.
func proposalUnique(proposals int) bool {
	return newProposalSlot(proposals).accepts(serverproposal.Lookup{})
}

// prepareEvent is the frame and the Prepare event the request kernel decides
// for match, the first of proposals requests that carry its dispatch ID, when the
// holder of claim prepares it against the card at cardRevision. The preparation
// and the guard of a plain move share it, so both ask the kernel the same
// question. material says that the dispatch the holder submits is the one the
// proposal froze. A slot that is not unique has no holder, generation, card, or
// window to compare, and the kernel refuses it whatever they are.
func (p *project) prepareEvent(match api.CollabRequest, proposals int, claim claimRecord, cardRevision uint64, material bool) (requestFrame, serverrequest.Prepare) {
	event := serverrequest.Prepare{Unique: proposalUnique(proposals)}
	if !event.Unique {
		return newRequestFrame(0, 0, &match), event
	}
	frame := newRequestFrame(claim.Generation, cardRevision, &match)
	event.Holder, event.Cgen, event.Card = true, frame.generation(claim.Generation), frame.cardRevision(cardRevision)
	event.Window = match.Dispatch.ConfirmBy.IsZero() || p.server.now().Before(match.Dispatch.ConfirmBy)
	event.Material = material
	return frame, event
}

// proposalError is the answer to a preparation the kernel refused, in the order
// the server has always checked.
func (p *project) proposalError(tx *board.Transaction, task string, command api.DispatchCommand, match api.CollabRequest, claim claimRecord, event serverrequest.Prepare, state serverrequest.State) error {
	switch {
	case !event.Unique:
		return apiError(api.CodeEvidenceIncomplete, "dispatch proposal identity is ambiguous")
	case match.TargetGeneration != claim.Generation || match.Status != api.RequestOpen && match.Status != api.RequestAccepted:
		return apiError(api.CodeLeaseInvalid, "dispatch proposal is no longer actionable by this execution")
	case !event.Window:
		return apiError(api.CodeEvidenceIncomplete, "dispatch proposal acceptance window has elapsed")
	case match.CardRevision != command.CardRevision:
		return apiError(api.CodeRevisionConflict, fmt.Sprintf("dispatch proposal names another card version: the proposal has card_revision %d, this request has card_revision %d. Next step: read the request with kander request show %s %s --json and the card with kander show --json %s. When the proposal still names the current card revision, prepare again at that revision; when the card moved on, the proposal is out of date, so ask the sender to propose again from the current card revision with a new dispatch ID", match.CardRevision, command.CardRevision, task, match.ID, task))
	case state.Prev != event.Card:
		if _, err := tx.Expect(task, "", match.CardRevision); err != nil {
			return err
		}
	}
	return apiError(api.CodeIdempotencyMismatch, "formal dispatch differs from the immutable proposed material")
}

// stagePreparedProposal accepts the inspected proposal: the kernel runs the
// event of the inspection again, so the proposal becomes accepted and its
// revision grows, also when it already was accepted.
func (p *project) stagePreparedProposal(tx *board.Transaction, proposal *dispatchProposal) error {
	if proposal == nil {
		return nil
	}
	if !proposal.kernel.step(proposal.event) {
		return apiError(api.CodeInternal, "dispatch proposal preparation was accepted by its inspection and refused by its staging")
	}
	request := proposal.request
	request.Status, request.Note, request.UpdatedAt = api.RequestAccepted, "holder prepared the proposed dispatch; business acceptance still requires its receipt", p.server.now()
	request.Revision++
	return stageJSON(tx, requestName(request.TaskID, request.ID), request)
}

// stageCompletedProposal settles the proposal a completion move fulfilled. The
// move into done releases the claim, so the holder can no longer answer the
// request itself; the completed dispatch of the same generation is the fact
// that settles it. An exact replay of that completion settles a request left
// accepted by an earlier server. Nothing else changes a request here.
func (p *project) stageCompletedProposal(tx *board.Transaction, task string, d board.Dispatch) error {
	if d.State != board.DispatchCompleted {
		return nil
	}
	claim, found, err := p.readClaim(task)
	if err != nil || !found {
		return err
	}
	requests, err := p.readRequests(task)
	if err != nil {
		return err
	}
	for _, request := range requests {
		frame := newRequestFrame(claim.Generation, 0, &request)
		match := request.Dispatch != nil && request.Dispatch.ID == d.Input.ID
		if !newRequestKernel(frame, &request).step(frame.completeEvent(found, match)) {
			continue
		}
		request.Status, request.Note, request.UpdatedAt = api.RequestDone, "the proposed dispatch completed with its task; settled by the completion receipt", p.server.now()
		request.Revision++
		if err := stageJSON(tx, requestName(task, request.ID), request); err != nil {
			return err
		}
	}
	return nil
}
