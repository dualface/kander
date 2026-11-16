package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/statemachine/serverclaim"
	"github.com/dualface/kander/internal/statemachine/serverdelivery"
	"github.com/dualface/kander/internal/statemachine/servergrouplease"
	"github.com/dualface/kander/internal/statemachine/serverrequest"
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
	out := p.groupWrite(key, "group-recover:"+input.Request.GroupID, input.Request.GroupID, input, h, func(extension board.TransactionExtension) (board.CoordinatorCheckpoint, error) {
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
