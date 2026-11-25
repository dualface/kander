package server_test

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/statemachine/serverclaim"
	"github.com/dualface/kander/internal/statemachine/servergrouplease"
)

// leaseProbe projects the lease record of one group before and after an
// entrance and compares it with the kernel step.
type leaseProbe struct {
	t     *testing.T
	f     *fixture
	p     *client.Project
	group string
}

func (l leaseProbe) state(base *uint64) (servergrouplease.State, uint64) {
	l.t.Helper()
	st, used, err := l.f.srv.GroupLeaseState(l.p.ID(), l.group, base)
	if err != nil {
		l.t.Fatal(err)
	}
	return st, used
}

// refines runs fn and asserts projection(after) = Step(projection(before), e),
// both with the generation base fixed before fn, when code is empty, or the
// error code, a kernel refusal, and an unchanged projection.
func (l leaseProbe) refines(e servergrouplease.Event, code string, fn func() error) {
	l.t.Helper()
	before, base := l.state(nil)
	err := fn()
	after, _ := l.state(&base)
	want, ok := servergrouplease.Step(before, e, servergrouplease.Params{MaxGen: uint32(before.Gen + 1)})
	switch {
	case ok != (code == ""):
		l.t.Fatalf("kernel acceptance of %T on %+v: %v, want %v", e, before, ok, code == "")
	case code == "" && err != nil:
		l.t.Fatalf("%T: %v", e, err)
	case code == "" && after != want:
		l.t.Fatalf("%T: projection %+v, kernel %+v", e, after, want)
	case code != "" && !api.IsCode(err, code):
		l.t.Fatalf("%T on %+v: error %v, want %s", e, before, err, code)
	case code != "" && after != before:
		l.t.Fatalf("refused %T changed the projection: %+v -> %+v", e, before, after)
	}
}

// stays runs fn, which the group kernel accepts as e without changing the
// lease, but which the entrance refuses with code because of a fact outside the
// lease (a member claim, a card revision), or accepts it as a whole when code is
// empty: the projection equals the kernel step and the error is code.
func (l leaseProbe) stays(e servergrouplease.Event, code string, fn func() error) {
	l.t.Helper()
	before, base := l.state(nil)
	err := fn()
	after, _ := l.state(&base)
	want, ok := servergrouplease.Step(before, e, servergrouplease.Params{MaxGen: uint32(before.Gen + 1)})
	if !ok || after != want || after != before || (code == "") != (err == nil) || code != "" && !api.IsCode(err, code) {
		l.t.Fatalf("%T on %+v: accepted %v, after %+v, error %v, want %s", e, before, ok, after, err, code)
	}
}

// Every lease entrance of the server refines the group kernel: a claim, the
// reuse of an active lease (also after it lapsed), a renewal, observations, a
// takeover, and a release. The refusals cover theorems 1 to 4: a forged token,
// a second claim of a valid lease, a claim of the lease's own execution
// identity by another client, a takeover without a decision or with a stale
// observed generation, and every request of a taken-over, lost, or released
// lease.
func TestGroupLeaseEntrancesRefineTheKernel(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	request := groupRequest(t, a)
	group := request.Request.GroupID
	l := leaseProbe{t: t, f: f, p: a, group: group}
	ctx := t.Context()

	var first api.GroupGrant
	l.refines(servergrouplease.Claim{Hdr: true, G: 1, Tg: 1, Expected: 0, Room: true}, "", func() (err error) {
		first, err = a.ClaimGroup(ctx, "", group, request, nil)
		return err
	})
	proof := client.Claim{Token: first.Token, Generation: 1}
	other := request
	other.ExecutionID, other.ExpectedGeneration, other.Decision = "second-coordinator", 1, "user:take over"
	l.refines(servergrouplease.Claim{Hdr: true, G: 2, Tg: 2, Expected: 1, Room: true, Decision: true}, api.CodeLeaseActive, func() error {
		_, err := b.ClaimGroup(ctx, "", group, other, nil)
		return err
	})
	own := other
	own.ExecutionID = request.ExecutionID
	l.refines(servergrouplease.Claim{Hdr: true, G: 2, Tg: 2, SameExec: true, Expected: 1, Room: true, Decision: true}, api.CodeLeaseInvalid, func() error {
		_, err := b.ClaimGroup(ctx, "", group, own, nil)
		return err
	})
	// The reuse by the same client with the current proof keeps the record.
	l.refines(servergrouplease.Claim{Hdr: true, G: 1, Tg: 1, SameExec: true, SameClient: true, Expected: 2, Room: true}, "", func() error {
		_, err := a.ClaimGroup(ctx, "", group, request, &proof)
		return err
	})
	forged := client.Claim{Token: "forged", Generation: 1}
	l.refines(servergrouplease.Claim{Hdr: true, G: 1, Tg: 2, SameExec: true, SameClient: true, Expected: 2, Room: true}, api.CodeHeldByOther, func() error {
		_, err := a.ClaimGroup(ctx, "", group, request, &forged)
		return err
	})
	f.clock.advance(16 * time.Second)
	l.refines(servergrouplease.Renew{Hdr: true, G: 1, Tg: 1}, "", func() error { _, err := a.RenewGroup(ctx, group, proof); return err })
	l.refines(servergrouplease.Renew{Hdr: true, G: 1, Tg: 2}, api.CodeHeldByOther, func() error { _, err := a.RenewGroup(ctx, group, forged); return err })
	l.refines(servergrouplease.Observe{Hdr: true, G: 1, Tg: 1}, "", func() error {
		_, err := a.ObserveGroup(ctx, "", group, api.AgentObservation{Status: "alive"}, proof)
		return err
	})
	l.refines(servergrouplease.Observe{Hdr: true, G: 1, Tg: 2, Stopped: true}, api.CodeHeldByOther, func() error {
		_, err := a.ObserveGroup(ctx, "", group, api.AgentObservation{Status: "stopped"}, forged)
		return err
	})
	l.refines(servergrouplease.Observe{Hdr: true, G: 1, Tg: 1, Stopped: true}, "", func() error {
		_, err := a.ObserveGroup(ctx, "", group, api.AgentObservation{Status: "stopped"}, proof)
		return err
	})
	// A lost lease ends the holder: it cannot renew, and the view shows it.
	l.refines(servergrouplease.Renew{Hdr: true, G: 1, Tg: 1}, api.CodeLeaseInvalid, func() error { _, err := a.RenewGroup(ctx, group, proof); return err })
	if view, err := b.Group(ctx, group); err != nil || view.Claim.Status != api.ClaimLost {
		t.Fatalf("view of a lost lease: %+v %v", view.Claim, err)
	}

	// A takeover needs the observed generation and a decision, and takes the next.
	take := other
	take.Request.ExpectedRevision, take.Request.ExpectedEpoch = first.Checkpoint.Revision, first.Checkpoint.Authority.Epoch
	noDecision := take
	noDecision.Decision = ""
	l.refines(servergrouplease.Claim{Hdr: true, G: 2, Tg: 2, Expected: 1, Room: true}, api.CodeInvalidRequest, func() error {
		_, err := b.ClaimGroup(ctx, "", group, noDecision, nil)
		return err
	})
	stale := take
	stale.ExpectedGeneration = 5
	l.refines(servergrouplease.Claim{Hdr: true, G: 2, Tg: 2, Expected: 2, Room: true, Decision: true}, api.CodeLeaseInvalid, func() error {
		_, err := b.ClaimGroup(ctx, "", group, stale, nil)
		return err
	})
	var second api.GroupGrant
	l.refines(servergrouplease.Claim{Hdr: true, G: 2, Tg: 2, Expected: 1, Room: true, Decision: true}, "", func() (err error) {
		second, err = b.ClaimGroup(ctx, "", group, take, nil)
		return err
	})
	if second.Claim.Generation != 2 {
		t.Fatalf("takeover granted generation %d", second.Claim.Generation)
	}
	l.refines(servergrouplease.Renew{Hdr: true, G: 3, Tg: 3}, api.CodeTakenOver, func() error { _, err := a.RenewGroup(ctx, group, proof); return err })
	next := client.Claim{Token: second.Token, Generation: 2}
	// A release is refused like a renewal: for a forged token and for the token
	// of the current lease presented with an older generation.
	release := api.GroupRelease{GroupID: group, Reason: "handoff", Decision: "user:release"}
	l.refines(servergrouplease.Release{Hdr: true, G: 2, Tg: 3}, api.CodeHeldByOther, func() error {
		_, err := b.ReleaseGroup(ctx, "", group, release, client.Claim{Token: "forged", Generation: 2})
		return err
	})
	l.refines(servergrouplease.Release{Hdr: true, G: 3, Tg: 2}, api.CodeLeaseInvalid, func() error {
		_, err := b.ReleaseGroup(ctx, "", group, release, client.Claim{Token: second.Token, Generation: 1})
		return err
	})
	l.refines(servergrouplease.Release{Hdr: true, G: 2, Tg: 2}, "", func() error {
		_, err := b.ReleaseGroup(ctx, "", group, release, next)
		return err
	})
	l.refines(servergrouplease.Renew{Hdr: true, G: 2, Tg: 2}, api.CodeLeaseInvalid, func() error { _, err := b.RenewGroup(ctx, group, next); return err })

	// A lease that expired keeps its record and its execution.
	third := take
	third.ExecutionID, third.ExpectedGeneration, third.Request.ExpectedRevision = "third-coordinator", 2, second.Checkpoint.Revision
	third.Request.ExpectedEpoch = second.Checkpoint.Authority.Epoch
	var grant api.GroupGrant
	l.refines(servergrouplease.Claim{Hdr: true, G: 3, Tg: 3, Expected: 2, Room: true, Decision: true}, "", func() (err error) {
		grant, err = b.ClaimGroup(ctx, "", group, third, nil)
		return err
	})
	before, _ := l.state(nil)
	f.clock.advance(91 * time.Second)
	if want, _ := servergrouplease.Step(before, servergrouplease.Expire{}, servergrouplease.Params{MaxGen: 3}); func() servergrouplease.State { st, _ := l.state(nil); return st }() != want {
		t.Fatalf("lease expiry: kernel %+v", want)
	}
	if view, err := a.Group(ctx, group); err != nil || view.Claim.Status != api.ClaimLapsed || servergrouplease.Shown(func() servergrouplease.State { st, _ := l.state(nil); return st }(), servergrouplease.Params{MaxGen: 4}) != servergrouplease.StatusLapsed {
		t.Fatalf("view of an expired lease: %+v %v", view.Claim, err)
	}
	// A lapsed lease keeps its holder: the same execution reuses it at the same
	// generation.
	reuse := client.Claim{Token: grant.Token, Generation: 3}
	l.refines(servergrouplease.Claim{Hdr: true, G: 3, Tg: 3, SameExec: true, SameClient: true, Expected: 4, Room: true}, "", func() error {
		_, err := b.ClaimGroup(ctx, "", group, third, &reuse)
		return err
	})
	if view, err := a.Group(ctx, group); err != nil || view.Claim.Status != api.ClaimActive || view.Claim.Generation != 3 {
		t.Fatalf("view of a reused lease: %+v %v", view.Claim, err)
	}
}

// A generation beyond the kernel's uint32 parameters keeps every decision: the
// holder renews, an older header is refused, a takeover after expiry takes the
// next generation, and the last uint64 generation takes no successor.
func TestLargeGroupGenerationKeepsDecisions(t *testing.T) {
	for _, gen := range []uint64{1 << 31, 1 << 40, math.MaxUint64 - 2, math.MaxUint64} {
		f := newFixture(t)
		a := f.project("host-a")
		request := groupRequest(t, a)
		group := request.Request.GroupID
		l := leaseProbe{t: t, f: f, p: a, group: group}
		token, err := f.srv.SeedGroupLease(a.ID(), group, "host-a", "exec-big", gen, f.clock.now().Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		own := client.Claim{Token: token, Generation: gen}
		l.refines(servergrouplease.Renew{Hdr: true, G: 1, Tg: 1}, "", func() error { _, err := a.RenewGroup(t.Context(), group, own); return err })
		l.refines(servergrouplease.Renew{Hdr: true, G: 2, Tg: 1}, api.CodeLeaseInvalid, func() error {
			_, err := a.RenewGroup(t.Context(), group, client.Claim{Token: token, Generation: gen - 1})
			return err
		})
		f.clock.advance(2 * time.Hour)
		take := request
		take.ExecutionID, take.ExpectedGeneration, take.Decision = "exec-next", gen, "user:take over"
		if gen == math.MaxUint64 {
			l.refines(servergrouplease.Claim{Hdr: true, G: 2, Tg: 2, Expected: 1, Room: false, Decision: true}, api.CodeLeaseInvalid, func() error {
				_, err := a.ClaimGroup(t.Context(), "", group, take, nil)
				return err
			})
			continue
		}
		var grant api.GroupGrant
		l.refines(servergrouplease.Claim{Hdr: true, G: 2, Tg: 2, Expected: 1, Room: true, Decision: true}, "", func() (err error) {
			grant, err = a.ClaimGroup(t.Context(), "", group, take, nil)
			return err
		})
		if grant.Claim.Generation != gen+1 {
			t.Fatalf("generation %d: takeover granted %d", gen, grant.Claim.Generation)
		}
	}
}

// memberProbe projects the claim record of one claimed task.
type memberProbe struct {
	t    *testing.T
	f    *fixture
	p    *client.Project
	task string
}

func (m memberProbe) state(base *uint64) (serverclaim.State, uint64) {
	m.t.Helper()
	st, used, err := m.f.srv.MemberClaimState(m.p.ID(), m.task, base)
	if err != nil {
		m.t.Fatal(err)
	}
	return st, used
}

// A lost member recovery refines both kernels: the group's recoverLost event
// and the member claim's audit event. The refusals cover theorem 7: a live
// member, a stale observed generation, a reused execution identity, a stale
// card revision, an observation that is not unknown, and a missing decision.
func TestLostMemberRecoveryRefinesTheKernels(t *testing.T) {
	r := newLostRecoveryFixture(t)
	group, task := r.input.Request.GroupID, r.task.TaskID
	l := leaseProbe{t: t, f: r.fixture, p: r.coordinator, group: group}
	m := memberProbe{t: t, f: r.fixture, p: r.owner, task: task}
	recover := func(input api.GroupRecover) func() error {
		return func() error {
			_, err := r.coordinator.CoordinatorAction(t.Context(), "", group, "recover", input, r.proof)
			return err
		}
	}
	lost := servergrouplease.RecoverLost{Hdr: true, G: 1, Tg: 1, Decision: true, Unknown: true, ExecOk: true}
	gen := r.taskGrant.Claim.Generation
	// refused runs a recovery the group kernel accepts and the member claim
	// kernel refuses: the member claim stays as it is.
	refused := func(audit serverclaim.Audit, code string, input api.GroupRecover) {
		t.Helper()
		before, _ := m.state(nil)
		if _, ok := serverclaim.Step(before, audit, serverclaim.Params{MaxGen: uint32(before.Gen + 1)}); ok {
			t.Fatalf("the claim kernel accepted %+v on %+v", audit, before)
		}
		l.stays(lost, code, recover(input))
		if after, _ := m.state(nil); after != before {
			t.Fatalf("a refused recovery changed the member claim: %+v -> %+v", before, after)
		}
	}

	refused(serverclaim.Audit{Expected: gen, Fresh: true}, api.CodeLeaseActive, r.input)
	r.expireMember(t)
	staleLease := *r.input.Lost
	staleLease.ExpectedGeneration++
	wrong := r.input
	wrong.Lost = &staleLease
	refused(serverclaim.Audit{Expected: gen + 1, Fresh: true}, api.CodeLeaseInvalid, wrong)
	reused := r.input
	reused.Lost = &api.RecoveryLease{ExecutionID: r.taskGrant.Claim.ExecutionID, ExpectedGeneration: gen}
	refused(serverclaim.Audit{Expected: gen}, api.CodeLeaseInvalid, reused)
	conflict := r.input
	conflict.ObservedRevision++
	l.stays(lost, api.CodeRevisionConflict, recover(conflict))
	noDecision := r.input
	noDecision.Request.Decision = ""
	l.refines(servergrouplease.RecoverLost{Hdr: true, G: 1, Tg: 1, Unknown: true, ExecOk: true}, api.CodeInvalidRequest, recover(noDecision))
	stopped := r.input
	stopped.Observation.Status = "stopped"
	l.refines(servergrouplease.RecoverLost{Hdr: true, G: 1, Tg: 1, Decision: true, ExecOk: true}, api.CodeInvalidRequest, recover(stopped))

	// The accepted recovery moves the member claim to an audit generation: one
	// above the observed one, released, and without a lease.
	before, base := m.state(nil)
	want, ok := serverclaim.Step(before, serverclaim.Audit{Expected: gen, Fresh: true}, serverclaim.Params{MaxGen: uint32(before.Gen + 1)})
	if !ok {
		t.Fatalf("the claim kernel refused the audit of an expired lease: %+v", before)
	}
	l.stays(lost, "", recover(r.input))
	if got, _ := m.state(&base); got != want || got.Status != serverclaim.StatusReleased || got.Gen != before.Gen+1 || got.Live {
		t.Fatalf("audit generation: %+v, kernel %+v", got, want)
	}
}

// An owned recovery refines the group kernel's recoverOwned event: a member
// card in working or review needs its holder and a fresh observation ticket.
// The kernel also refuses every state when the observation is not `stopped`
// and a start is unfinished; those cases are asserted on the kernel only. The
// check after a write, which refuses a transaction whose member cards lack a
// holder (theorems 5 and 6), runs through groupWrite in
// TestGroupWriteAfterRefusesChangedMembersWithoutHolder.
func TestOwnedRecoveryRefinesTheKernel(t *testing.T) {
	r := newLostRecoveryFixture(t, true)
	group := r.input.Request.GroupID
	l := leaseProbe{t: t, f: r.fixture, p: r.coordinator, group: group}
	input := r.input
	input.Lost = nil
	input.Observation.Status = "stopped"
	recover := func() error {
		_, err := r.coordinator.CoordinatorAction(t.Context(), "", group, "recover", input, r.proof)
		return err
	}
	r.proof.Members = map[string]api.ClaimProof{r.task.TaskID: {Token: r.taskGrant.Token, Generation: r.taskGrant.Claim.Generation}}
	facts := servergrouplease.RecoverOwned{Hdr: true, G: 1, Tg: 1, Stopped: true, Claimed: true, Member: true, Ticket: true}
	noTicket := facts
	noTicket.Ticket = false
	l.refines(noTicket, api.CodeEvidenceIncomplete, recover)
	forgedMember := r.proof
	forgedMember.Members = map[string]api.ClaimProof{r.task.TaskID: {Token: strings.Repeat("0", 64), Generation: r.taskGrant.Claim.Generation}}
	noMember := facts
	noMember.Member = false
	l.refines(noMember, api.CodeHeldByOther, func() error {
		_, err := r.coordinator.CoordinatorAction(t.Context(), "", group, "recover", input, forgedMember)
		return err
	})
	state, err := r.owner.ExecutionState(t.Context(), r.task.TaskID, client.Claim{Token: r.taskGrant.Token, Generation: r.taskGrant.Claim.Generation})
	if err != nil {
		t.Fatal(err)
	}
	input.ObservationTicket = state.ObservationTicket
	l.refines(facts, "", recover)

	// The kernel side of the other refusals, on the lease the server holds.
	st, _ := l.state(nil)
	params := servergrouplease.Params{MaxGen: uint32(st.Gen + 1)}
	for name, event := range map[string]servergrouplease.Event{
		"not stopped": servergrouplease.RecoverOwned{Hdr: true, G: 1, Tg: 1, Claimed: false, Member: true, Ticket: true},
		"pending":     servergrouplease.RecoverOwned{Hdr: true, G: 1, Tg: 1, Stopped: true, Member: true, Ticket: true, Pending: true},
		"members":     servergrouplease.WriteAfter{Hdr: true, G: 1, Tg: 1},
		"no headers":  servergrouplease.WriteAfter{G: 1, Tg: 1, Members: true},
	} {
		if _, ok := servergrouplease.Step(st, event, params); ok {
			t.Errorf("the group kernel accepted %s on %+v", name, st)
		}
	}
	if _, ok := servergrouplease.Step(st, servergrouplease.WriteAfter{Hdr: true, G: 1, Tg: 1, Members: true}, params); !ok {
		t.Errorf("the group kernel refused a write of the group holder: %+v", st)
	}
}

// A group write refines the require event before and inside its transaction
// and refuses a request of an older or foreign coordinator before any change.
func TestGroupWriteRefinesTheKernel(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	request := groupRequest(t, a)
	group := request.Request.GroupID
	l := leaseProbe{t: t, f: f, p: a, group: group}
	first, err := a.ClaimGroup(t.Context(), "", group, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	proof := client.Claim{Token: first.Token, Generation: 1}
	reconcile := board.CoordinatorReconcile{GroupID: group, ExpectedRevision: first.Checkpoint.Revision, Authority: first.Checkpoint.Authority, Members: map[string]board.CoordinatorObservation{}}
	for id, member := range first.Checkpoint.Members {
		reconcile.Members[id] = board.CoordinatorObservation{Revision: member.Revision}
	}
	input := api.GroupReconcile{Request: reconcile}
	forged := client.Claim{Token: "forged", Generation: 1}
	l.refines(servergrouplease.Require{Hdr: true, G: 1, Tg: 2}, api.CodeHeldByOther, func() error {
		_, err := b.CoordinatorAction(t.Context(), "", group, "reconcile", input, forged)
		return err
	})
	l.refines(servergrouplease.WriteAfter{Hdr: true, G: 1, Tg: 1, Members: true}, "", func() error {
		_, err := a.CoordinatorAction(t.Context(), "", group, "reconcile", input, proof)
		return err
	})
	f.clock.advance(91 * time.Second)
	l.refines(servergrouplease.WriteAfter{Hdr: true, G: 1, Tg: 1, Members: true}, "", func() error {
		_, err := a.CoordinatorAction(t.Context(), "", group, "reconcile", input, proof)
		return err
	})
}

// The check after a group write refuses a transaction that changed a member
// card without its holder, and the transaction is rolled back (theorems 5 and
// 6). The recover route stops such a request earlier, in its readiness check,
// so the request goes through the guard of the group write alone: the group
// lease holds, the member card was in working before the transaction, and the
// member's proof is forged.
func TestGroupWriteAfterRefusesChangedMembersWithoutHolder(t *testing.T) {
	r := newLostRecoveryFixture(t, true)
	group := r.input.Request.GroupID
	l := leaseProbe{t: t, f: r.fixture, p: r.coordinator, group: group}
	input := r.input
	input.Lost = nil
	input.Observation.Status = "stopped"
	member := r.task.TaskID
	valid := map[string]api.ClaimProof{member: {Token: r.taskGrant.Token, Generation: r.taskGrant.Claim.Generation}}
	forged := map[string]api.ClaimProof{member: {Token: strings.Repeat("0", 64), Generation: r.taskGrant.Claim.Generation}}
	write := func(key string, members map[string]api.ClaimProof) func() error {
		return func() error {
			return r.fixture.srv.GroupRecoverUnchecked(t.Context(), r.coordinator.ID(), key, input, r.proof.Token, r.proof.Generation, members)
		}
	}
	snapshot := func() (api.Task, api.GroupView) {
		t.Helper()
		task, err := r.coordinator.Show(t.Context(), member)
		if err != nil {
			t.Fatal(err)
		}
		view, err := r.coordinator.Group(t.Context(), group)
		if err != nil {
			t.Fatal(err)
		}
		return task, view
	}
	if task, _ := snapshot(); task.State != "working" {
		t.Fatalf("the member card is %s before the transaction", task.State)
	}
	taskBefore, viewBefore := snapshot()

	// The group holder is checked first and passes; the member check refuses.
	l.refines(servergrouplease.WriteAfter{Hdr: true, G: 1, Tg: 1}, api.CodeHeldByOther, write("after-guard-forged", forged))
	taskAfter, viewAfter := snapshot()
	if !reflect.DeepEqual(taskBefore, taskAfter) || !reflect.DeepEqual(viewBefore, viewAfter) {
		t.Fatalf("the refused transaction committed: card %+v -> %+v, group %+v -> %+v", taskBefore, taskAfter, viewBefore, viewAfter)
	}
	if len(viewAfter.Checkpoint.Recoveries) != 0 {
		t.Fatalf("the refused transaction recorded a recovery: %+v", viewAfter.Checkpoint.Recoveries)
	}

	// The same request with the member's holder passes the check and commits.
	l.refines(servergrouplease.WriteAfter{Hdr: true, G: 1, Tg: 1, Members: true}, "", write("after-guard-valid", valid))
	if _, view := snapshot(); len(view.Checkpoint.Recoveries) != 1 {
		t.Fatalf("the accepted transaction recorded %d recoveries", len(view.Checkpoint.Recoveries))
	}
}
