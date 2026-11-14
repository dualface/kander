package server

import (
	"crypto/subtle"
	"strconv"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/statemachine/serverclaim"
)

// The claim record decisions are made by the generated kernel in
// internal/statemachine/serverclaim
// (formal/KanderFormal/Machines/ServerClaim.lean). This file projects a claim
// record into a kernel state, maps a request's claim headers to kernel
// arguments, and selects the error of a refused request. See
// docs/formal-verification.md.

var claimStatuses = map[string]serverclaim.Status{
	claimPending: serverclaim.StatusPending, api.ClaimActive: serverclaim.StatusActive,
	api.ClaimLost: serverclaim.StatusLost, api.ClaimReleased: serverclaim.StatusReleased,
}

// claimKernel is the projected claim record of one task in one operation.
type claimKernel struct {
	state  serverclaim.State
	params serverclaim.Params
}

// projectClaim is the kernel state of a task's claim record: whether the card
// is in working or review, the record's status (none without a record), its
// generation, and whether the server clock is before its lease expiry.
func (p *project) projectClaim(r claimRecord, found, held bool) (*claimKernel, error) {
	k := &claimKernel{state: serverclaim.State{Held: held}}
	if found {
		status, ok := claimStatuses[r.Status]
		if !ok {
			return nil, apiError(api.CodeLeaseInvalid, "claim record status "+strconv.Quote(r.Status)+" is unknown")
		}
		k.state.Status, k.state.Gen, k.state.Live = status, kernelGeneration(r.Generation), p.server.now().Before(r.ExpiresAt)
	}
	// One above the generation, so a claim or a takeover can take the next one.
	k.params = serverclaim.Params{MaxGen: uint32(k.state.Gen + 1)}
	return k, nil
}

// kernelGeneration keeps a generation within the kernel's uint32 parameters.
// The kernel only compares a generation with the record's and adds one, and the
// headers map to the record's projected generation or the one after it, so
// every generation from 2^31 on projects to 1 without changing a decision. The
// server still refuses only the overflow of the last uint64 generation.
func kernelGeneration(gen uint64) uint64 {
	if gen < 1<<31 {
		return gen
	}
	return 1
}

func (k *claimKernel) step(e serverclaim.Event) bool {
	next, ok := serverclaim.Step(k.state, e, k.params)
	if ok {
		k.state = next
	}
	return ok
}

// headers maps the claim headers of h to kernel arguments: the generation
// header, and the generation the token was derived for. A header or token of
// any other generation maps to the generation after the record's, which the
// kernel tells apart from the current one exactly as the server does.
func (k *claimKernel) headers(r claimRecord, found bool, h holder) (g, tg uint64) {
	other := k.state.Gen + 1
	g, tg = other, other
	if found && h.generation == r.Generation {
		g = k.state.Gen
	}
	if found && h.token != "" && subtle.ConstantTimeCompare([]byte(tokenHash(h.token)), []byte(r.TokenHash)) == 1 {
		tg = k.state.Gen
	}
	return g, tg
}

// holderError is the answer of requireHolder to a request the kernel refused,
// in the order the server has always checked.
func (p *project) holderError(task string, r claimRecord, found bool, h holder) error {
	if !found || r.Status != api.ClaimActive {
		return apiError(api.CodeLeaseInvalid, "task "+task+" has no active claim; an explicit takeover is required")
	}
	if h.token == "" || subtle.ConstantTimeCompare([]byte(tokenHash(h.token)), []byte(r.TokenHash)) != 1 {
		return apiError(api.CodeHeldByOther, "task "+task+" is claimed by another execution")
	}
	if h.generation != r.Generation {
		return apiError(api.CodeLeaseInvalid, "claim generation "+strconv.FormatUint(h.generation, 10)+" is not current")
	}
	return apiError(api.CodeLeaseInvalid, "the lease of task "+task+" expired; an explicit takeover is required")
}

// forTask is the claim h presents for task: its own, or the task's proof of a
// multi-task claim.
func (h holder) forTask(task string) (holder, error) {
	if h.invalid != nil {
		return h, h.invalid
	}
	if h.members != nil {
		proof, found := h.members[task]
		if !found {
			return h, apiError(api.CodeHeldByOther, "a separate claim proof is required for task "+task)
		}
		h.token, h.generation = proof.Token, proof.Generation
	}
	return h, nil
}

// holderEvent builds the kernel event of a request from its claim headers.
type holderEvent func(g, tg uint64) serverclaim.Event

func writeEvent(terminate bool) holderEvent {
	return func(g, tg uint64) serverclaim.Event { return serverclaim.Write{G: g, Tg: tg, Terminate: terminate} }
}

func leaveEvent(terminate bool) holderEvent {
	return func(g, tg uint64) serverclaim.Event { return serverclaim.Leave{G: g, Tg: tg, Terminate: terminate} }
}

func renewEvent(g, tg uint64) serverclaim.Event    { return serverclaim.Renew{G: g, Tg: tg} }
func releaseEvent(g, tg uint64) serverclaim.Event  { return serverclaim.Release{G: g, Tg: tg} }
func rollbackEvent(g, tg uint64) serverclaim.Event { return serverclaim.Rollback{G: g, Tg: tg} }

// holderDecision runs one request that presents h against the claim record of
// task, a card in working or review when held. It returns the record and
// whether one exists; a refused request returns the error requireHolder has
// always answered.
func (p *project) holderDecision(task string, h holder, held bool, event holderEvent) (claimRecord, bool, error) {
	h, err := h.forTask(task)
	if err != nil {
		return claimRecord{}, false, err
	}
	r, found, err := p.readClaim(task)
	if err != nil {
		return r, found, err
	}
	k, err := p.projectClaim(r, found, held)
	if err != nil {
		return r, found, err
	}
	if !k.step(event(k.headers(r, found, h))) {
		return r, found, p.holderError(task, r, found, h)
	}
	return r, found, nil
}
