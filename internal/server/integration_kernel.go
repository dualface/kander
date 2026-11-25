package server

import (
	"crypto/subtle"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/statemachine/serverintegration"
)

// The integration decisions are made by the generated kernel in
// internal/statemachine/serverintegration
// (formal/KanderFormal/Machines/ServerIntegration.lean). This file projects an
// integration record or a published receipt into a kernel state, maps the
// facts of a request to kernel arguments, and selects the error of a refused
// request in the order the server has always checked. See
// docs/formal-verification.md.

var integrationStatuses = map[string]serverintegration.Status{
	api.ClaimActive: serverintegration.StatusActive, api.ClaimLost: serverintegration.StatusLost,
	api.ClaimReleased: serverintegration.StatusReleased,
}

var integrationStatusNames = map[serverintegration.Status]string{
	serverintegration.StatusActive: api.ClaimActive, serverintegration.StatusLost: api.ClaimLost,
	serverintegration.StatusReleased: api.ClaimReleased, serverintegration.StatusLapsed: api.ClaimLapsed,
}

var integrationPhases = map[string]serverintegration.Phase{
	"reserved": serverintegration.PhaseReserved, "pushing": serverintegration.PhasePushing,
	"integrated": serverintegration.PhaseIntegrated, "released": serverintegration.PhaseReleased,
	"uncertain": serverintegration.PhaseUncertain,
}

var integrationPurposes = map[string]serverintegration.Purpose{
	"initialize": serverintegration.PurposeInitialize, "assemble": serverintegration.PurposeAssemble,
	"deliver": serverintegration.PurposeDeliver,
}

// authorityMap maps the generation of an integration actor's lease into the
// kernel range. The kernel only compares authority generations, so the map is
// injective for the two values an operation looks at: the record's own
// authority generation, and any other value, which maps to the value after it.
type authorityMap struct{ ref, state uint64 }

func (m authorityMap) of(g uint64) uint64 {
	if g == m.ref {
		return m.state
	}
	return m.state + 1
}

// IntegrationBases fixes the maps of an operation: the generation and revision
// bases and the authority map. The kernel only compares and adds one, which a
// common shift keeps, so an operation fixes them from the record before it and
// maps the record, the request's values, and the result with them.
type IntegrationBases struct {
	gen, rev uint64
	auth     authorityMap
}

// integrationKernel is the projected integration record, or receipt, of one
// target in one operation.
type integrationKernel struct {
	state  serverintegration.State
	params serverintegration.Params
	bases  IntegrationBases
}

// projectIntegration is the kernel state of an integration record: the status
// (none without a record; a status the server never writes counts as an expired
// lease), the generation and revision, whether the server clock is before the
// lease expiry, the phase, the purpose, whether the request declares the source
// contained, the authority generation, and whether the observed target is a
// commit SHA. bases is the maps of an operation, or nil to fix them from the
// record.
func (p *project) projectIntegration(v api.Integration, found bool, bases *IntegrationBases) (*integrationKernel, error) {
	k := &integrationKernel{}
	switch {
	case bases != nil:
		k.bases = *bases
	case found:
		if v.Generation >= 1<<31 {
			k.bases.gen = v.Generation
		}
		if v.Revision >= 1<<31 {
			k.bases.rev = v.Revision
		}
		k.bases.auth.ref = v.AuthorityGeneration
		k.bases.auth.state = v.AuthorityGeneration
		if v.AuthorityGeneration >= 1<<31 {
			k.bases.auth.state = 1
		}
	}
	if found {
		status, known := integrationStatuses[v.Status]
		if !known {
			status = serverintegration.StatusLost
		}
		phase, known := integrationPhases[v.Phase]
		if !known {
			phase = serverintegration.PhaseUncertain
		}
		k.state = serverintegration.State{
			Status: status, Gen: shiftGeneration(k.bases.gen, v.Generation), Rev: shiftGeneration(k.bases.rev, v.Revision),
			Live: p.server.now().Before(v.ExpiresAt), Phase: phase, Purpose: integrationPurposes[v.Request.Purpose],
			Contained: v.Request.AlreadyContained, Auth: k.bases.auth.of(v.AuthorityGeneration), Obs: api.CommitSHA(v.ObservedTarget),
		}
	}
	if k.state.Gen >= math.MaxUint32 || k.state.Rev >= math.MaxUint32 {
		return nil, apiError(api.CodeLeaseInvalid, "integration generation "+strconv.FormatUint(v.Generation, 10)+" or revision "+strconv.FormatUint(v.Revision, 10)+" is out of the operation's range")
	}
	// One above each counter, so a claim can take the next one and an action the
	// next revision.
	k.params = serverintegration.Params{MaxGen: uint32(k.state.Gen + 1), MaxRev: uint32(k.state.Rev + 1), MaxAuth: uint32(k.state.Auth + 1)}
	return k, nil
}

func (k *integrationKernel) step(e serverintegration.Event) bool {
	next, ok := serverintegration.Step(k.state, e, k.params)
	if ok {
		k.state = next
	}
	return ok
}

// accepts asks the kernel whether it would accept e, and keeps the state.
func (k *integrationKernel) accepts(e serverintegration.Event) bool {
	_, ok := serverintegration.Step(k.state, e, k.params)
	return ok
}

// valid is the kernel's query for a lease that is active and has not expired.
func (k *integrationKernel) valid() bool { return serverintegration.Valid(k.state, k.params) }

// generation maps a generation a request names: the record's own maps to the
// kernel generation, zero stays zero, any other to the generation after it.
func (k *integrationKernel) generation(named, record uint64) uint64 {
	switch named {
	case record:
		return k.state.Gen
	case 0:
		return 0
	}
	return k.state.Gen + 1
}

// revision maps a revision a request names like generation.
func (k *integrationKernel) revision(named, record uint64) uint64 {
	switch named {
	case record:
		return k.state.Rev
	case 0:
		return 0
	}
	return k.state.Rev + 1
}

// integrationShown renders the status of a record the kernel shows: an active
// record whose lease expired is lapsed.
func (p *project) integrationShown(v api.Integration) (api.Integration, error) {
	if _, known := integrationStatuses[v.Status]; !known {
		return v, nil
	}
	k, err := p.projectIntegration(v, true, nil)
	if err != nil {
		return v, err
	}
	v.Status = integrationStatusNames[serverintegration.Shown(k.state, k.params)]
	return v, nil
}

// actorFacts are the facts of the integration actor's current lease: the claim
// of the single card or the group. standing is the standing event of the
// actor's own kernel for its current generation, which ignores the lease clock.
type actorFacts struct {
	standing   bool
	generation uint64
	exec       bool
	client     bool
}

// integrationActorFacts reads the lease of the integration actor of value and
// compares it with the record, as currentIntegrationActor always has.
func (p *project) integrationActorFacts(value api.Integration) (actorFacts, error) {
	var claim claimRecord
	var found bool
	var err error
	if value.Request.GroupID != "" {
		claim, found, err = p.readGroupClaim(value.Request.GroupID)
	} else {
		claim, found, err = p.readClaim(value.Request.Members[0].TaskID)
	}
	if err != nil {
		return actorFacts{}, err
	}
	var standing bool
	if value.Request.GroupID != "" {
		standing = p.groupStanding(claim, found, claim.Generation)
	} else if standing, err = p.standing(claim, found, claim.Generation); err != nil {
		return actorFacts{}, err
	}
	return actorFacts{
		standing: standing, generation: claim.Generation,
		exec: claim.ExecutionID == value.Request.ActorExecutionID, client: claim.ClientID == value.ClientID,
	}, nil
}

// integrationLease is the integration record a request presents the claim
// headers of, projected, with the facts of its owner and of its actor.
type integrationLease struct {
	record integrationRecord
	found  bool
	kernel *integrationKernel
	hdr    bool
	g, tg  uint64
	client bool
	actor  actorFacts
}

// headers maps the claim headers of h against the record: the headers parsed,
// the generation header, and the generation the token was derived for. A header
// or token of any other generation maps to the generation after the record's,
// which the kernel tells apart from the current one exactly as the server does.
func (l *integrationLease) headers(h holder) {
	other := l.kernel.state.Gen + 1
	l.g, l.tg = other, other
	if l.found && h.generation == l.record.Value.Generation {
		l.g = l.kernel.state.Gen
	}
	if l.found && h.token != "" && subtle.ConstantTimeCompare([]byte(tokenHash(h.token)), []byte(l.record.Hash)) == 1 {
		l.tg = l.kernel.state.Gen
	}
	l.hdr, l.client = h.invalid == nil, h.clientID == l.record.Value.ClientID
}

func (l *integrationLease) requireOwner() serverintegration.Event {
	return serverintegration.RequireOwner{Hdr: l.hdr, G: l.g, Tg: l.tg, Client: l.client}
}

func (l *integrationLease) actorGen() uint64 { return l.kernel.bases.auth.of(l.actor.generation) }

func (l *integrationLease) require() serverintegration.Event {
	return serverintegration.Require{Hdr: l.hdr, G: l.g, Tg: l.tg, Client: l.client, ActorStanding: l.actor.standing, ActorGen: l.actorGen(), ActorExec: l.actor.exec, ActorClient: l.actor.client}
}

func (l *integrationLease) renew(facts bool) serverintegration.Event {
	return serverintegration.Renew{Hdr: l.hdr, G: l.g, Tg: l.tg, Client: l.client, ActorStanding: l.actor.standing, ActorGen: l.actorGen(), ActorExec: l.actor.exec, ActorClient: l.actor.client, Facts: facts}
}

func (l *integrationLease) guard(ticket, facts bool) serverintegration.Event {
	return serverintegration.Guard{Hdr: l.hdr, G: l.g, Tg: l.tg, Client: l.client, ActorStanding: l.actor.standing, ActorGen: l.actorGen(), ActorExec: l.actor.exec, ActorClient: l.actor.client, Ticket: ticket, Facts: facts}
}

func (l *integrationLease) guardRelease(ticket bool) serverintegration.Event {
	return serverintegration.GuardRelease{Hdr: l.hdr, G: l.g, Tg: l.tg, Client: l.client, Ticket: ticket}
}

// ownerError is the answer of requireIntegrationOwner to a request the kernel
// refused, in the order the server has always checked: the headers, then
// whether a newer generation took the target over, then the record, then the
// client and the token. As for the single card claim, the taken-over answer
// does not authenticate the old token and grants nothing.
func (l *integrationLease) ownerError(h holder) error {
	if h.invalid != nil {
		return h.invalid
	}
	if l.found && takenOver(h, l.record.Hash, l.record.Value.Generation) {
		return apiError(api.CodeTakenOver, "target was taken over by integration generation "+strconv.FormatUint(l.record.Value.Generation, 10))
	}
	if !l.found || l.record.Value.Status != api.ClaimActive || l.record.Value.Generation != h.generation {
		return apiError(api.CodeLeaseInvalid, "integration lease is no longer current")
	}
	return apiError(api.CodeHeldByOther, "target is held by another integration execution")
}

// integrationOwner reads the record of id and runs requireIntegrationOwner on
// the claim headers of h. A refused request returns the error the server has
// always answered.
func (p *project) integrationOwner(id string, h holder) (*integrationLease, error) {
	record, found, err := p.readIntegration(id)
	if err != nil {
		return nil, err
	}
	k, err := p.projectIntegration(record.Value, found, nil)
	if err != nil {
		return nil, err
	}
	l := &integrationLease{record: record, found: found, kernel: k}
	l.headers(h)
	if !k.step(l.requireOwner()) {
		return l, l.ownerError(h)
	}
	return l, nil
}

// integrationHolder is requireIntegration: the owner whose integration actor
// still holds its task or coordinator lease.
func (p *project) integrationHolder(id string, h holder) (*integrationLease, error) {
	l, err := p.integrationOwner(id, h)
	if err != nil {
		return l, err
	}
	if l.actor, err = p.integrationActorFacts(l.record.Value); err != nil {
		return l, err
	}
	if !l.kernel.step(l.require()) {
		return l, apiError(api.CodeLeaseInvalid, "integration actor lost its task or coordinator authority")
	}
	return l, nil
}

// integrationClaim is the pre-transaction decision of claimIntegration against
// the record previous: the kernel event, built from the request and the actor
// the transaction accepted, and the error of a refused claim.
type integrationClaim struct {
	kernel   *integrationKernel
	previous integrationRecord
	found    bool
	input    api.IntegrationRequest
	h        holder
	actor    claimRecord
	actorErr error
}

func (c *integrationClaim) event() serverintegration.Event {
	prev := c.previous.Value
	e := serverintegration.Claim{
		ExpGen: c.kernel.generation(c.input.ExpectedGeneration, prev.Generation), ExpRev: c.kernel.revision(c.input.ExpectedRevision, prev.Revision),
		RoomGen: prev.Generation != ^uint64(0), RoomRev: prev.Revision != ^uint64(0),
		SameExec: c.found && prev.Request.ExecutionID == c.input.ExecutionID, Actor: c.actorErr == nil,
		Purpose: integrationPurposes[c.input.Purpose], Contained: c.input.AlreadyContained,
	}
	if c.actorErr == nil {
		e.ActorMatch = c.actor.ClientID == c.h.clientID && c.actor.ExecutionID == c.input.ActorExecutionID
		e.ActorGen = c.kernel.bases.auth.of(c.actor.Generation)
	}
	return e
}

// err is the answer to a claim the kernel refused, in the order the server has
// always checked.
func (c *integrationClaim) err() error {
	prev := c.previous.Value
	switch {
	case c.kernel.valid():
		return apiError(api.CodeLeaseActive, "target already has an active integration lease")
	case c.input.ExpectedGeneration != prev.Generation || c.input.ExpectedRevision != prev.Revision:
		return apiError(api.CodeRevisionConflict, "integration target changed since inspection")
	case prev.Generation == ^uint64(0) || prev.Revision == ^uint64(0) || c.found && prev.Request.ExecutionID == c.input.ExecutionID:
		return apiError(api.CodeLeaseInvalid, "integration requires a fresh execution identity and generation")
	case c.actorErr != nil:
		return c.actorErr
	case c.actor.ClientID != c.h.clientID || c.actor.ExecutionID != c.input.ActorExecutionID:
		return apiError(api.CodeHeldByOther, "integration actor differs from the presented task or coordinator execution")
	}
	return apiError(api.CodeInvalidRequest, "invalid integration purpose")
}

// integrationRefusal is the error of a check the kernel refused after the
// shell computed the facts it needs: the error of the facts. A refusal without
// one means the shell and the kernel disagree, which is a bug, not a request.
func integrationRefusal(factsErr error) error {
	if factsErr != nil {
		return factsErr
	}
	return apiError(api.CodeInternal, "integration kernel refused a request that the facts allow")
}

// integrationActionDecision is one action of an integration the lease of which
// the kernel accepted: the events of its checks and of its action, and the
// errors of a refusal in the order the server has always checked.
type integrationActionDecision struct {
	lease *integrationLease
	input api.IntegrationCommand
	now   time.Time
}

func (a *integrationActionDecision) expRev() uint64 {
	return a.lease.kernel.revision(a.input.ExpectedRevision, a.lease.record.Value.Revision)
}

func (a *integrationActionDecision) room() bool { return a.lease.record.Value.Revision != ^uint64(0) }

// check is the decision that precedes the switch on the action: the lease, the
// inspected revision, and the room for one more revision. A `release` needs the
// owner only.
func (a *integrationActionDecision) check() serverintegration.Event {
	l := a.lease
	if a.input.Action == "release" {
		return serverintegration.CheckOwner{Hdr: l.hdr, G: l.g, Tg: l.tg, Client: l.client, ExpRev: a.expRev(), Room: a.room()}
	}
	return serverintegration.Check{Hdr: l.hdr, G: l.g, Tg: l.tg, Client: l.client, ActorStanding: l.actor.standing, ActorGen: l.actorGen(), ActorExec: l.actor.exec, ActorClient: l.actor.client, ExpRev: a.expRev(), Room: a.room()}
}

// revisionError is the answer to a check the kernel refused after the lease
// passed.
func (a *integrationActionDecision) revisionError() error {
	if a.input.ExpectedRevision == 0 || a.input.ExpectedRevision != a.lease.record.Value.Revision {
		return apiError(api.CodeRevisionConflict, "integration requires its inspected resource version")
	}
	return apiError(api.CodeRevisionConflict, "integration revision exhausted")
}

// event is the kernel event of the action, or false for an action the server
// does not know.
func (a *integrationActionDecision) event() (serverintegration.Event, bool) {
	l, in, request := a.lease, a.input, a.lease.record.Value.Request
	sha := api.CommitSHA(in.ObservedTarget)
	switch in.Action {
	case "pushing":
		return serverintegration.Pushing{Hdr: l.hdr, G: l.g, Tg: l.tg, Client: l.client, ActorStanding: l.actor.standing, ActorGen: l.actorGen(), ActorExec: l.actor.exec, ActorClient: l.actor.client, ExpRev: a.expRev(), Room: a.room(), ObsMatch: in.ObservedTarget == request.ExpectedTarget, ObsSha: sha}, true
	case "confirm":
		required := []api.IntegrationEdge{{Ancestor: request.ConfirmAncestor(), Descendant: in.ObservedTarget}}
		return serverintegration.Confirm{Hdr: l.hdr, G: l.g, Tg: l.tg, Client: l.client, ActorStanding: l.actor.standing, ActorGen: l.actorGen(), ActorExec: l.actor.exec, ActorClient: l.actor.client, ExpRev: a.expRev(), Room: a.room(), ObsSha: sha, ObsAt: !in.ObservedAt.IsZero(), EdgesOk: reflect.DeepEqual(normalizedIntegrationEdges(required), normalizedIntegrationEdges(in.Edges))}, true
	case "release":
		return serverintegration.Release{Hdr: l.hdr, G: l.g, Tg: l.tg, Client: l.client, ExpRev: a.expRev(), Room: a.room(), Decision: strings.TrimSpace(in.Decision) != "", ObsEmpty: in.ObservedTarget == "", ObsSha: sha}, true
	}
	return nil, false
}

// refusal is the answer to an action the kernel refused after its checks
// passed: the evidence the action needs is incomplete.
func (a *integrationActionDecision) refusal() error {
	record := a.lease.record.Value
	switch a.input.Action {
	case "pushing":
		return apiError(api.CodeEvidenceIncomplete, "push intent must bind the unchanged inspected remote target")
	case "confirm":
		reconciles := record.Phase == "reserved" && record.Request.AlreadyContained
		if (!reconciles && record.Phase != "pushing") || !api.CommitSHA(a.input.ObservedTarget) || a.input.ObservedAt.IsZero() {
			return apiError(api.CodeEvidenceIncomplete, "completion requires a push intent and fresh exact remote facts")
		}
		return apiError(api.CodeEvidenceIncomplete, "completion must prove the integrated source is in the observed remote target")
	}
	return apiError(api.CodeEvidenceIncomplete, "release requires a decision and inspected remote state")
}
