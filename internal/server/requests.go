package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/internal/statemachine/serverproposal"
)

// checkFixProposal is the evidence check of a new fix proposal. Only tests
// replace it, to reach the outcomes real evidence cannot produce on a test board.
var checkFixProposal = board.CheckDispatchFixProposal

var requestKinds = []string{api.RequestFix, api.RequestSync, api.RequestWrapUp, api.RequestReview, api.RequestStop, api.RequestNote}

func requestID(key string) string { return api.CollaborationRequestID(key) }

func requestName(task, id string) string { return filepath.Join("requests", task, id+".json") }
func requestDir(task string) string      { return servicePath("requests", task) }
func (p *project) readRequests(task string) ([]api.CollabRequest, error) {
	dir := filepath.Join(p.root, requestDir(task))
	exists, err := fs.DirectoryExists(p.root, dir)
	if err != nil || !exists {
		return []api.CollabRequest{}, err
	}
	entries, err := fs.ListDirectory(p.root, dir)
	if err != nil {
		return nil, err
	}
	out := []api.CollabRequest{}
	for _, entry := range entries {
		if entry.Kind != fs.KindFile || !strings.HasSuffix(entry.Name, ".json") {
			continue
		}
		var request api.CollabRequest
		if _, err := readJSON(p.root, filepath.Join(requestDir(task), entry.Name), &request); err != nil {
			return nil, err
		}
		out = append(out, request)
	}
	slices.SortFunc(out, func(a, b api.CollabRequest) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out, nil
}
func (p *project) requests(id string) ([]api.CollabRequest, error) {
	id, err := board.NormalizeTaskID(id)
	if err != nil {
		return nil, err
	}
	return p.readRequests(id)
}

// requestWrite shares generation validation, the request state and the exact
// operation receipt in one redo. It never advances the card's content version.
func (p *project) requestWrite(scope receiptScope, operation, id string, payload any, produce func(*board.Transaction) (api.CollabRequest, error), status int) outcome {
	return p.requestWriteScoped(scope, operation, id, payload, nil, produce, status)
}

// requestWriteScoped is requestWrite for a producer that reads more of the board
// than the request and its card. extend adds the groups and cards it needs; it
// runs only when the operation is not a replay of a stored receipt.
func (p *project) requestWriteScoped(scope receiptScope, operation, id string, payload any, extend func(board.LockScope) board.LockScope, produce func(*board.Transaction) (api.CollabRequest, error), status int) outcome {
	return p.idempotent(scope, operation, payload, func() outcome {
		var request api.CollabRequest
		lock := board.LockScope{Groups: []string{serviceGroup}, Tasks: []string{id}}
		if extend != nil {
			lock = extend(lock)
		}
		err := p.boardTransaction(lock, func(tx *board.Transaction) error {
			var err error
			request, err = produce(tx)
			if err != nil {
				return err
			}
			if err := stageJSON(tx, requestName(id, request.ID), request); err != nil {
				return err
			}
			return p.stageOperationReceiptStatus(tx, scope, operation, payload, status, request)
		})
		if err != nil {
			return outcome{err: err}
		}
		_ = p.events.append(api.Event{Kind: api.EventRequest, TaskID: id, RequestID: request.ID, Generation: request.TargetGeneration})
		return outcome{status: status, body: request}
	}, func() (outcome, bool) { return outcome{}, false })
}

// checkRequestShape refuses a request that is malformed or contradicts its own
// kind. Only the two kinds that have no older writers are checked against the
// dispatch: an older client still sends a wrap-up proposal as kind sync.
func checkRequestShape(input api.NewCollabRequest) error {
	switch {
	case !slices.Contains(requestKinds, input.Kind):
		return apiError(api.CodeInvalidRequest, fmt.Sprintf("unknown request kind %q", input.Kind))
	case !api.ValidIdentity(input.FromClient):
		return apiError(api.CodeInvalidRequest, "request needs a valid client identity")
	case strings.TrimSpace(input.Message) == "":
		return apiError(api.CodeInvalidRequest, "request needs a message")
	case len(input.Message) > api.MaxRequestMessage:
		return apiError(api.CodeInvalidRequest, "request message exceeds 64 KiB")
	case input.Kind == api.RequestNote && input.Dispatch != nil:
		return apiError(api.CodeInvalidRequest, "a note request carries no dispatch")
	case input.Kind == api.RequestWrapUp && (input.Dispatch == nil || input.Dispatch.Kind != "wrap-up"):
		return apiError(api.CodeInvalidRequest, "a wrap-up request needs a wrap-up dispatch")
	}
	return nil
}

func (p *project) createRequest(scope receiptScope, id string, input api.NewCollabRequest) outcome {
	if err := checkRequestShape(input); err != nil {
		return outcome{err: err}
	}
	if input.AcceptWithin != 0 && (input.Dispatch == nil || input.AcceptWithin <= 0 || input.AcceptWithin > api.DurationOf(24*time.Hour) || !input.Dispatch.CreatedAt.IsZero() || !input.Dispatch.ConfirmBy.IsZero()) {
		return outcome{err: apiError(api.CodeInvalidRequest, "relative acceptance window requires an unstamped dispatch and a duration of at most 24 hours")}
	}
	normalized, err := board.NormalizeTaskID(id)
	if err != nil {
		return outcome{err: err}
	}
	if err := p.lockBoard(); err != nil {
		return outcome{err: err}
	}
	defer p.unlockBoard()
	return p.requestWriteScoped(scope, "request:"+id, normalized, input, p.proposalScope(input), func(tx *board.Transaction) (api.CollabRequest, error) {
		var request api.CollabRequest
		task, err := tx.Snapshot(normalized)
		if err != nil {
			return request, err
		}
		frame := newRequestFrame(0, task.Revision, nil)
		if input.Dispatch != nil {
			if input.Dispatch.Message != input.Message {
				return request, apiError(api.CodeInvalidRequest, "dispatch proposal message differs from its visible request")
			}
			if !newRequestKernel(frame, nil).accepts(frame.bindEvent(input.Dispatch.TaskID == normalized, input.CardRevision)) {
				return request, apiError(api.CodeRevisionConflict, bindRefusal(input.Dispatch.TaskID, normalized, input.CardRevision, task.Revision))
			}
			raw, err := json.Marshal(input.Dispatch)
			if err != nil || len(raw) > 512<<10 {
				return request, apiError(api.CodeInvalidRequest, "dispatch request exceeds its evidence bound")
			}
		}
		claim, found, err := p.readClaim(normalized)
		if err != nil {
			return request, err
		}
		standing, err := p.standing(claim, found, input.TargetGeneration)
		if err != nil {
			return request, err
		}
		facts := claimFacts{held: claimedState(task.Entry.State), active: found && claim.Status == api.ClaimActive, standing: standing}
		frame = newRequestFrame(claim.Generation, task.Revision, nil)
		create := frame.createEvent(facts, input.TargetGeneration, input.Dispatch != nil, input.CardRevision)
		if !newRequestKernel(frame, nil).step(create) {
			if !facts.held || !facts.active {
				return request, apiError(api.CodeClaimRequired, "task has no holder to answer a request")
			}
			return request, apiError(api.CodeLeaseInvalid, "request target generation is not the claim's")
		}
		// The request ID derives from the key alone. A creation in another
		// receipt scope with the same key reaches this point without a receipt
		// and must never replace, nor be answered with, the request another
		// scope created, also when it repeats the dispatch of that request.
		newID := requestID(scope.key)
		if taken, err := p.requestIDTaken(newID); err != nil || taken {
			if err == nil {
				err = apiError(api.CodeIdempotencyMismatch, "idempotency key already created another request")
			}
			return request, err
		}
		if input.Dispatch != nil {
			previous, err := p.readRequests(normalized)
			if err != nil {
				return request, err
			}
			// The slot of the dispatch ID decides whether this creation takes it
			// or finds the first proposal that already has the ID.
			found := proposalsOf(previous, input.Dispatch.ID)
			if slot := newProposalSlot(len(found)); !slot.step(serverproposal.Admit{}) {
				if !slot.accepts(serverproposal.Repeat{}) {
					return request, apiError(api.CodeInternal, "dispatch proposal slot refused a creation")
				}
				old := found[0]
				same := old.FromClient == input.FromClient && old.AcceptWithin == input.AcceptWithin && sameRequestedDispatch(old, input)
				oldFrame := newRequestFrame(claim.Generation, task.Revision, &old)
				if !newRequestKernel(oldFrame, &old).accepts(oldFrame.recreateEvent(facts, input.TargetGeneration, same)) {
					return request, apiError(api.CodeIdempotencyMismatch, "dispatch ID already identifies another immutable proposal")
				}
				return old, nil
			}
			// The shell checks the evidence of a new fix proposal once the kernel
			// accepted the creation and the slot; a refusal writes nothing.
			if input.Dispatch.Kind == "fix" {
				if err := checkFixProposal(tx, *input.Dispatch); err != nil {
					return request, err
				}
			}
		}
		now := p.server.now()
		dispatch := input.Dispatch
		if dispatch != nil && input.AcceptWithin != 0 {
			copy := *dispatch
			copy.CreatedAt, copy.ConfirmBy = now, now.Add(input.AcceptWithin.Std())
			dispatch = &copy
		}
		return api.CollabRequest{AcceptWithin: input.AcceptWithin, Dispatch: dispatch, CardRevision: input.CardRevision, Revision: 1, ID: newID, TaskID: normalized, Kind: input.Kind, Message: input.Message, FromClient: input.FromClient, TargetGeneration: input.TargetGeneration, Status: api.RequestOpen, CreatedAt: now, UpdatedAt: now}, nil
	}, http.StatusCreated)
}

// requestIDTaken reports whether a request of any task already has id. Every
// request write holds the service group, which the caller holds too.
func (p *project) requestIDTaken(id string) (bool, error) {
	dir := filepath.Join(p.root, servicePath("requests"))
	exists, err := fs.DirectoryExists(p.root, dir)
	if err != nil || !exists {
		return false, err
	}
	entries, err := fs.ListDirectory(p.root, dir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.Kind != fs.KindDirectory {
			continue
		}
		if taken, err := fs.RegularFileExists(p.root, filepath.Join(p.root, requestDir(entry.Name), id+".json")); err != nil || taken {
			return taken, err
		}
	}
	return false, nil
}

// proposalScope widens the write scope of a new fix proposal to what the check
// of its evidence reads. Any other request keeps the scope of the request.
func (p *project) proposalScope(input api.NewCollabRequest) func(board.LockScope) board.LockScope {
	if input.Dispatch == nil || input.Dispatch.Kind != "fix" || input.Dispatch.Evidence.Fix == nil {
		return nil
	}
	return func(base board.LockScope) board.LockScope {
		read := board.FixProposalScope(p.board, input.Dispatch.Evidence.Fix)
		base.Groups = append(base.Groups, read.Groups...)
		base.Tasks = append(base.Tasks, read.Tasks...)
		slices.Sort(base.Tasks)
		base.Tasks = slices.Compact(base.Tasks)
		return base
	}
}
func (p *project) answerRequest(scope receiptScope, id, requestID string, answer api.CollabAnswer, h holder) outcome {
	if !slices.Contains([]string{api.RequestAccepted, api.RequestDone, api.RequestRejected}, answer.Status) || !api.ValidIdentity(requestID) {
		return outcome{err: apiError(api.CodeInvalidRequest, "invalid request answer")}
	}
	normalized, err := board.NormalizeTaskID(id)
	if err != nil {
		return outcome{err: err}
	}
	if err := p.lockBoard(); err != nil {
		return outcome{err: err}
	}
	defer p.unlockBoard()
	return p.requestWrite(scope, "answer:"+id+":"+requestID, normalized, answer, func(tx *board.Transaction) (api.CollabRequest, error) {
		var request api.CollabRequest
		claim, err := p.requireHolder(normalized, h)
		if err != nil {
			return request, err
		}
		data, found, err := tx.ReadGroup(serviceGroup, filepath.ToSlash(requestName(normalized, requestID)))
		if err != nil {
			return request, err
		}
		if !found {
			return request, apiError(api.CodeNotFound, "request does not exist")
		}
		if err := json.Unmarshal(data, &request); err != nil {
			return request, err
		}
		// The first acceptance of an open proposal also needs its acceptance
		// window and the card at the version it binds; the card is read only
		// once the checks before it passed.
		now := p.server.now()
		leaseBad := request.TargetGeneration != claim.Generation || request.Status == api.RequestSuperseded
		expectedBad := answer.ExpectedRevision != 0 && answer.ExpectedRevision != request.Revision
		firstAcceptance := request.Dispatch != nil && request.Status == api.RequestOpen && answer.Status == api.RequestAccepted
		window := request.Dispatch == nil || request.Dispatch.ConfirmBy.IsZero() || now.Before(request.Dispatch.ConfirmBy)
		card := request.CardRevision
		if firstAcceptance && !leaseBad && !expectedBad && window {
			snapshot, err := tx.Snapshot(normalized)
			if err != nil {
				return request, err
			}
			card = snapshot.Revision
		}
		frame := newRequestFrame(claim.Generation, card, &request)
		same, change := frame.answerEvents(answerFacts{answer: answer, request: request, window: window, claim: claim.Generation, cardRevision: card})
		k := newRequestKernel(frame, &request)
		switch {
		case k.step(same):
			return request, nil
		case k.step(change):
			request.Status, request.Note, request.UpdatedAt = answer.Status, answer.Note, now
			request.Revision++
			return request, nil
		case leaseBad:
			return request, apiError(api.CodeLeaseInvalid, "request targets another generation")
		case expectedBad:
			return request, apiError(api.CodeRevisionConflict, fmt.Sprintf("request revision changed: the answer has expected_revision %d, the request is at revision %d. Next step: read the request with kander request show %s %s --json, confirm that the answer still holds, and answer again", answer.ExpectedRevision, request.Revision, normalized, request.ID))
		case firstAcceptance && !window:
			return request, apiError(api.CodeEvidenceIncomplete, "dispatch proposal acceptance window has elapsed")
		case firstAcceptance && card != request.CardRevision:
			if _, err := tx.Expect(normalized, "", request.CardRevision); err != nil {
				return request, err
			}
		}
		return request, apiError(api.CodeRevisionConflict, "a settled request cannot change its answer")
	}, http.StatusOK)
}

// stageSupersedeRequests runs inside the execution handoff transaction, so no
// old request remains actionable after the successor claim becomes visible.
func (p *project) stageSupersedeRequests(tx *board.Transaction, task string, generation uint64) error {
	requests, err := p.readRequests(task)
	if err != nil {
		return err
	}
	for _, request := range requests {
		frame := newRequestFrame(generation, 0, &request)
		if !newRequestKernel(frame, &request).step(frame.supersedeEvent()) {
			continue
		}
		request.Status, request.UpdatedAt = api.RequestSuperseded, p.server.now()
		request.Revision++
		if err := stageJSON(tx, requestName(task, request.ID), request); err != nil {
			return err
		}
	}
	return nil
}

func sameRequestedDispatch(old api.CollabRequest, input api.NewCollabRequest) bool {
	stored := *old.Dispatch
	if input.AcceptWithin != 0 {
		stored.CreatedAt, stored.ConfirmBy = time.Time{}, time.Time{}
	}
	left, _ := json.Marshal(stored)
	right, _ := json.Marshal(input.Dispatch)
	return string(left) == string(right)
}

// bindRefusal is the text of a dispatch request that does not bind the card
// version it inspected: its proposal names another task than the request path, or
// its card_revision is not the card's current revision.
func bindRefusal(proposalTask, task string, named, current uint64) string {
	var parts []string
	if proposalTask != task {
		parts = append(parts, fmt.Sprintf("the proposal names task %s, the request path names task %s", board.RefusalTaskID(proposalTask), task))
	}
	if named != current {
		parts = append(parts, fmt.Sprintf("the request has card_revision %d, the card is at revision %d", named, current))
	}
	text := "dispatch request must bind the inspected card version"
	if len(parts) > 0 {
		text += ": " + strings.Join(parts, "; ")
	}
	return text + ". Next step: read the card with kander show --json " + task + " and propose again against its current revision"
}
