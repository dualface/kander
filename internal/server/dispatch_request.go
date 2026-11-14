package server

import (
	"encoding/json"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
)

// inspectDispatchProposal binds formal preparation to the exact material a
// collaborator proposed. A later card version needs a new proposal identity.
func (p *project) inspectDispatchProposal(tx *board.Transaction, task string, command api.DispatchCommand, h holder) (*api.CollabRequest, error) {
	if command.Input == nil {
		return nil, nil
	}
	requests, err := p.readRequests(task)
	if err != nil {
		return nil, err
	}
	var match *api.CollabRequest
	for _, request := range requests {
		if request.Dispatch == nil || request.Dispatch.ID != command.Input.ID {
			continue
		}
		if match != nil {
			return nil, apiError(api.CodeEvidenceIncomplete, "dispatch proposal identity is ambiguous")
		}
		copy := request
		match = &copy
	}
	if match == nil {
		return nil, nil
	}
	claim, err := p.requireHolder(task, h)
	if err != nil {
		return nil, err
	}
	if match.TargetGeneration != claim.Generation || match.Status != api.RequestOpen && match.Status != api.RequestAccepted {
		return nil, apiError(api.CodeLeaseInvalid, "dispatch proposal is no longer actionable by this execution")
	}
	if !match.Dispatch.ConfirmBy.IsZero() && !p.server.now().Before(match.Dispatch.ConfirmBy) {
		return nil, apiError(api.CodeEvidenceIncomplete, "dispatch proposal acceptance window has elapsed")
	}
	if match.CardRevision != command.CardRevision {
		return nil, apiError(api.CodeRevisionConflict, "dispatch proposal names another card version")
	}
	if _, err := tx.Expect(task, "", match.CardRevision); err != nil {
		return nil, err
	}
	left, _ := json.Marshal(match.Dispatch)
	right, _ := json.Marshal(command.Input)
	if string(left) != string(right) {
		return nil, apiError(api.CodeIdempotencyMismatch, "formal dispatch differs from the immutable proposed material")
	}
	return match, nil
}

func (p *project) stagePreparedProposal(tx *board.Transaction, request *api.CollabRequest) error {
	if request == nil {
		return nil
	}
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
		if request.Dispatch == nil || request.Dispatch.ID != d.Input.ID || request.Status != api.RequestAccepted || request.TargetGeneration != claim.Generation {
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
