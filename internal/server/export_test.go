package server

import (
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/statemachine/serverclaim"
	"github.com/dualface/kander/internal/statemachine/servergrouplease"
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
