package board

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/statemachine/coordinator"
	"github.com/dualface/kander/internal/statemachine/coordmember"
	"github.com/dualface/kander/internal/statemachine/wrapupwitness"
)

// The tests of this file drive each production entrance that runs a coordinator
// kernel (formal/KanderFormal/Machines/Coordinator.lean, CoordinatorMember.lean,
// WrapUpWitness.lean) on a temporary board and assert the refinement: the
// projection after a write equals the kernel step from the projection before it,
// and a refused entrance returns an error, a kernel refusal of the same event,
// and leaves the projection alone. Each theorem of the three machines has a
// refused scenario here, named in the comment above it.

func groupState(c CoordinatorCheckpoint) coordinator.State {
	return newGroupKernel(c, true, len(c.Members)).state
}

func mirrorState(m CoordinatorMember) coordmember.State { return newMirrorKernel(m).state }

func storedCheckpoint(t *testing.T, root string) CoordinatorCheckpoint {
	t.Helper()
	c, err := ReadCoordinatorCheckpoint(root, coordinatorGroup)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// requireCheckpointUnchanged requires that a refused entrance left the checkpoint, and so
// its projection, as it was.
func requireCheckpointUnchanged(t *testing.T, root string, before CoordinatorCheckpoint) {
	t.Helper()
	if after := storedCheckpoint(t, root); !reflect.DeepEqual(before, after) {
		t.Fatalf("a refused entrance changed the checkpoint: %+v -> %+v", before, after)
	}
}

// Theorems 1 and 2: a claim creates the checkpoint at revision 1 and epoch 1, an
// identical claim replays, and a takeover raises both by one.
func TestCoordinatorClaimRefinesKernel(t *testing.T) {
	root := tempBoard(t)
	id := coordinatorCard(t, root, "claim").Entry.TaskID
	r := CoordinatorClaim{GroupID: coordinatorGroup, Owner: "coordinator", Token: "session-one", Basis: "basis", Members: []string{id}}
	g := newGroupKernel(CoordinatorCheckpoint{}, false, 1)
	want, ok := g.step(g.claim(r, false, false, true, true))
	if !ok {
		t.Fatal("the kernel refused the first claim")
	}
	c, err := ClaimCoordinator(context.Background(), root, r)
	if err != nil || groupState(c) != want || c.Revision != 1 || c.Authority.Epoch != 1 {
		t.Fatalf("%+v %v want %+v", c, err, want)
	}

	// A replay returns the checkpoint as it is.
	g = newGroupKernel(c, true, 1)
	if next, ok := g.step(g.claim(r, true, true, true, true)); !ok || next != g.state {
		t.Fatalf("replay: %+v %v", next, ok)
	}
	if replay, err := ClaimCoordinator(context.Background(), root, r); err != nil || !reflect.DeepEqual(replay, c) {
		t.Fatalf("replay changed the checkpoint: %v", err)
	}

	// A takeover names the revision and epoch it saw and raises both.
	take := CoordinatorClaim{GroupID: coordinatorGroup, ExpectedRevision: c.Revision, ExpectedEpoch: c.Authority.Epoch, Owner: "successor", Token: "session-two", Basis: "basis", Members: []string{id}}
	want, ok = g.step(g.claim(take, false, false, true, true))
	if !ok {
		t.Fatal("the kernel refused the takeover")
	}
	next, err := ClaimCoordinator(context.Background(), root, take)
	if err != nil || groupState(next) != want || next.Revision != 2 || next.Authority.Epoch != 2 || next.LastRequestHash != "" {
		t.Fatalf("%+v %v want %+v", next, err, want)
	}
}

// Theorem 1: every claim refusal returns the error, is a kernel refusal, and
// writes nothing.
func TestCoordinatorClaimRefusals(t *testing.T) {
	root := tempBoard(t)
	id := coordinatorCard(t, root, "claim-refused").Entry.TaskID
	first := CoordinatorClaim{GroupID: coordinatorGroup, ExpectedRevision: 1, Owner: "coordinator", Token: "session-one", Basis: "basis", Members: []string{id}}
	if _, err := ClaimCoordinator(context.Background(), root, first); err == nil {
		t.Fatal("a first claim named a revision")
	} else if !strings.Contains(err.Error(), "claim CAS") {
		t.Fatal(err)
	}
	g := newGroupKernel(CoordinatorCheckpoint{}, false, 1)
	if _, ok := g.step(g.claim(first, false, false, true, true)); ok {
		t.Fatal("the kernel took a first claim at revision 1")
	}
	first.ExpectedRevision = 0
	c, err := ClaimCoordinator(context.Background(), root, first)
	if err != nil {
		t.Fatal(err)
	}
	other := coordinatorCard(t, root, "claim-late").Entry.TaskID
	good := CoordinatorClaim{GroupID: coordinatorGroup, ExpectedRevision: c.Revision, ExpectedEpoch: c.Authority.Epoch, Owner: "successor", Token: "session-two", Basis: "basis", Members: []string{id}}
	g = newGroupKernel(c, true, 1)
	for name, tc := range map[string]struct {
		change func(*CoordinatorClaim)
		fact   func(*coordinator.Claim)
		msg    string
	}{
		"stale revision": {func(r *CoordinatorClaim) { r.ExpectedRevision-- }, nil, "claim CAS"},
		"stale epoch":    {func(r *CoordinatorClaim) { r.ExpectedEpoch++ }, nil, "claim CAS"},
		"same token":     {func(r *CoordinatorClaim) { r.Token = "session-one" }, nil, "claim CAS"},
		"new members":    {func(r *CoordinatorClaim) { r.Members = []string{id, other} }, func(e *coordinator.Claim) { e.MembersOk = false }, "cannot change membership"},
		"group changed":  {func(r *CoordinatorClaim) {}, func(e *coordinator.Claim) { e.Membership = false }, "membership changed"},
	} {
		t.Run(name, func(t *testing.T) {
			r := good
			tc.change(&r)
			if _, err := ClaimCoordinator(context.Background(), root, r); err == nil || !strings.Contains(err.Error(), tc.msg) {
				t.Fatalf("%v", err)
			}
			e := g.claim(r, false, r.Token == "session-one", true, true)
			if tc.fact != nil {
				tc.fact(&e)
			}
			if next, ok := g.step(e); ok || next != g.state {
				t.Fatalf("the kernel took the claim: %+v", next)
			}
			requireCheckpointUnchanged(t, root, c)
		})
	}
}

// Theorem 1 at the ceilings and for large values: the projection keeps the
// decisions at 2^40 and at the last uint64, where the claim has no room.
func TestCoordinatorKernelLargeCounters(t *testing.T) {
	for _, rev := range []uint64{1<<31 - 1, 1 << 31, 1 << 40, math.MaxUint64 - 1, math.MaxUint64} {
		c := CoordinatorCheckpoint{Revision: rev, Authority: CoordinatorAuthority{Epoch: rev / 2, Token: "t"}, ClaimHash: "h", Members: map[string]CoordinatorMember{"a": {}}}
		g := newGroupKernel(c, true, 1)
		r := CoordinatorClaim{ExpectedRevision: rev, ExpectedEpoch: rev / 2, Token: "other"}
		next, ok := g.step(g.claim(r, false, false, true, true))
		room := rev != math.MaxUint64
		if ok != room {
			t.Fatalf("rev %d: accepted=%v want %v", rev, ok, room)
		}
		if ok && (g.revOut(next.Rev) != rev+1 || g.epochOut(next.Epoch) != rev/2+1) {
			t.Fatalf("rev %d: %d %d", rev, g.revOut(next.Rev), g.epochOut(next.Epoch))
		}
		for _, wrong := range []uint64{0, rev - 1, rev + 1, math.MaxUint64 - 7, 1 << 63} {
			if wrong == rev {
				continue
			}
			bad := r
			bad.ExpectedRevision = wrong
			if _, ok := g.step(g.claim(bad, false, false, true, true)); ok {
				t.Fatalf("rev %d: the kernel took revision %d", rev, wrong)
			}
		}
	}
	// A card revision keeps its order from 2^31 on, and a chain position below the
	// mirror's is refused wherever it sits.
	for _, rev := range []uint64{5, 1 << 31, 1 << 40, math.MaxUint64 - 2} {
		k := newMirrorKernel(CoordinatorMember{Revision: rev, TransitionSeq: rev})
		if k.refuseRevision(rev) || k.refuseRevision(rev+1) || !k.refuseRevision(rev-1) {
			t.Fatalf("revision order lost at %d", rev)
		}
		if k.refuseChain(rev+1, false) || !k.refuseChain(rev-1, false) || !k.refuseChain(rev, true) {
			t.Fatalf("chain order lost at %d", rev)
		}
	}
}

// Theorem 3: a reconcile judges each member alone. The blocked member keeps its
// mirror, the other advances, and the checkpoint step is the kernel's.
func TestCoordinatorReconcileRefinesKernels(t *testing.T) {
	root := tempBoard(t)
	ids := []string{recoveryMember(t, root, "refine-a"), recoveryMember(t, root, "refine-b")}
	c0 := coordinatorReconcile(t, root, coordinatorClaim(t, root, ids...))
	staleReclaim(t, root, ids[0])
	moveMember(t, root, ids[1], "review")
	r := coordinatorRequest(t, root, c0)
	c1, err := ReconcileCoordinator(context.Background(), root, r, nil)
	if err != nil {
		t.Fatal(err)
	}
	g := newGroupKernel(c0, true, 2)
	want, ok := g.step(g.reconcile(c0, r, reconcileFacts{membersOk: true, membership: true, recoveries: true, changed: true, blockedAfter: 1}))
	if !ok || groupState(c1) != want || c1.Revision != c0.Revision+1 || c1.Authority.Epoch != c0.Authority.Epoch {
		t.Fatalf("%+v %+v", groupState(c1), want)
	}
	// The blocked member: the kernel's block, nothing else moved.
	blocked := mirrorState(c0.Members[ids[0]])
	if next, _ := coordmember.Step(blocked, coordmember.Block{}, newMirrorKernel(c0.Members[ids[0]]).params); next != mirrorState(c1.Members[ids[0]]) {
		t.Fatalf("%+v %+v", next, mirrorState(c1.Members[ids[0]]))
	}
	// The other member: the kernel's acceptance of the walk it ran.
	s := transactionSnapshot(t, root, ids[1])
	k := newMirrorKernel(c0.Members[ids[1]])
	if next, ok := k.accept(s, c0.Members[ids[1]], c1.Members[ids[1]]); !ok || next != mirrorState(c1.Members[ids[1]]) || c1.Members[ids[1]].State != "review" {
		t.Fatalf("%+v %v", next, ok)
	}

	// Theorem 5: while the chain misses its handoff, every reconcile blocks the
	// member again and the mirror stays the one the block kept.
	for range 3 {
		again, err := ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c1), nil)
		if err != nil || !reflect.DeepEqual(again, c1) {
			t.Fatalf("a repeated block changed the checkpoint: %v", err)
		}
	}
	g = newGroupKernel(c1, true, 2)
	if next, ok := g.step(g.reconcile(c1, coordinatorRequest(t, root, c1), reconcileFacts{membersOk: true, membership: true, recoveries: true, blockedAfter: 1})); !ok || next != g.state {
		t.Fatalf("an unchanged reconcile wrote: %+v", next)
	}
	if listed := map[string]string{}; ReadBlockedMembers(context.Background(), root, coordinatorGroup, listed) != nil || len(listed) != int(groupState(c1).Blocked) {
		t.Fatalf("blocked listing %v", listed)
	}
}

// Theorem 1: a retry of the last request returns the checkpoint it produced, and
// another request at an old revision is refused.
func TestCoordinatorReconcileRetryAndRefusals(t *testing.T) {
	root := tempBoard(t)
	ids := []string{recoveryMember(t, root, "retry-a"), recoveryMember(t, root, "retry-b")}
	c0 := coordinatorReconcile(t, root, coordinatorClaim(t, root, ids...))
	moveMember(t, root, ids[0], "review")
	r := coordinatorRequest(t, root, c0)
	c1, err := ReconcileCoordinator(context.Background(), root, r, nil)
	if err != nil || c1.Revision != c0.Revision+1 {
		t.Fatalf("%+v %v", c1, err)
	}
	g := newGroupKernel(c1, true, 2)
	if next, ok := g.step(g.reconcile(c1, r, reconcileFacts{membersOk: true, membership: true, recoveries: true})); !ok || next != g.state {
		t.Fatalf("the retry of the last request: %+v %v", next, ok)
	}
	if retry, err := ReconcileCoordinator(context.Background(), root, r, nil); err != nil || !reflect.DeepEqual(retry, c1) {
		t.Fatalf("retry: %v", err)
	}

	stale := r
	stale.Members = map[string]CoordinatorObservation{ids[0]: r.Members[ids[0]], ids[1]: r.Members[ids[1]]}
	o := stale.Members[ids[0]]
	o.Base = strings.Repeat("c", 40)
	stale.Members[ids[0]] = o
	forged := r
	forged.Authority.Token = "someone-else"
	owner := r
	owner.Authority.Owner = "someone-else"
	epoch := r
	epoch.Authority.Epoch++
	for name, tc := range map[string]struct {
		request CoordinatorReconcile
		msg     string
	}{
		"another request at an old revision": {stale, "checkpoint CAS"},
		"token":                              {forged, "stale coordinator authority"},
		"owner":                              {owner, "stale coordinator authority"},
		"epoch":                              {epoch, "stale coordinator authority"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ReconcileCoordinator(context.Background(), root, tc.request, nil); err == nil || !strings.Contains(err.Error(), tc.msg) {
				t.Fatalf("%v", err)
			}
			requireCheckpointUnchanged(t, root, c1)
			if strings.Contains(tc.msg, "authority") {
				if !g.refuseAuthority(c1, tc.request) {
					t.Fatal("the kernel took the authority")
				}
			} else if !g.refuseRevision(c1, tc.request) {
				t.Fatal("the kernel took the revision")
			}
		})
	}

	// A request that failed at request level (a stale card revision of one member)
	// is refused whole: no member reads as blocked for it.
	moveMember(t, root, ids[1], "review")
	if _, err := ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c1), nil); err != nil {
		t.Fatal(err)
	}
	c2 := storedCheckpoint(t, root)
	late := coordinatorRequest(t, root, c2)
	late.Members[ids[1]] = CoordinatorObservation{Revision: late.Members[ids[1]].Revision - 1}
	if _, err := ReconcileCoordinator(context.Background(), root, late, nil); err == nil {
		t.Fatal("a stale observation was a verdict")
	}
	if !newGroupKernel(c2, true, 2).refuseRequest(c2, late) {
		t.Fatal("the kernel took a request with a request-level failure")
	}
	requireCheckpointUnchanged(t, root, c2)
}

// Theorem 3: a reconcile that leaves every member blocked is committed, and the
// checkpoint then reports it.
func TestCoordinatorAllBlockedIsCommitted(t *testing.T) {
	root := tempBoard(t)
	id := recoveryMember(t, root, "all-blocked")
	c := coordinatorReconcile(t, root, coordinatorClaim(t, root, id))
	staleReclaim(t, root, id)
	r := coordinatorRequest(t, root, c)
	next, err := ReconcileCoordinator(context.Background(), root, r, nil)
	var blocked *CoordinatorBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("%v", err)
	}
	g := newGroupKernel(c, true, 1)
	want, ok := g.step(g.reconcile(c, r, reconcileFacts{membersOk: true, membership: true, recoveries: true, changed: true, blockedAfter: 1}))
	if !ok || groupState(next) != want || !allBlocked(next) || allBlocked(c) {
		t.Fatalf("%+v %v", want, ok)
	}
}

// Theorem 4: the card of a member below its mirror, and a chain that ended below
// the mirror's position, are refused.
func TestCoordinatorMemberRefusesRegression(t *testing.T) {
	root := tempBoard(t)
	ids := []string{recoveryMember(t, root, "regress-a"), recoveryMember(t, root, "regress-b")}
	c := coordinatorReconcile(t, root, coordinatorClaim(t, root, ids...))
	old := c.Members[ids[0]]
	old.Revision += 5
	err := WithTransaction(root, LockScope{Tasks: ids, Groups: []string{taskStartGroup, reviewControlGroup, dispatchRegistry}, ReadOnly: true}, func(tx *Transaction) error {
		s, e := tx.Snapshot(ids[0])
		if e != nil {
			return e
		}
		if !newMirrorKernel(old).refuseRevision(s.Revision) {
			t.Fatal("the kernel took a card below its mirror")
		}
		_, e = reconcileCoordinatorMember(context.Background(), tx, &coordinatorPass{}, ids[0], old, CoordinatorObservation{Revision: s.Revision})
		if e == nil || !strings.Contains(e.Error(), "member revision or execution cycle changed") {
			t.Fatalf("%v", e)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	m := c.Members[ids[1]]
	m.TransitionSeq = ledgerOf(t, root, ids[1]).end() + 3
	if _, err := coordinatorChainPosition(ledgerOf(t, root, ids[1]), m); err == nil || !strings.Contains(err.Error(), "execution chain regressed") {
		t.Fatalf("%v", err)
	}
	if !newMirrorKernel(m).refuseChain(m.TransitionSeq-3, false) {
		t.Fatal("the kernel took a chain below the mirror")
	}
	// A mirror that moves on accepts: the position follows the chain.
	m.TransitionSeq = ledgerOf(t, root, ids[1]).end()
	if pos, err := coordinatorChainPosition(ledgerOf(t, root, ids[1]), m); err != nil || pos != m.TransitionSeq {
		t.Fatalf("%d %v", pos, err)
	}
}

// Theorem 6: a recovery of a blocked member raises the revision by one, clears
// the block, and leaves one member less blocked: the kernel's step for the group
// and for the member, in both modes.
func TestCoordinatorRecoverRefinesKernels(t *testing.T) {
	root := tempBoard(t)
	ids, c := blockedGroup(t, root)
	s := transactionSnapshot(t, root, ids[0])
	r := recoverRequest(c, ids[0])
	g := newGroupKernel(c, true, 2)
	want, ok := g.step(g.recoverEvent(r, c, true, true, true, true))
	if !ok {
		t.Fatal("the kernel refused the recovery")
	}
	next, err := RecoverCoordinator(context.Background(), root, r, s.Revision)
	if err != nil || groupState(next) != want || next.Revision != c.Revision+1 || next.Authority.Epoch != c.Authority.Epoch || next.LastRequestHash != "" {
		t.Fatalf("%+v %v want %+v", groupState(next), err, want)
	}
	if got := mirrorState(next.Members[ids[0]]); got.Blocked || got.Confirmed || got.Col != coordmember.ColWorking {
		t.Fatalf("a reset leaves a blocked or confirmed member: %+v", got)
	}
	k := newMirrorKernel(c.Members[ids[0]])
	if step, ok := k.recoverReset(s, next.Members[ids[0]], true); !ok || step != mirrorState(next.Members[ids[0]]) {
		t.Fatalf("%+v %v want %+v", step, ok, mirrorState(next.Members[ids[0]]))
	}
	// Theorem 1: a retry that read the revision before the recovery is refused.
	if _, err := ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), nil); err == nil {
		t.Fatal("a request that read the revision before the recovery was accepted")
	}
}

// Theorem 6: a confirmation writes no card, rebinds the mirror to the card's
// column and cycle, and lets the next reconcile take the short path.
func TestCoordinatorConfirmRefinesKernels(t *testing.T) {
	root := tempBoard(t)
	ids := []string{recoveryMember(t, root, "confirm-k-a"), recoveryMember(t, root, "confirm-k-b")}
	c := coordinatorReconcile(t, root, coordinatorClaim(t, root, ids...))
	gatePlan(t, root, ids, noReviewRequirements())
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{}); err != nil {
		t.Fatal(err)
	}
	staleReclaim(t, root, ids[0])
	if err := WithTransaction(root, LockScope{Tasks: []string{ids[0]}, Groups: []string{reviewControlGroup}, ExclusiveBoard: true}, func(tx *Transaction) error {
		return tx.Relocate(ids[0], "done")
	}); err != nil {
		t.Fatal(err)
	}
	c = coordinatorRequestAfter(t, root, c)
	if c.Members[ids[0]].Blocked == nil {
		t.Fatal("no block")
	}
	s := transactionSnapshot(t, root, ids[0])
	r := recoverRequest(c, ids[0])
	next, err := RecoverCoordinator(context.Background(), root, r, s.Revision)
	if err != nil {
		t.Fatal(err)
	}
	g := newGroupKernel(c, true, 2)
	if want, ok := g.step(g.recoverEvent(r, c, true, true, true, true)); !ok || groupState(next) != want {
		t.Fatalf("%+v %v", want, ok)
	}
	m := next.Members[ids[0]]
	k := newMirrorKernel(c.Members[ids[0]])
	if step, ok := k.recoverConfirm(s, m, true, true); !ok || step != mirrorState(m) || !step.Confirmed || step.Blocked || step.Col != coordmember.ColDone {
		t.Fatalf("%+v %v want %+v", step, ok, mirrorState(m))
	}
	// The short path: the confirmed cycle of a finished card accepts without any
	// other check.
	after := coordinatorReconcile(t, root, next)
	if _, ok := newMirrorKernel(next.Members[ids[0]]).acceptConfirmed(transactionSnapshot(t, root, ids[0]), next.Members[ids[0]]); !ok || len(after.BlockedMembers()) != 0 {
		t.Fatalf("%+v", after.Members[ids[0]])
	}
}

// Theorem 6: the refusals of a recovery return the error, are kernel refusals,
// and write nothing.
func TestCoordinatorRecoverKernelRefusals(t *testing.T) {
	root := tempBoard(t)
	ids, c := blockedGroup(t, root)
	g := newGroupKernel(c, true, 2)
	good := recoverRequest(c, ids[0])
	revision := transactionRevision(t, root, ids[0])
	stale, fenced := good, good
	stale.ExpectedRevision--
	fenced.ExpectedEpoch++
	for name, r := range map[string]CoordinatorRecover{"stale revision": stale, "fenced epoch": fenced} {
		t.Run(name, func(t *testing.T) {
			if _, err := RecoverCoordinator(context.Background(), root, r, revision); err == nil || !strings.Contains(err.Error(), "recover CAS") {
				t.Fatalf("%v", err)
			}
			if _, ok := g.probe().step(g.recoverEvent(r, c, true, true, true, true)); ok {
				t.Fatal("the kernel took the recovery")
			}
			requireCheckpointUnchanged(t, root, c)
		})
	}
	// A member that is not blocked is refused before anything else, by its mirror.
	if _, err := RecoverCoordinator(context.Background(), root, recoverRequest(c, ids[1]), transactionRevision(t, root, ids[1])); err == nil || !strings.Contains(err.Error(), ids[1]) {
		t.Fatalf("%v", err)
	}
	if !newMirrorKernel(c.Members[ids[1]]).refuseBlocked() || newMirrorKernel(c.Members[ids[0]]).refuseBlocked() {
		t.Fatal("the mirror kernel misjudged which member is blocked")
	}
	if _, ok := g.step(g.recoverEvent(good, c, true, false, true, true)); ok {
		t.Fatal("the kernel recovered a member that is not blocked")
	}
	// A card in backlog or todo has no execution to recover, in either mode.
	k := newMirrorKernel(c.Members[ids[0]])
	for _, col := range []coordmember.Col{coordmember.ColEarly} {
		if _, ok := k.step(coordmember.RecoverReset{CardCol: col, NewRev: k.state.Rev, Ok: true}); ok {
			t.Fatal("reset of an early column")
		}
		if _, ok := k.step(coordmember.RecoverConfirm{CardCol: col, NewRev: k.state.Rev}); ok {
			t.Fatal("confirm of an early column")
		}
	}
	// Each mode takes only its own columns.
	if _, ok := k.step(coordmember.RecoverReset{CardCol: coordmember.ColDone, NewRev: k.state.Rev, Ok: true}); ok {
		t.Fatal("reset of a finished card")
	}
	if _, ok := k.step(coordmember.RecoverConfirm{CardCol: coordmember.ColWorking, NewRev: k.state.Rev}); ok {
		t.Fatal("confirm of a working card")
	}
	requireCheckpointUnchanged(t, root, c)
}

// Theorem 7: a checkpoint with a blocked member refuses a wrap-up, and a recovery
// that leaves none lets it through again.
func TestCoordinatorWrapUpRefusalIsTheKernelQuery(t *testing.T) {
	root := tempBoard(t)
	ids, c := blockedGroup(t, root)
	if !wrapUpBlocked(c) || wrapUpBlocked(newGroupCheckpointWithoutBlocks(c)) {
		t.Fatal("the wrap-up query misjudged the blocked members")
	}
	err := WithTransaction(root, LockScope{Tasks: ids, Groups: []string{coordinatorGroup}, ReadOnly: true}, func(tx *Transaction) error {
		e := refuseBlockedWrapUp(tx, DispatchInput{TaskID: ids[1], Evidence: DispatchEvidence{WrapUp: &DispatchWrapUpBinding{}}})
		if e == nil || !strings.Contains(e.Error(), ids[0]) {
			t.Fatalf("%v", e)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	next := recoverMember(t, root, c, ids[0])
	if wrapUpBlocked(next) || groupState(next).Blocked != 0 {
		t.Fatalf("%+v", groupState(next))
	}
}

// newGroupCheckpointWithoutBlocks is the checkpoint as it reads once every block
// is cleared.
func newGroupCheckpointWithoutBlocks(c CoordinatorCheckpoint) CoordinatorCheckpoint {
	cleared := c
	cleared.Members = map[string]CoordinatorMember{}
	for id, m := range c.Members {
		m.Blocked = nil
		cleared.Members[id] = m
	}
	return cleared
}

// Theorem 8: the witness of a wrap-up is written once; a second write is refused
// by the kernel and by putWrapUpEvidence, and a stale epoch writes nothing.
func TestWrapUpWitnessIsImmutable(t *testing.T) {
	root := tempBoard(t)
	id := coordinatorCard(t, root, "witness").Entry.TaskID
	coordinatorClaim(t, root, id)
	e := WrapUpGitEvidence{Schema: wrapUpEvidenceSchema, DispatchID: "wrap-immutable", TaskID: id, Epoch: 1, VerifiedBy: wrapUpVerifiedByCompletion, VerifiedAt: "2026-11-16T00:00:00Z"}
	if err := WithTransaction(root, LockScope{Groups: []string{coordinatorGroup}}, func(tx *Transaction) error {
		return putWrapUpEvidence(tx, coordinatorGroup, e)
	}); err != nil {
		t.Fatal(err)
	}
	err := WithTransaction(root, LockScope{Groups: []string{coordinatorGroup}}, func(tx *Transaction) error {
		return putWrapUpEvidence(tx, coordinatorGroup, e)
	})
	if err == nil || !strings.Contains(err.Error(), "immutable wrap-up Git evidence") {
		t.Fatalf("%v", err)
	}
	w := newWitnessKernel(false)
	next, ok := w.step(wrapupwitness.Complete{EpochSame: true, Verified: true})
	if !ok || !next.Written {
		t.Fatalf("%+v %v", next, ok)
	}
	if _, ok = newWitnessKernel(true).step(wrapupwitness.Complete{EpochSame: true, Verified: true}); ok {
		t.Fatal("the kernel wrote a witness twice")
	}
	if _, ok = w.step(wrapupwitness.Complete{EpochSame: false, Verified: true}); ok {
		t.Fatal("the kernel wrote a witness for a stale epoch")
	}
	// A completed wrap-up with its valid witness is never verified again; a
	// record that does not match its dispatch is refused whatever Git says.
	if _, ok = newWitnessKernel(true).step(wrapupwitness.Reconcile{Completed: true, Valid: true}); !ok {
		t.Fatal("a recorded wrap-up asked for Git")
	}
	if _, ok = newWitnessKernel(true).step(wrapupwitness.Reconcile{Completed: true, Valid: false, Verified: true}); ok {
		t.Fatal("an invalid record passed")
	}
}
