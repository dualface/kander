package server

import (
	"crypto/subtle"
	"math"
	"strconv"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/statemachine/serverclaim"
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

// claimStatusNames renders the status the kernel's shown query reports, which
// adds the view value lapsed to the stored statuses.
var claimStatusNames = map[serverclaim.Status]string{
	serverclaim.StatusPending: claimPending, serverclaim.StatusActive: api.ClaimActive,
	serverclaim.StatusLost: api.ClaimLost, serverclaim.StatusReleased: api.ClaimReleased,
	serverclaim.StatusLapsed: api.ClaimLapsed,
}

// claimKernel is the projected claim record of one task in one operation.
type claimKernel struct {
	state  serverclaim.State
	params serverclaim.Params
	// base shifts generations from 2^31 on into the kernel's uint32
	// parameters; 0 below, where the projection is the generation itself.
	base uint64
}

// projectClaim is the kernel state of a task's claim record: whether the card
// is in working or review, the record's status (none without a record), its
// generation, and whether the server clock is before its lease expiry.
func (p *project) projectClaim(r claimRecord, found, held bool) (*claimKernel, error) {
	return p.projectClaimAt(r, found, held, nil)
}

// projectClaimAt projects with the generation base of an operation. The kernel
// only compares generations and adds one, which a common shift keeps, so an
// operation fixes the base from the record before it (a generation from 2^31
// on projects to 1) and maps the record, the request's generations, and the
// result with it. The server still refuses only the overflow of the last
// uint64 generation.
func (p *project) projectClaimAt(r claimRecord, found, held bool, base *uint64) (*claimKernel, error) {
	k := &claimKernel{state: serverclaim.State{Held: held}}
	switch {
	case base != nil:
		k.base = *base
	case found && r.Generation >= 1<<31:
		k.base = r.Generation
	}
	if found {
		status, ok := claimStatuses[r.Status]
		if !ok {
			return nil, apiError(api.CodeLeaseInvalid, "claim record status "+strconv.Quote(r.Status)+" is unknown")
		}
		k.state.Status, k.state.Gen, k.state.Live = status, k.gen(r.Generation), p.server.now().Before(r.ExpiresAt)
	}
	if k.state.Gen >= math.MaxUint32 {
		return nil, apiError(api.CodeLeaseInvalid, "claim generation "+strconv.FormatUint(r.Generation, 10)+" is out of the operation's range")
	}
	// One above the generation, so a claim or a takeover can take the next one.
	k.params = serverclaim.Params{MaxGen: uint32(k.state.Gen + 1)}
	return k, nil
}

// gen maps a generation with the operation's base.
func (k *claimKernel) gen(g uint64) uint64 { return shiftGeneration(k.base, g) }

// shiftGeneration maps a generation with the base of an operation: unchanged
// without a base, else its distance from the base plus one, and 0 below the
// base.
func shiftGeneration(base, g uint64) uint64 {
	switch {
	case base == 0:
		return g
	case g < base:
		return 0
	}
	return g - base + 1
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
// in the order the server has always checked. A request that presents a token
// other than the current one with an older generation header was taken over,
// whatever the record's status is now: the server keeps only the current
// token hash, so this refusal does not authenticate the old token, and it
// grants nothing.
func (p *project) holderError(task string, r claimRecord, found bool, h holder) error {
	if found && takenOver(h, r.TokenHash, r.Generation) {
		return apiError(api.CodeTakenOver, "task "+task+" was taken over by generation "+strconv.FormatUint(r.Generation, 10)+"; generation "+strconv.FormatUint(h.generation, 10)+" may no longer write")
	}
	if !found || r.Status != api.ClaimActive {
		return apiError(api.CodeLeaseInvalid, "task "+task+" has no active claim; an explicit takeover is required")
	}
	if h.token == "" || subtle.ConstantTimeCompare([]byte(tokenHash(h.token)), []byte(r.TokenHash)) != 1 {
		return apiError(api.CodeHeldByOther, "task "+task+" is claimed by another execution")
	}
	return apiError(api.CodeLeaseInvalid, "claim generation "+strconv.FormatUint(h.generation, 10)+" is not current")
}

// takenOver reports that h presents a token other than the record's current
// one together with an older generation header: a newer generation took the
// lease over. The record keeps only the current token hash, so the old token
// is not authenticated; the answer is only a refusal.
func takenOver(h holder, hash string, generation uint64) bool {
	return h.token != "" && h.generation < generation && subtle.ConstantTimeCompare([]byte(tokenHash(h.token)), []byte(hash)) != 1
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

// standing reports whether the claim record of a task gives generation g an
// execution: the claim kernel's standing event, which ignores the lease clock.
// It checks another task's claim (a publication member, a review writer, a
// request target, an integration contributor or actor), not the requester's.
// The card state takes no part in the event, so the record is projected
// without it; callers that need the card in working or review check it
// themselves.
func (p *project) standing(r claimRecord, found bool, g uint64) (bool, error) {
	k, err := p.projectClaim(r, found, false)
	if err != nil {
		return false, err
	}
	kg := k.state.Gen + 1
	if found && g == r.Generation {
		kg = k.state.Gen
	}
	return k.step(serverclaim.Standing{G: kg}), nil
}

// hasStanding reports whether some generation has an execution on the claim
// record: the claim kernel's hasStanding query, projected like standing.
func (p *project) hasStanding(r claimRecord, found bool) (bool, error) {
	k, err := p.projectClaim(r, found, false)
	if err != nil {
		return false, err
	}
	return serverclaim.HasStanding(k.state, k.params), nil
}

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
