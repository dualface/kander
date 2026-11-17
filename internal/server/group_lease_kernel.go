package server

import (
	"crypto/subtle"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/statemachine/serverclaim"
	"github.com/dualface/kander/internal/statemachine/servergrouplease"
)

// The group lease decisions are made by the generated kernel in
// internal/statemachine/servergrouplease
// (formal/KanderFormal/Machines/ServerGroupLease.lean). This file projects a
// group lease record into a kernel state, maps a request's claim headers to
// kernel arguments, and selects the error of a refused request. The member
// claim of a lost member recovery goes through the `audit` event of the claim
// kernel (claim_kernel.go). See docs/formal-verification.md.

var groupStatuses = map[string]servergrouplease.Status{
	api.ClaimActive: servergrouplease.StatusActive, api.ClaimLost: servergrouplease.StatusLost,
	api.ClaimReleased: servergrouplease.StatusReleased,
}

var groupStatusNames = map[servergrouplease.Status]string{
	servergrouplease.StatusActive: api.ClaimActive, servergrouplease.StatusLost: api.ClaimLost,
	servergrouplease.StatusReleased: api.ClaimReleased, servergrouplease.StatusLapsed: api.ClaimLapsed,
}

// groupKernel is the projected lease record of one group in one operation.
type groupKernel struct {
	state  servergrouplease.State
	params servergrouplease.Params
	// base shifts generations from 2^31 on into the kernel's uint32
	// parameters, as claimKernel does; 0 below.
	base uint64
}

// projectGroup is the kernel state of a group's lease record: its status (none
// without a record; a status the server never writes counts as an expired
// lease), its generation, and whether the server clock is before its expiry.
// base is the generation base of an operation, or nil to fix it from the
// record.
func (p *project) projectGroup(r claimRecord, found bool, base *uint64) *groupKernel {
	k := &groupKernel{}
	switch {
	case base != nil:
		k.base = *base
	case found && r.Generation >= 1<<31:
		k.base = r.Generation
	}
	if found {
		status, known := groupStatuses[r.Status]
		if !known {
			status = servergrouplease.StatusLost
		}
		k.state = servergrouplease.State{Status: status, Gen: shiftGeneration(k.base, r.Generation), Live: p.server.now().Before(r.ExpiresAt)}
	}
	// One above the generation, so a claim can take the next one.
	k.params = servergrouplease.Params{MaxGen: uint32(k.state.Gen + 1)}
	return k
}

func (k *groupKernel) step(e servergrouplease.Event) bool {
	next, ok := servergrouplease.Step(k.state, e, k.params)
	if ok {
		k.state = next
	}
	return ok
}

// accepts asks the kernel whether it would accept e, and keeps the state.
func (k *groupKernel) accepts(e servergrouplease.Event) bool {
	_, ok := servergrouplease.Step(k.state, e, k.params)
	return ok
}

// valid is the kernel's query for a lease that is active and has not expired.
func (k *groupKernel) valid() bool { return servergrouplease.Valid(k.state, k.params) }

// groupStanding reports whether the lease record of a group gives generation g
// an execution: the group kernel's standing event, which ignores the lease
// clock (the integration actor check of a group).
func (p *project) groupStanding(r claimRecord, found bool, g uint64) bool {
	k := p.projectGroup(r, found, nil)
	kg := k.state.Gen + 1
	if found && g == r.Generation {
		kg = k.state.Gen
	}
	return k.accepts(servergrouplease.Standing{G: kg})
}

// observed maps a generation a request names: the record's own maps to the
// kernel generation, any other to the generation after it, which the kernel
// tells apart from the current one exactly as the server does.
func (k *groupKernel) observed(named, record uint64) uint64 {
	if named == record {
		return k.state.Gen
	}
	return k.state.Gen + 1
}

// groupEvent builds the kernel event of a request from its claim headers: the
// headers parsed, the generation header, and the generation the token was
// derived for.
type groupEvent func(hdr bool, g, tg uint64) servergrouplease.Event

func requireGroupEvent(hdr bool, g, tg uint64) servergrouplease.Event {
	return servergrouplease.Require{Hdr: hdr, G: g, Tg: tg}
}

func renewGroupEvent(hdr bool, g, tg uint64) servergrouplease.Event {
	return servergrouplease.Renew{Hdr: hdr, G: g, Tg: tg}
}

func releaseGroupEvent(hdr bool, g, tg uint64) servergrouplease.Event {
	return servergrouplease.Release{Hdr: hdr, G: g, Tg: tg}
}

func observeGroupEvent(stopped bool) groupEvent {
	return func(hdr bool, g, tg uint64) servergrouplease.Event {
		return servergrouplease.Observe{Hdr: hdr, G: g, Tg: tg, Stopped: stopped}
	}
}

// writeAfterEvent is the check after a group write: members says that every
// member card the transaction changed has its holder.
func writeAfterEvent(members bool) groupEvent {
	return func(hdr bool, g, tg uint64) servergrouplease.Event {
		return servergrouplease.WriteAfter{Hdr: hdr, G: g, Tg: tg, Members: members}
	}
}

// groupLease is the lease record of a group, projected, with the claim headers
// a request presents against it.
type groupLease struct {
	record claimRecord
	found  bool
	kernel *groupKernel
	h      holder
}

// leaseOf projects an already read record.
func (p *project) leaseOf(r claimRecord, found bool, h holder) *groupLease {
	return &groupLease{record: r, found: found, kernel: p.projectGroup(r, found, nil), h: h}
}

// groupLease reads and projects the lease record of group.
func (p *project) groupLease(group string, h holder) (*groupLease, error) {
	r, found, err := p.readGroupClaim(group)
	if err != nil {
		return nil, err
	}
	return p.leaseOf(r, found, h), nil
}

// event maps the request headers against the record and builds the kernel
// event. A header or token of any other generation maps to the generation after
// the record's.
func (c *groupLease) event(build groupEvent) servergrouplease.Event {
	other := c.kernel.state.Gen + 1
	g, tg := other, other
	if c.found && c.h.generation == c.record.Generation {
		g = c.kernel.state.Gen
	}
	if c.found && c.h.token != "" && subtle.ConstantTimeCompare([]byte(tokenHash(c.h.token)), []byte(c.record.TokenHash)) == 1 {
		tg = c.kernel.state.Gen
	}
	return build(c.h.invalid == nil, g, tg)
}

func (c *groupLease) step(build groupEvent) bool    { return c.kernel.step(c.event(build)) }
func (c *groupLease) accepts(build groupEvent) bool { return c.kernel.accepts(c.event(build)) }

// holderError is the answer of requireGroup to a request the kernel refused,
// in the order the server has always checked: the headers, then whether a
// newer generation took the group over, then the record, then the token. As
// for the single card claim, the taken-over answer does not authenticate the
// old token and grants nothing.
func (c *groupLease) holderError() error {
	if c.h.invalid != nil {
		return c.h.invalid
	}
	if c.found && takenOver(c.h, c.record.TokenHash, c.record.Generation) {
		return apiError(api.CodeTakenOver, "group was taken over by coordinator generation "+strconv.FormatUint(c.record.Generation, 10))
	}
	if !c.found || c.record.Status != api.ClaimActive || c.record.Generation != c.h.generation {
		return apiError(api.CodeLeaseInvalid, "coordinator lease is not current")
	}
	return apiError(api.CodeHeldByOther, "group is held by another coordinator")
}

// claimError is the answer to a group claim the kernel refused, in the order
// the server has always checked. activeMessage words the refusal of a takeover
// over a valid lease.
func (c *groupLease) claimError(sameExec, sameClient bool, input api.GroupClaim, activeMessage string) error {
	valid := c.kernel.valid()
	switch {
	case sameExec && !(sameClient && c.record.Status == api.ClaimActive):
		return apiError(api.CodeLeaseInvalid, "a lost coordinator execution cannot reuse its identity")
	case sameExec:
		return c.holderError()
	case valid:
		return apiError(api.CodeLeaseActive, activeMessage)
	case c.record.Generation != input.ExpectedGeneration || c.record.Generation == ^uint64(0):
		return apiError(api.CodeLeaseInvalid, "the observed coordinator generation is not current")
	}
	return apiError(api.CodeInvalidRequest, "coordinator takeover needs an explicit decision reference")
}

// groupClaim runs the claim of client against a read lease record: a new
// record, a takeover, or the reuse of the client's own valid lease. It returns
// whether the claim reuses the lease, or the error of a refused claim.
func (p *project) groupClaim(previous claimRecord, found bool, client string, input api.GroupClaim, h holder, activeMessage string) (bool, error) {
	c := p.leaseOf(previous, found, h)
	sameExec, sameClient := found && previous.ExecutionID == input.ExecutionID, found && previous.ClientID == client
	claim := func(hdr bool, g, tg uint64) servergrouplease.Event {
		return servergrouplease.Claim{
			Hdr: hdr, G: g, Tg: tg, SameExec: sameExec, SameClient: sameClient,
			Expected: c.kernel.observed(input.ExpectedGeneration, previous.Generation),
			Room:     previous.Generation != ^uint64(0), Decision: strings.TrimSpace(input.Decision) != "",
		}
	}
	if !c.step(claim) {
		return false, c.claimError(sameExec, sameClient, input, activeMessage)
	}
	return sameExec, nil
}

// groupClaimView renders the public claim of a group lease with the status the
// kernel shows: an active record whose lease expired is lost.
func (p *project) groupClaimView(r claimRecord) *api.ClaimView {
	view := r.view(p.server.now())
	if _, known := groupStatuses[r.Status]; known {
		k := p.projectGroup(r, true, nil)
		view.Status = groupStatusNames[servergrouplease.Shown(k.state, k.params)]
	}
	return view
}

// auditEvent builds the claim kernel event of a lost member recovery: the
// request observed generation expected of the member's claim, and names a fresh
// execution identity below the last generation.
func (k *claimKernel) auditEvent(r claimRecord, input api.RecoveryLease) serverclaim.Event {
	return serverclaim.Audit{
		Expected: k.observed(input.ExpectedGeneration, r.Generation),
		Fresh:    r.Generation != ^uint64(0) && r.ExecutionID != input.ExecutionID,
	}
}

// observed maps a generation a request names like groupKernel.observed.
func (k *claimKernel) observed(named, record uint64) uint64 {
	if named == record {
		return k.state.Gen
	}
	return k.state.Gen + 1
}
