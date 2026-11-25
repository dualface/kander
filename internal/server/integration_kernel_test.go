package server_test

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/server"
	"github.com/dualface/kander/internal/statemachine/serverintegration"
)

// integrationProbe projects the integration record of the target v2 before and
// after an entrance and compares it with the kernel step.
type integrationProbe struct {
	t       *testing.T
	f       *fixture
	project string
	id      string
}

func (i integrationProbe) state(bases *server.IntegrationBases) (serverintegration.State, server.IntegrationBases) {
	i.t.Helper()
	st, used, err := i.f.srv.IntegrationState(i.project, i.id, bases)
	if err != nil {
		i.t.Fatal(err)
	}
	return st, used
}

func integrationParams(s serverintegration.State) serverintegration.Params {
	return serverintegration.Params{MaxGen: uint32(s.Gen + 1), MaxRev: uint32(s.Rev + 1), MaxAuth: uint32(s.Auth + 1)}
}

// refines runs fn and asserts projection(after) = Step(projection(before), e),
// both with the bases fixed before fn, when code is empty, or the error code, a
// kernel refusal, and an unchanged projection. build makes the event from the
// projection before fn.
func (i integrationProbe) refines(build func(serverintegration.State) serverintegration.Event, code string, fn func() error) {
	i.t.Helper()
	before, bases := i.state(nil)
	e := build(before)
	err := fn()
	after, _ := i.state(&bases)
	want, ok := serverintegration.Step(before, e, integrationParams(before))
	switch {
	case ok != (code == ""):
		i.t.Fatalf("kernel acceptance of %T%+v on %+v: %v, want %v", e, e, before, ok, code == "")
	case code == "" && err != nil:
		i.t.Fatalf("%T: %v", e, err)
	case code == "" && after != want:
		i.t.Fatalf("%T: projection %+v, kernel %+v", e, after, want)
	case code != "" && !api.IsCode(err, code):
		i.t.Fatalf("%T on %+v: error %v, want %s", e, before, err, code)
	case code != "" && after != before:
		i.t.Fatalf("refused %T changed the projection: %+v -> %+v", e, before, after)
	}
}

// guarded runs fn, which the kernel refuses at the check inside the transaction
// (guard, claimGuard, or guardRelease) after the check before it accepted: the
// entrance fails with code and the projection stays.
func (i integrationProbe) guarded(build func(serverintegration.State) serverintegration.Event, code string, fn func() error) {
	i.t.Helper()
	before, bases := i.state(nil)
	e := build(before)
	err := fn()
	after, _ := i.state(&bases)
	if _, ok := serverintegration.Step(before, e, integrationParams(before)); ok || !api.IsCode(err, code) || after != before {
		i.t.Fatalf("%T on %+v: kernel accepted %v, error %v, after %+v, want %s", e, before, ok, err, after, code)
	}
}

// lease is the claim headers and the actor facts of a request against the
// record projected before it.
type lease struct {
	hdr, client, actorStanding, actorExec, actorClient bool
	g, tg, actorGen                                    uint64
}

// current is the owner of the record with its actor holding the lease.
func current(s serverintegration.State) lease {
	return lease{hdr: true, client: true, actorStanding: true, actorExec: true, actorClient: true, g: s.Gen, tg: s.Gen, actorGen: s.Auth}
}

func (l lease) renew(facts bool) serverintegration.Event {
	return serverintegration.Renew{Hdr: l.hdr, G: l.g, Tg: l.tg, Client: l.client, ActorStanding: l.actorStanding, ActorGen: l.actorGen, ActorExec: l.actorExec, ActorClient: l.actorClient, Facts: facts}
}

func (l lease) pushing(rev uint64, room, match, sha bool) serverintegration.Event {
	return serverintegration.Pushing{Hdr: l.hdr, G: l.g, Tg: l.tg, Client: l.client, ActorStanding: l.actorStanding, ActorGen: l.actorGen, ActorExec: l.actorExec, ActorClient: l.actorClient, ExpRev: rev, Room: room, ObsMatch: match, ObsSha: sha}
}

func (l lease) confirm(rev uint64, room, sha, at, edges bool) serverintegration.Event {
	return serverintegration.Confirm{Hdr: l.hdr, G: l.g, Tg: l.tg, Client: l.client, ActorStanding: l.actorStanding, ActorGen: l.actorGen, ActorExec: l.actorExec, ActorClient: l.actorClient, ExpRev: rev, Room: room, ObsSha: sha, ObsAt: at, EdgesOk: edges}
}

func (l lease) release(rev uint64, room, decision, empty, sha bool) serverintegration.Event {
	return serverintegration.Release{Hdr: l.hdr, G: l.g, Tg: l.tg, Client: l.client, ExpRev: rev, Room: room, Decision: decision, ObsEmpty: empty, ObsSha: sha}
}

func (l lease) guard(ticket, facts bool) serverintegration.Event {
	return serverintegration.Guard{Hdr: l.hdr, G: l.g, Tg: l.tg, Client: l.client, ActorStanding: l.actorStanding, ActorGen: l.actorGen, ActorExec: l.actorExec, ActorClient: l.actorClient, Ticket: ticket, Facts: facts}
}

func (l lease) guardRelease(ticket bool) serverintegration.Event {
	return serverintegration.GuardRelease{Hdr: l.hdr, G: l.g, Tg: l.tg, Client: l.client, Ticket: ticket}
}

// claimOf is a claim of the record projected before it, by an actor that is
// accepted and presents the request's client and execution identity. The
// request inspected the record's generation and revision.
func claimOf(b serverintegration.State, contained bool) serverintegration.Claim {
	return serverintegration.Claim{ExpGen: b.Gen, ExpRev: b.Rev, RoomGen: true, RoomRev: true, Actor: true, ActorMatch: true, ActorGen: b.Auth, Purpose: serverintegration.PurposeDeliver, Contained: contained}
}

type integrationRig struct {
	t      *testing.T
	f      *fixture
	a, b   *client.Project
	member api.IntegrationMember
	task   api.ClaimGrant
	owner  client.Claim
	probe  integrationProbe
}

func newIntegrationRig(t *testing.T, slug string) *integrationRig {
	t.Helper()
	f := newFixture(t)
	r := &integrationRig{t: t, f: f, a: f.project("host-a"), b: f.project("host-b")}
	r.member, r.task = integrationTask(t, f, r.a, "host-a", slug)
	r.owner = client.Claim{Token: r.task.Token, Generation: r.task.Claim.Generation}
	r.probe = integrationProbe{t: t, f: f, project: r.a.ID(), id: api.IntegrationID("refs/heads/v2")}
	return r
}

func (r *integrationRig) input(execution string) api.IntegrationRequest {
	r.t.Helper()
	return integrationInput(r.t, r.a, r.member, r.task.Claim.ExecutionID, execution)
}

// claim is a claim the kernel accepts, with the authority generation the task's
// own claim has; the first claim of a target takes the authority generation as
// a value other than the record's (none).
func (r *integrationRig) claim(key string, input api.IntegrationRequest, out *api.IntegrationGrant) {
	r.t.Helper()
	r.probe.refines(func(b serverintegration.State) serverintegration.Event {
		e := claimOf(b, input.AlreadyContained)
		if b.Status == serverintegration.StatusNone || r.task.Claim.Generation != b.Auth {
			e.ActorGen = b.Auth + 1
		}
		return e
	}, "", func() (err error) {
		*out, err = r.a.ClaimIntegration(r.t.Context(), key, input, r.owner)
		return err
	})
}

func (r *integrationRig) command(proof client.Claim, grantRevision uint64, action string) api.IntegrationCommand {
	r.t.Helper()
	observation, err := r.a.IntegrationObservation(r.t.Context(), r.probe.id)
	if err != nil {
		r.t.Fatal(err)
	}
	return api.IntegrationCommand{Action: action, ExpectedRevision: grantRevision, ObservationTicket: observation.Ticket}
}

// Every entrance of an integration refines the kernel: a claim, a renewal, and
// the three actions, through the refusals of theorems 1 to 5: a forged token, a
// foreign client, a stale revision, a stale ticket, a target the claim did not
// inspect, a missing push intent, and every request of a record that
// integrated.
func TestIntegrationEntrancesRefineTheKernel(t *testing.T) {
	r := newIntegrationRig(t, "refine-entrances")
	ctx := t.Context()
	input := r.input("first-execution")
	var grant api.IntegrationGrant
	r.claim("first-claim", input, &grant)
	proof := client.Claim{Token: grant.Token, Generation: grant.Integration.Generation}
	forged := client.Claim{Token: "forged", Generation: proof.Generation}

	r.probe.refines(func(b serverintegration.State) serverintegration.Event {
		l := current(b)
		l.tg = b.Gen + 1
		return l.renew(true)
	}, api.CodeHeldByOther, func() error { _, err := r.a.RenewIntegration(ctx, grant.Integration.ID, forged); return err })
	r.probe.refines(func(b serverintegration.State) serverintegration.Event {
		l := current(b)
		l.client = false
		return l.renew(true)
	}, api.CodeHeldByOther, func() error { _, err := r.b.RenewIntegration(ctx, grant.Integration.ID, proof); return err })
	r.f.clock.advance(16 * time.Second)
	r.probe.refines(func(b serverintegration.State) serverintegration.Event { return current(b).renew(true) }, "", func() error {
		_, err := r.a.RenewIntegration(ctx, grant.Integration.ID, proof)
		return err
	})

	push := func(command api.IntegrationCommand) func() error {
		return func() error {
			_, err := r.a.IntegrationAction(ctx, "", grant.Integration.ID, command, proof)
			return err
		}
	}
	pushing := r.command(proof, grant.Integration.Revision, "pushing")
	pushing.ObservedTarget = input.ExpectedTarget
	other := pushing
	other.ObservedTarget = strings.Repeat("d", 40)
	r.probe.refines(func(b serverintegration.State) serverintegration.Event {
		return current(b).pushing(b.Rev, true, false, true)
	}, api.CodeEvidenceIncomplete, push(other))
	stale := pushing
	stale.ExpectedRevision = grant.Integration.Revision + 7
	r.probe.refines(func(b serverintegration.State) serverintegration.Event {
		return current(b).pushing(b.Rev+1, true, true, true)
	}, api.CodeRevisionConflict, push(stale))
	badTicket := pushing
	badTicket.ObservationTicket = "stale"
	r.probe.guarded(func(b serverintegration.State) serverintegration.Event { return current(b).guard(false, true) },
		api.CodeEvidenceIncomplete, push(badTicket))
	r.probe.refines(func(b serverintegration.State) serverintegration.Event {
		return current(b).pushing(b.Rev, true, true, true)
	}, "", push(pushing))

	// A second push intent, and a confirmation without the exact edge.
	again := r.command(proof, grant.Integration.Revision+1, "pushing")
	again.ObservedTarget = input.ExpectedTarget
	r.probe.refines(func(b serverintegration.State) serverintegration.Event {
		return current(b).pushing(b.Rev, true, true, true)
	}, api.CodeEvidenceIncomplete, push(again))
	confirm := r.command(proof, grant.Integration.Revision+1, "confirm")
	confirm.ObservedTarget, confirm.ObservedAt = input.Commit, time.Now().UTC()
	noEdge := confirm
	r.probe.refines(func(b serverintegration.State) serverintegration.Event {
		return current(b).confirm(b.Rev, true, true, true, false)
	}, api.CodeEvidenceIncomplete, push(noEdge))
	noTime := confirm
	noTime.ObservedAt = time.Time{}
	noTime.Edges = []api.IntegrationEdge{{Ancestor: input.Commit, Descendant: input.Commit}}
	r.probe.refines(func(b serverintegration.State) serverintegration.Event {
		return current(b).confirm(b.Rev, true, true, false, true)
	}, api.CodeEvidenceIncomplete, push(noTime))
	confirm.Edges = noTime.Edges
	r.probe.refines(func(b serverintegration.State) serverintegration.Event {
		return current(b).confirm(b.Rev, true, true, true, true)
	}, "", push(confirm))

	// The record integrated: it has no lease, and nothing changes it but a claim.
	r.probe.refines(func(b serverintegration.State) serverintegration.Event { return current(b).renew(true) }, api.CodeLeaseInvalid, func() error {
		_, err := r.a.RenewIntegration(ctx, grant.Integration.ID, proof)
		return err
	})
	late := r.command(proof, grant.Integration.Revision+2, "release")
	late.Decision = "release a record that integrated"
	r.probe.refines(func(b serverintegration.State) serverintegration.Event {
		return current(b).release(b.Rev, true, true, true, false)
	}, api.CodeLeaseInvalid, push(late))

	// The next generation releases without a decision, with one, and from a push
	// intent, which leaves the target uncertain.
	var next api.IntegrationGrant
	r.claim("second-claim", r.input("second-execution"), &next)
	second := client.Claim{Token: next.Token, Generation: next.Integration.Generation}
	release := func(command api.IntegrationCommand) func() error {
		return func() error {
			_, err := r.a.IntegrationAction(ctx, "", next.Integration.ID, command, second)
			return err
		}
	}
	noDecision := r.command(second, next.Integration.Revision, "release")
	r.probe.refines(func(b serverintegration.State) serverintegration.Event {
		return current(b).release(b.Rev, true, false, true, false)
	}, api.CodeEvidenceIncomplete, release(noDecision))
	noDecision.Decision = "abandon the reserved target"
	r.probe.guarded(func(b serverintegration.State) serverintegration.Event { return current(b).guardRelease(false) },
		api.CodeEvidenceIncomplete, release(api.IntegrationCommand{Action: "release", ExpectedRevision: next.Integration.Revision, Decision: "abandon the reserved target", ObservationTicket: "stale"}))
	r.probe.refines(func(b serverintegration.State) serverintegration.Event {
		return current(b).release(b.Rev, true, true, true, false)
	}, "", release(noDecision))

	var third api.IntegrationGrant
	r.claim("third-claim", r.input("third-execution"), &third)
	thirdProof := client.Claim{Token: third.Token, Generation: third.Integration.Generation}
	intent := r.command(thirdProof, third.Integration.Revision, "pushing")
	intent.ObservedTarget = input.ExpectedTarget
	r.probe.refines(func(b serverintegration.State) serverintegration.Event {
		return current(b).pushing(b.Rev, true, true, true)
	}, "", func() error {
		_, err := r.a.IntegrationAction(ctx, "", third.Integration.ID, intent, thirdProof)
		return err
	})
	uncertain := r.command(thirdProof, third.Integration.Revision+1, "release")
	uncertain.Decision = "the push result is unknown"
	r.probe.refines(func(b serverintegration.State) serverintegration.Event {
		return current(b).release(b.Rev, true, true, true, false)
	}, "", func() error {
		out, err := r.a.IntegrationAction(ctx, "", third.Integration.ID, uncertain, thirdProof)
		if err == nil && out.Phase != "uncertain" {
			t.Fatalf("release of a push intent: %+v", out)
		}
		return err
	})
}

// A claim is refused while a lease is valid, when the request inspected another
// revision or reuses the execution identity of the record, and when the actor is
// not accepted or is not the request's. It is accepted over a record whose
// lease expired in any phase.
func TestIntegrationClaimRefusalsRefineTheKernel(t *testing.T) {
	r := newIntegrationRig(t, "refine-claims")
	ctx := t.Context()
	other, otherGrant := integrationTask(t, r.f, r.b, "host-b", "refine-claims-other")
	otherInput := func(execution string) api.IntegrationRequest {
		return integrationInput(t, r.b, other, otherGrant.Claim.ExecutionID, execution)
	}
	var grant api.IntegrationGrant
	input := r.input("held-execution")
	r.claim("held-claim", input, &grant)
	otherProof := client.Claim{Token: otherGrant.Token, Generation: otherGrant.Claim.Generation}
	valid := otherInput("rival-execution")
	r.probe.refines(func(b serverintegration.State) serverintegration.Event {
		e := claimOf(b, false)
		e.ActorGen = b.Auth + 1
		return e
	}, api.CodeLeaseActive, func() error { _, err := r.b.ClaimIntegration(ctx, "", valid, otherProof); return err })

	// An expired reserved record is claimed again, by the same host, after the
	// actor renewed its own lease.
	r.f.clock.advance(80 * time.Second)
	if _, err := r.a.Renew(ctx, r.member.TaskID, r.owner); err != nil {
		t.Fatal(err)
	}
	r.f.clock.advance(11 * time.Second)
	var second api.IntegrationGrant
	r.claim("expired-reserved", r.input("second-execution"), &second)

	// The same for an expired push intent.
	secondProof := client.Claim{Token: second.Token, Generation: second.Integration.Generation}
	intent := r.command(secondProof, second.Integration.Revision, "pushing")
	intent.ObservedTarget = input.ExpectedTarget
	r.probe.refines(func(b serverintegration.State) serverintegration.Event {
		return current(b).pushing(b.Rev, true, true, true)
	}, "", func() error {
		_, err := r.a.IntegrationAction(ctx, "", second.Integration.ID, intent, secondProof)
		return err
	})
	r.f.clock.advance(50 * time.Second)
	if _, err := r.a.Renew(ctx, r.member.TaskID, r.owner); err != nil {
		t.Fatal(err)
	}
	r.f.clock.advance(41 * time.Second)
	third := r.input("third-execution")
	var thirdGrant api.IntegrationGrant
	r.claim("expired-pushing", third, &thirdGrant)

	// A claim names the generation and the revision it inspected, a fresh
	// execution identity, and an accepted actor that presents its own identity.
	thirdProof := client.Claim{Token: thirdGrant.Token, Generation: thirdGrant.Integration.Generation}
	release := r.command(thirdProof, thirdGrant.Integration.Revision, "release")
	release.Decision = "make the record claimable"
	if _, err := r.a.IntegrationAction(ctx, "", thirdGrant.Integration.ID, release, thirdProof); err != nil {
		t.Fatal(err)
	}
	claimWith := func(mutate func(*api.IntegrationRequest)) func() error {
		input := r.input("fourth-execution")
		mutate(&input)
		return func() error { _, err := r.a.ClaimIntegration(ctx, "", input, r.owner); return err }
	}
	r.probe.refines(func(b serverintegration.State) serverintegration.Event {
		e := claimOf(b, false)
		e.ExpRev = b.Rev + 1
		return e
	}, api.CodeRevisionConflict, claimWith(func(in *api.IntegrationRequest) { in.ExpectedRevision-- }))
	r.probe.refines(func(b serverintegration.State) serverintegration.Event {
		e := claimOf(b, false)
		e.SameExec = true
		return e
	}, api.CodeLeaseInvalid, claimWith(func(in *api.IntegrationRequest) { in.ExecutionID = thirdGrant.Integration.Request.ExecutionID }))
	r.probe.refines(func(b serverintegration.State) serverintegration.Event {
		e := claimOf(b, false)
		e.ActorMatch = false
		return e
	}, api.CodeHeldByOther, claimWith(func(in *api.IntegrationRequest) { in.ActorExecutionID = "somebody-else" }))
	r.probe.refines(func(b serverintegration.State) serverintegration.Event {
		e := claimOf(b, false)
		e.Actor, e.ActorMatch, e.ActorGen = false, false, 0
		return e
	}, api.CodeHeldByOther, func() error {
		input := r.input("fifth-execution")
		_, err := r.a.ClaimIntegration(ctx, "", input, client.Claim{Token: "forged", Generation: r.owner.Generation})
		return err
	})
	// The request's ticket and the facts are checked inside the transaction.
	r.probe.guarded(func(b serverintegration.State) serverintegration.Event {
		return serverintegration.ClaimGuard{Ticket: false, Actor: true, Facts: true}
	}, api.CodeEvidenceIncomplete, claimWith(func(in *api.IntegrationRequest) { in.ObservationTicket = "stale" }))
	r.probe.guarded(func(b serverintegration.State) serverintegration.Event {
		return serverintegration.ClaimGuard{Ticket: true, Actor: true, Facts: false}
	}, api.CodeEvidenceIncomplete, claimWith(func(in *api.IntegrationRequest) { in.Edges = nil }))
}

// Member progress breaks the integration facts of a renewal and of an action.
func TestIntegrationFactsRefineTheKernel(t *testing.T) {
	r := newIntegrationRig(t, "refine-actor")
	ctx := t.Context()
	var grant api.IntegrationGrant
	r.claim("actor-claim", r.input("actor-execution"), &grant)
	proof := client.Claim{Token: grant.Token, Generation: grant.Integration.Generation}

	// Progress on the member changes the card revision the claim bound.
	if _, err := r.a.PutDocument(ctx, "progress", r.member.TaskID, api.Document{Path: "progress.md", Text: "progress", ExpectedRevision: r.member.Revision}, &r.owner); err != nil {
		t.Fatal(err)
	}
	r.probe.refines(func(b serverintegration.State) serverintegration.Event { return current(b).renew(false) }, api.CodeRevisionConflict, func() error {
		_, err := r.a.RenewIntegration(ctx, grant.Integration.ID, proof)
		return err
	})
	command := r.command(proof, grant.Integration.Revision, "pushing")
	command.ObservedTarget = r.input("unused").ExpectedTarget
	r.probe.guarded(func(b serverintegration.State) serverintegration.Event { return current(b).guard(true, false) }, api.CodeRevisionConflict, func() error {
		_, err := r.a.IntegrationAction(ctx, "", grant.Integration.ID, command, proof)
		return err
	})

}

// The integration lease is renewed, the task lease of the actor is not: while
// the actor's lease only lapsed it still stands and the integration renews;
// once a takeover replaced the actor every check that needs the actor refuses,
// and a release, which needs the owner only, still ends the integration.
func TestIntegrationActorLeaseLossRefinesTheKernel(t *testing.T) {
	r := newIntegrationRig(t, "refine-actor-lost")
	ctx := t.Context()
	var grant api.IntegrationGrant
	r.claim("actor-claim", r.input("actor-execution"), &grant)
	proof := client.Claim{Token: grant.Token, Generation: grant.Integration.Generation}
	command := r.command(proof, grant.Integration.Revision, "pushing")
	command.ObservedTarget = r.input("unused").ExpectedTarget
	r.f.clock.advance(45 * time.Second)
	r.probe.refines(func(b serverintegration.State) serverintegration.Event { return current(b).renew(true) }, "", func() error {
		_, err := r.a.RenewIntegration(ctx, grant.Integration.ID, proof)
		return err
	})
	r.f.clock.advance(46 * time.Second)
	r.probe.refines(func(b serverintegration.State) serverintegration.Event { return current(b).renew(true) }, "", func() error {
		_, err := r.a.RenewIntegration(ctx, grant.Integration.ID, proof)
		return err
	})
	if _, err := r.b.Takeover(ctx, "", r.member.TaskID, api.ClaimRequest{ClientID: "host-b", ExecutionID: "actor-successor", Owner: "codex", Decision: "user:takeover", ExpectedGeneration: r.task.Claim.Generation}); err != nil {
		t.Fatal(err)
	}
	lost := func(b serverintegration.State) lease {
		l := current(b)
		l.actorStanding = false
		return l
	}
	r.probe.refines(func(b serverintegration.State) serverintegration.Event { return lost(b).renew(true) }, api.CodeLeaseInvalid, func() error {
		_, err := r.a.RenewIntegration(ctx, grant.Integration.ID, proof)
		return err
	})
	r.probe.refines(func(b serverintegration.State) serverintegration.Event {
		return lost(b).pushing(b.Rev, true, true, true)
	}, api.CodeLeaseInvalid, func() error {
		_, err := r.a.IntegrationAction(ctx, "", grant.Integration.ID, command, proof)
		return err
	})
	release := r.command(proof, grant.Integration.Revision, "release")
	release.Decision = "the actor lost its lease"
	r.probe.refines(func(b serverintegration.State) serverintegration.Event {
		return lost(b).release(b.Rev, true, true, true, false)
	}, "", func() error {
		_, err := r.a.IntegrationAction(ctx, "", grant.Integration.ID, release, proof)
		return err
	})
}

// The counters of a record near the ends of uint64: a record at the last
// revision refuses every action, a record at the last generation or revision
// refuses a claim, and counters above 2^31 and 2^40 behave like small ones.
func TestIntegrationCountersRefineTheKernel(t *testing.T) {
	r := newIntegrationRig(t, "refine-counters")
	ctx := t.Context()
	var grant api.IntegrationGrant
	r.claim("counter-claim", r.input("counter-execution"), &grant)
	proof := client.Claim{Token: grant.Token, Generation: grant.Integration.Generation}
	id := grant.Integration.ID
	set := func(generation, revision uint64) {
		if err := r.f.srv.MutateIntegration(r.probe.project, id, func(v *api.Integration) { v.Generation, v.Revision = generation, revision }); err != nil {
			t.Fatal(err)
		}
		proof.Generation = generation
	}

	set(1, math.MaxUint64)
	command := api.IntegrationCommand{Action: "pushing", ExpectedRevision: math.MaxUint64, ObservedTarget: r.input("unused").ExpectedTarget}
	r.probe.refines(func(b serverintegration.State) serverintegration.Event {
		return current(b).pushing(b.Rev, false, true, true)
	}, api.CodeRevisionConflict, func() error {
		_, err := r.a.IntegrationAction(ctx, "", id, command, proof)
		return err
	})
	for _, counters := range [][2]uint64{{math.MaxUint64, 4}, {2, math.MaxUint64}} {
		set(counters[0], counters[1])
		if err := r.f.srv.MutateIntegration(r.probe.project, id, func(v *api.Integration) { v.Status, v.Phase = api.ClaimReleased, "released" }); err != nil {
			t.Fatal(err)
		}
		r.probe.refines(func(b serverintegration.State) serverintegration.Event {
			e := claimOf(b, false)
			e.RoomGen, e.RoomRev = counters[0] != math.MaxUint64, counters[1] != math.MaxUint64
			return e
		}, api.CodeLeaseInvalid, func() error {
			_, err := r.a.ClaimIntegration(ctx, "", r.input("exhausted-execution"), r.owner)
			return err
		})
	}
	for _, big := range []uint64{1 << 31, 1<<31 + 5, 1 << 40} {
		set(big, big+3)
		r.probe.refines(func(b serverintegration.State) serverintegration.Event { return claimOf(b, false) }, "", func() error {
			_, err := r.a.ClaimIntegration(ctx, "", r.input("big-execution-"+strings.Repeat("x", int(big%7))), r.owner)
			return err
		})
		if err := r.f.srv.MutateIntegration(r.probe.project, id, func(v *api.Integration) { v.Status, v.Phase = api.ClaimReleased, "released" }); err != nil {
			t.Fatal(err)
		}
	}
}

// The completion evidence of a task and the integration evidence of a wrap-up
// refine the kernel on the published receipt: they accept a receipt that
// integrated for the task's generation, and refuse a task without a receipt, a
// receipt of another source, target, project, or generation, and another task.
func TestIntegrationEvidenceRefinesTheKernel(t *testing.T) {
	r := newIntegrationRig(t, "refine-evidence")
	input := r.input("evidence-execution")
	if _, err := r.a.TaskIntegration(t.Context(), r.member.TaskID); !api.IsCode(err, api.CodeNotFound) {
		t.Fatalf("task integration before the integration: %v", err)
	}
	done := integrate(t, r.a, input, "evidence", r.owner)
	if view, err := r.a.TaskIntegration(t.Context(), r.member.TaskID); err != nil || view.Phase != "integrated" || view.Generation != done.Generation {
		t.Fatalf("task integration of the published receipt: %+v %v", view, err)
	}
	project := r.probe.project
	task, generation := r.member.TaskID, r.member.Generation

	receipt, err := r.f.srv.ReceiptState(project, task, generation)
	if err != nil {
		t.Fatal(err)
	}
	evidence := serverintegration.Completion{Closed: true, Member: true, PlanIsCommit: true}
	if _, ok := serverintegration.Step(receipt, evidence, integrationParams(receipt)); !ok {
		t.Fatalf("receipt %+v is not evidence", receipt)
	}
	if got, err := r.f.srv.CompletionEvidence(project, task, generation); err != nil || got.Phase != "integrated" {
		t.Fatalf("completion evidence: %+v %v", got, err)
	}
	// Another generation has no receipt: the empty state refuses the evidence.
	empty, err := r.f.srv.ReceiptState(project, task, generation+1)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := serverintegration.Step(empty, evidence, integrationParams(empty)); ok {
		t.Fatal("the empty state accepted completion evidence")
	}
	if _, err := r.f.srv.CompletionEvidence(project, task, generation+1); !api.IsCode(err, api.CodeNotFound) {
		t.Fatalf("task generation without a receipt: %v", err)
	}

	git := wrapUpRound(t, r.a, r.member, done, "evidence-wrap-up").Evidence.WrapUp.Git
	execution, err := r.f.srv.ExecutionState(project, done.ID, done.Request.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	wrap := func(g board.DispatchIntegration) serverintegration.WrapUp {
		return serverintegration.WrapUp{
			IdGiven: true, IdMatch: g.TargetRef == done.Request.TargetRef, Igen: execution.Gen, SrcMatch: g.SourceCommit == done.Request.Commit,
			TgtMatch: g.TargetCommit == done.ObservedTarget, CwdMatch: g.CWD == "project:"+project, Member: true, Closed: true, PlanIsSrc: true, ReviewIsSrc: g.ReviewTarget == g.SourceCommit,
		}
	}
	check := func(g board.DispatchIntegration, gen uint64, code string, mutate func(*serverintegration.WrapUp)) {
		t.Helper()
		e := wrap(g)
		mutate(&e)
		_, ok := serverintegration.Step(execution, e, integrationParams(execution))
		err := r.f.srv.WrapUpEvidence(project, task, gen, g)
		if ok != (code == "") || !(code == "" && err == nil || code != "" && api.IsCode(err, code)) {
			t.Fatalf("wrap-up evidence %+v: kernel %v, error %v, want %q", g, ok, err, code)
		}
	}
	check(git, generation, "", func(*serverintegration.WrapUp) {})
	source := git
	source.SourceCommit = strings.Repeat("e", 40)
	source.ReviewTarget = source.SourceCommit
	check(source, generation, api.CodeEvidenceIncomplete, func(*serverintegration.WrapUp) {})
	target := git
	target.TargetCommit = strings.Repeat("f", 40)
	check(target, generation, api.CodeEvidenceIncomplete, func(*serverintegration.WrapUp) {})
	cwd := git
	cwd.CWD = "project:elsewhere"
	check(cwd, generation, api.CodeEvidenceIncomplete, func(*serverintegration.WrapUp) {})
	ref := git
	ref.TargetRef = "refs/heads/other"
	check(ref, generation, api.CodeEvidenceIncomplete, func(*serverintegration.WrapUp) {})
	generationMismatch := git
	generationMismatch.IntegrationGeneration = done.Generation + 1
	check(generationMismatch, generation, api.CodeEvidenceIncomplete, func(e *serverintegration.WrapUp) { e.Igen = execution.Gen + 1 })
	review := git
	review.ReviewTarget = strings.Repeat("e", 40)
	check(review, generation, api.CodeEvidenceIncomplete, func(*serverintegration.WrapUp) {})
	// Another execution generation of the task is not a member of the receipt.
	check(git, generation+1, api.CodeLeaseInvalid, func(e *serverintegration.WrapUp) { e.Member = false })
	missing := git
	missing.IntegrationExecution = ""
	if err := r.f.srv.WrapUpEvidence(project, task, generation, missing); !api.IsCode(err, api.CodeEvidenceIncomplete) {
		t.Fatalf("wrap-up without an execution: %v", err)
	}
}

// A group integration has the coordinator of its group as its actor: the
// initialization of the group branch is claimed with the coordinator's lease,
// a lapsed coordinator lease still stands, and once the coordinator's agent is
// observed stopped (the group lease is lost) only the owner's release still
// ends it.
func TestIntegrationGroupActorRefinesTheKernel(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	ctx := t.Context()
	request := groupRequest(t, p)
	group := request.Request.GroupID
	coordinator, err := p.ClaimGroup(ctx, "integration-coordinator", group, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	coordinatorProof := client.Claim{Token: coordinator.Token, Generation: coordinator.Claim.Generation}
	target := "refs/heads/group/" + group
	base := strings.Repeat("a", 40)
	observation, err := p.IntegrationObservation(ctx, api.IntegrationID(target))
	if err != nil {
		t.Fatal(err)
	}
	input := api.IntegrationRequest{Purpose: "initialize", TargetRef: target, BaseCommit: base, Commit: base, GroupID: group, ExecutionID: "group-initialization", ActorExecutionID: request.ExecutionID, Decision: "initialize the confirmed group anchor", ObservationTicket: observation.Ticket, Edges: []api.IntegrationEdge{{Ancestor: base, Descendant: base}}}
	for _, id := range request.Request.Members {
		task, err := p.Show(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		input.Members = append(input.Members, api.IntegrationMember{TaskID: id, Revision: task.Revision})
	}
	probe := integrationProbe{t: t, f: f, project: p.ID(), id: api.IntegrationID(target)}
	var grant api.IntegrationGrant
	probe.refines(func(b serverintegration.State) serverintegration.Event {
		e := claimOf(b, false)
		e.Purpose, e.ActorGen = serverintegration.PurposeInitialize, b.Auth+1
		return e
	}, "", func() (err error) {
		grant, err = p.ClaimIntegration(ctx, "group-integration-claim", input, coordinatorProof)
		return err
	})
	proof := client.Claim{Token: grant.Token, Generation: grant.Integration.Generation}
	f.clock.advance(45 * time.Second)
	probe.refines(func(b serverintegration.State) serverintegration.Event { return current(b).renew(true) }, "", func() error {
		_, err := p.RenewIntegration(ctx, grant.Integration.ID, proof)
		return err
	})
	f.clock.advance(46 * time.Second)
	probe.refines(func(b serverintegration.State) serverintegration.Event { return current(b).renew(true) }, "", func() error {
		_, err := p.RenewIntegration(ctx, grant.Integration.ID, proof)
		return err
	})
	if _, err := p.ObserveGroup(ctx, "", group, api.AgentObservation{Status: "stopped"}, coordinatorProof); err != nil {
		t.Fatal(err)
	}
	lost := func(b serverintegration.State) lease {
		l := current(b)
		l.actorStanding = false
		return l
	}
	probe.refines(func(b serverintegration.State) serverintegration.Event { return lost(b).renew(true) }, api.CodeLeaseInvalid, func() error {
		_, err := p.RenewIntegration(ctx, grant.Integration.ID, proof)
		return err
	})
	release := api.IntegrationCommand{Action: "release", ExpectedRevision: grant.Integration.Revision, Decision: "the coordinator lost its lease"}
	observation, err = p.IntegrationObservation(ctx, grant.Integration.ID)
	if err != nil {
		t.Fatal(err)
	}
	release.ObservationTicket = observation.Ticket
	probe.refines(func(b serverintegration.State) serverintegration.Event {
		return lost(b).release(b.Rev, true, true, true, false)
	}, "", func() error {
		_, err := p.IntegrationAction(ctx, "", grant.Integration.ID, release, proof)
		return err
	})
}

// A task whose review is closed without a target and that has no delivery needs
// no integration: the completion evidence accepts the empty state.
func TestIntegrationCompletionNeedsNoReceiptWhenNotApplicable(t *testing.T) {
	f := newFixture(t)
	a := f.project("host-a")
	ctx := t.Context()
	task := todoTask(t, a, "not-applicable")
	g, err := claim(t, a, task.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	own := &client.Claim{Token: g.Token, Generation: g.Claim.Generation}
	cwd := t.TempDir()
	plan := map[string]any{
		"schema": 1, "sealed": true, "plan_id": "plan-na", "author": "claude", "basis": "test", "cwd": cwd, "report_language": "en",
		"task_ids": []string{task.TaskID},
		"batches": []map[string]any{{"batch_id": "batch-na", "task_ids": []string{task.TaskID}, "base": "N/A", "target_commit": "N/A",
			"requirements": map[string]string{"PMQA": "N/A: test", "Security": "N/A: test"}}},
	}
	if _, err := a.ReviewPlan(ctx, "", plan, own); err != nil {
		t.Fatal(err)
	}
	if got, err := f.srv.CompletionEvidence(a.ID(), task.TaskID, g.Claim.Generation); err == nil {
		t.Fatalf("an open review needs no receipt: %+v", got)
	}
	// An open review is not "not applicable": the empty state refuses, so the view
	// has no integration to show.
	if _, err := a.TaskIntegration(ctx, task.TaskID); !api.IsCode(err, api.CodeNotFound) {
		t.Fatalf("task integration of an open review: %v", err)
	}
	raw, err := a.ReviewAggregate(ctx, "", "batch-na", own)
	if err != nil {
		t.Fatal(err)
	}
	var view board.ReviewBatchView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	request := board.ReviewCloseRequest{BatchID: "batch-na", ExpectedRevision: view.Batch.Revision, ViewHash: board.ReviewViewDigest(view), Author: "claude", Roles: map[string]board.ReviewRoleConclusion{}, ResolvedFailures: map[string]string{}, Opinions: []board.ReviewOpinion{}}
	edges, _, err := board.ReviewClosureEdges(view, request)
	if err != nil {
		t.Fatal(err)
	}
	evidence := board.ReviewGitEvidence{CWD: cwd, Head: "N/A", NotApplicable: "test", VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges}
	if _, err := a.ReviewClose(ctx, "", map[string]any{"request": request, "evidence": evidence}, own); err != nil {
		t.Fatal(err)
	}
	empty, err := f.srv.ReceiptState(a.ID(), task.TaskID, g.Claim.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := serverintegration.Step(empty, serverintegration.Completion{Closed: true, PlanNA: true, NoDelivery: true}, integrationParams(empty)); !ok {
		t.Fatal("the kernel refused a task that needs no integration")
	}
	// The task view of the same facts: the kernel accepts the empty state, and the
	// view answers not-applicable instead of not_found.
	if view, err := a.TaskIntegration(ctx, task.TaskID); err != nil || view.Phase != "not-applicable" || view.Request.Members[0].Generation != g.Claim.Generation {
		t.Fatalf("task integration of a task that needs none: %+v %v", view, err)
	}
	if got, err := f.srv.CompletionEvidence(a.ID(), task.TaskID, g.Claim.Generation); err != nil || got.Phase != "not-applicable" {
		t.Fatalf("completion evidence of a task that needs no integration: %+v %v", got, err)
	}
}
