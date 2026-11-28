package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/internal/statemachine/serverclaim"
	"github.com/dualface/kander/v2/internal/statemachine/serverdelivery"
	"github.com/dualface/kander/v2/internal/statemachine/servergrouplease"
	"github.com/dualface/kander/v2/internal/statemachine/serverintegration"
	"github.com/dualface/kander/v2/internal/statemachine/serverproposal"
	"github.com/dualface/kander/v2/internal/statemachine/serverrequest"
)

// GroupLeaseState projects the lease record of a group of project id the way
// an operation does, with the generation base of the operation or the base the
// record fixes when base is nil. It returns the base it used.
func (s *Server) GroupLeaseState(id, group string, base *uint64) (servergrouplease.State, uint64, error) {
	p, err := s.project(id)
	if err != nil {
		return servergrouplease.State{}, 0, err
	}
	r, found, err := p.readGroupClaim(group)
	if err != nil {
		return servergrouplease.State{}, 0, err
	}
	k := p.projectGroup(r, found, base)
	return k.state, k.base, nil
}

// MemberClaimState projects the claim record of a claimed task the way a lost
// member recovery does.
func (s *Server) MemberClaimState(id, task string, base *uint64) (serverclaim.State, uint64, error) {
	p, err := s.project(id)
	if err != nil {
		return serverclaim.State{}, 0, err
	}
	r, found, err := p.readClaim(task)
	if err != nil {
		return serverclaim.State{}, 0, err
	}
	view, err := board.LoadBoard(p.board)
	if err != nil {
		return serverclaim.State{}, 0, err
	}
	entry, err := board.Locate(view, task)
	if err != nil {
		return serverclaim.State{}, 0, err
	}
	k, err := p.projectClaimAt(r, found, claimedState(entry.State), base)
	if err != nil {
		return serverclaim.State{}, 0, err
	}
	return k.state, k.base, nil
}

// SeedGroupLease writes an active lease record of a group at a generation and
// returns the bearer token of that generation.
func (s *Server) SeedGroupLease(id, group, client, execution string, generation uint64, expires time.Time) (string, error) {
	p, err := s.project(id)
	if err != nil {
		return "", err
	}
	token := s.token(p.info.ID, "group:"+group, execution, generation)
	r := claimRecord{TaskID: group, ClientID: client, ExecutionID: execution, Owner: "codex", Generation: generation, Status: api.ClaimActive, RenewedAt: expires.Add(-time.Minute), ExpiresAt: expires, TokenHash: tokenHash(token)}
	if err := p.lockBoard(); err != nil {
		return "", err
	}
	defer p.mu.Unlock()
	return token, board.WithTransaction(p.board, board.LockScope{Groups: []string{serviceGroup}}, func(tx *board.Transaction) error {
		return stageJSON(tx, groupClaimName(group), r)
	})
}

// RequestFrame is the frame of an operation on a request.
type RequestFrame = requestFrame

// NewRequestFrame is the frame an operation uses for a request of revision rev:
// gen is the generation it compares requests with, and card the card revision it
// compares proposals with. A request that does not exist has revision 0.
func NewRequestFrame(gen, card, rev uint64) RequestFrame {
	return newRequestFrame(gen, card, &api.CollabRequest{Revision: rev})
}

// RequestState projects the request id of a task the way an operation does,
// with the kernel parameters of the operation; a request that does not exist
// projects to the empty slot.
func (s *Server) RequestState(id, task, request string, frame RequestFrame) (serverrequest.State, serverrequest.Params, error) {
	p, err := s.project(id)
	if err != nil {
		return serverrequest.State{}, serverrequest.Params{}, err
	}
	requests, err := p.readRequests(task)
	if err != nil {
		return serverrequest.State{}, serverrequest.Params{}, err
	}
	for _, r := range requests {
		if r.ID == request {
			k := newRequestKernel(frame, &r)
			return k.state, k.params, nil
		}
	}
	k := newRequestKernel(frame, nil)
	return k.state, k.params, nil
}

// SeedRequest writes a request record of a task.
func (s *Server) SeedRequest(id string, request api.CollabRequest) error {
	p, err := s.project(id)
	if err != nil {
		return err
	}
	if err := p.lockBoard(); err != nil {
		return err
	}
	defer p.mu.Unlock()
	return board.WithTransaction(p.board, board.LockScope{Groups: []string{serviceGroup}, Tasks: []string{request.TaskID}}, func(tx *board.Transaction) error {
		return stageJSON(tx, requestName(request.TaskID, request.ID), request)
	})
}

// DeliveryState projects the record of a commit of a task the way an operation
// does, with the generation and the card revision it compares the record with,
// and the latest index: the stored index names this commit (here), another
// (other), or does not exist (none).
func (s *Server) DeliveryState(id, task, commit string, gen, card uint64) (serverdelivery.State, error) {
	p, err := s.project(id)
	if err != nil {
		return serverdelivery.State{}, err
	}
	frame := deliveryFrame{gen: gen, card: card}
	var record api.DeliveryEvidence
	found, err := readJSON(p.root, servicePath(deliveryPath(task, commit)), &record)
	if err != nil {
		return serverdelivery.State{}, err
	}
	var state serverdelivery.State
	if found {
		state = frame.project(&record)
	}
	var latest api.DeliveryEvidence
	indexed, err := readJSON(p.root, servicePath(deliveryPath(task, "latest")), &latest)
	switch {
	case err != nil:
		return serverdelivery.State{}, err
	case indexed && latest.Commit == commit:
		state.Latest = serverdelivery.LatestHere
	case indexed:
		state.Latest = serverdelivery.LatestOther
	}
	return state, nil
}

// DeliveryHistory counts the superseded versions kept for a task.
func (s *Server) DeliveryHistory(id, task string) (int, error) {
	p, err := s.project(id)
	if err != nil {
		return 0, err
	}
	dir := filepath.Join(p.root, servicePath("delivery-history", task))
	exists, err := fs.DirectoryExists(p.root, dir)
	if err != nil || !exists {
		return 0, err
	}
	entries, err := fs.ListDirectory(p.root, dir)
	return len(entries), err
}

// SeedDelivery writes the delivery record of a commit, without the checks of a
// publish.
func (s *Server) SeedDelivery(id string, value api.DeliveryEvidence) error {
	p, err := s.project(id)
	if err != nil {
		return err
	}
	if err := p.lockBoard(); err != nil {
		return err
	}
	defer p.mu.Unlock()
	return board.WithTransaction(p.board, board.LockScope{Groups: []string{serviceGroup}, Tasks: []string{value.TaskID}}, func(tx *board.Transaction) error {
		return stageJSON(tx, deliveryPath(value.TaskID, value.Commit), value)
	})
}

// GroupRecoverUnchecked runs an owned recovery of a group through groupWrite
// without the readiness check of the recover route, so that only the guard's
// check after the transaction decides whether the member cards it changed have
// their holder. group is the claim proof of the coordinator and members the
// proofs of the member cards.
func (s *Server) GroupRecoverUnchecked(ctx context.Context, id, key string, input api.GroupRecover, token string, generation uint64, members map[string]api.ClaimProof) error {
	p, err := s.project(id)
	if err != nil {
		return err
	}
	h := holder{token: token, generation: generation, members: members}
	out := p.groupWrite(receiptScope{key: key, holder: holderDigest(h)}, "group-recover:"+input.Request.GroupID, input.Request.GroupID, input, h, func(extension board.TransactionExtension) (board.CoordinatorCheckpoint, error) {
		return board.RecoverCoordinatorWithTransaction(ctx, p.board, input.Request, input.ObservedRevision, extension)
	})
	if out.err != nil {
		return out.err
	}
	// A refusal is recorded in the receipt of its key as the body of the reply.
	if out.status >= http.StatusBadRequest {
		var refusal api.Error
		raw, _ := out.body.(json.RawMessage)
		if err := json.Unmarshal(raw, &refusal); err != nil {
			return err
		}
		return &refusal
	}
	return nil
}

// ProposalSlot projects the slot of a dispatch ID in a task the way an
// operation does.
func (s *Server) ProposalSlot(id, task, dispatchID string) (serverproposal.State, serverproposal.Params, error) {
	p, err := s.project(id)
	if err != nil {
		return serverproposal.State{}, serverproposal.Params{}, err
	}
	requests, err := p.readRequests(task)
	if err != nil {
		return serverproposal.State{}, serverproposal.Params{}, err
	}
	return newProposalSlot(len(proposalsOf(requests, dispatchID))).state, proposalSlotParams, nil
}

// IntegrationState projects the integration record of target integration of
// project id the way an operation does, with the bases of the operation or the
// bases the record fixes when bases is nil. It returns the bases it used.
func (s *Server) IntegrationState(id, integration string, bases *IntegrationBases) (serverintegration.State, IntegrationBases, error) {
	p, err := s.project(id)
	if err != nil {
		return serverintegration.State{}, IntegrationBases{}, err
	}
	record, found, err := p.readIntegration(integration)
	if err != nil {
		return serverintegration.State{}, IntegrationBases{}, err
	}
	k, err := p.projectIntegration(record.Value, found, bases)
	if err != nil {
		return serverintegration.State{}, IntegrationBases{}, err
	}
	return k.state, k.bases, nil
}

// ReceiptState projects the published receipt of one generation of a task, or
// the empty state when the generation has none.
func (s *Server) ReceiptState(id, task string, generation uint64) (serverintegration.State, error) {
	p, err := s.project(id)
	if err != nil {
		return serverintegration.State{}, err
	}
	var value api.Integration
	found, err := readJSON(p.root, taskIntegrationName(task, generation), &value)
	if err != nil {
		return serverintegration.State{}, err
	}
	k, err := p.projectIntegration(value, found, nil)
	if err != nil {
		return serverintegration.State{}, err
	}
	return k.state, nil
}

// ExecutionState projects the published execution of an integration the way a
// wrap-up check does.
func (s *Server) ExecutionState(id, integration, execution string) (serverintegration.State, error) {
	p, err := s.project(id)
	if err != nil {
		return serverintegration.State{}, err
	}
	value, err := p.integrationExecution(integration, execution)
	if err != nil {
		return serverintegration.State{}, err
	}
	k, err := p.projectIntegration(value, true, nil)
	if err != nil {
		return serverintegration.State{}, err
	}
	return k.state, nil
}

// MutateIntegration rewrites the stored record of an integration, keeping its
// token hash, to reach counters the operations cannot reach in a test.
func (s *Server) MutateIntegration(id, integration string, mutate func(*api.Integration)) error {
	p, err := s.project(id)
	if err != nil {
		return err
	}
	if err := p.lockBoard(); err != nil {
		return err
	}
	defer p.mu.Unlock()
	record, found, err := p.readIntegration(integration)
	if err != nil || !found {
		return err
	}
	mutate(&record.Value)
	return board.WithTransaction(p.board, board.LockScope{Groups: []string{serviceGroup}}, func(tx *board.Transaction) error {
		return stageJSON(tx, integrationName(integration), record)
	})
}

// CompletionEvidence runs the evidence check of a move of task into done inside
// a transaction with the scope the move holds.
func (s *Server) CompletionEvidence(id, task string, generation uint64) (api.Integration, error) {
	p, err := s.project(id)
	if err != nil {
		return api.Integration{}, err
	}
	var value api.Integration
	err = p.evidence(task, func(tx *board.Transaction) error {
		var err error
		value, err = p.completionIntegration(tx, task, generation)
		return err
	})
	return value, err
}

// WrapUpEvidence runs the integration evidence check of a wrap-up dispatch of
// task inside a transaction with the scope the dispatch holds.
func (s *Server) WrapUpEvidence(id, task string, generation uint64, g board.DispatchIntegration) error {
	p, err := s.project(id)
	if err != nil {
		return err
	}
	return p.evidence(task, func(tx *board.Transaction) error { return p.wrapUpIntegration(tx, task, generation, g) })
}

// IntegrationFacts runs the member checks of an integration request inside a
// transaction over its members, without the actor and ticket checks that
// come first on the API.
func (s *Server) IntegrationFacts(id string, input api.IntegrationRequest) error {
	p, err := s.project(id)
	if err != nil {
		return err
	}
	if err := p.lockBoard(); err != nil {
		return err
	}
	defer p.mu.Unlock()
	var ids []string
	for _, member := range input.Members {
		ids = append(ids, member.TaskID)
	}
	scope := board.IntegrationScope(ids, input.GroupID)
	scope.Groups = append(scope.Groups, serviceGroup)
	return board.WithTransaction(p.board, scope, func(tx *board.Transaction) error { return p.integrationFacts(tx, input, true) })
}

func (p *project) evidence(task string, fn func(*board.Transaction) error) error {
	if err := p.lockBoard(); err != nil {
		return err
	}
	defer p.mu.Unlock()
	scope := board.IntegrationScope([]string{task}, "")
	scope.Groups = append(scope.Groups, serviceGroup)
	return board.WithTransaction(p.board, scope, fn)
}

// SetFixProposalCheck replaces the evidence check of a new fix proposal and
// returns the function that restores it.
func SetFixProposalCheck(check func(*board.Transaction, board.DispatchInput) error) (restore func()) {
	previous := checkFixProposal
	checkFixProposal = check
	return func() { checkFixProposal = previous }
}

// BoardRoot is the board directory of project id.
func (s *Server) BoardRoot(id string) (string, error) {
	p, err := s.project(id)
	if err != nil {
		return "", err
	}
	return p.board, nil
}

// OpenDispatchRefusal runs the open-dispatch condition of the coordinator
// review write on a card text, reading the dispatch record through read.
func OpenDispatchRefusal(task, text string, read func(task, id string) (board.Dispatch, error)) error {
	return openDispatch(task, text, read)
}

// SweepStartOrders runs one start order sweep at the server clock.
func (s *Server) SweepStartOrders() { s.sweepStartOrders() }
