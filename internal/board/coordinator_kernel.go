package board

import (
	"math"

	"github.com/dualface/kander/internal/statemachine/coordinator"
	"github.com/dualface/kander/internal/statemachine/coordmember"
	"github.com/dualface/kander/internal/statemachine/wrapupwitness"
)

// The accept and refuse decisions of a coordinator checkpoint are made by three
// generated kernels (formal/KanderFormal/Machines/Coordinator.lean,
// CoordinatorMember.lean, WrapUpWitness.lean; docs/formal-verification.md):
//
//   - internal/statemachine/coordinator: one instance per group. It holds whether
//     the checkpoint exists, its revision, the authority epoch, whether it holds
//     a last request, and how many members are blocked. It decides claim,
//     reconcile, recover, and removal of a member, and answers the wrap-up refusal.
//   - internal/statemachine/coordmember: one instance per member mirror. It holds
//     the column, the card revision, the chain position, the dispatch position,
//     and the blocked and confirmed flags. It decides which new mirror a member
//     accepts, when it blocks, and what a recovery does. A member depends on no
//     other member.
//   - internal/statemachine/wrapupwitness: one instance per wrap-up dispatch. It
//     decides when the Git witness is written.
//
// This file projects checkpoints, mirrors, and witness records into kernel
// states and builds kernel events. The facts the kernel does not compute stay
// here: the hashes and tokens (their comparison is an argument), the board
// membership, the recovery records, the execution chain and dispatch round walk
// (only their positions and the "no handoff" verdict reach the kernel), the card
// lifecycle and ledger recovery checks, the review projection, and Git.
//
// Projection of a group: Exists is whether the checkpoint file exists, Last
// whether it holds a LastRequestHash, Blocked how many mirrors carry a block.
// Revision and epoch are shifted like the counters of the execution ledger
// (execution_kernel.go): the kernel parameters are uint32 and its decisions only
// compare them and add one, so an operation fixes a shift from the state before
// it and maps the state, the request values, and the result with it. A request
// value that differs from the state maps to a value that differs from it too.
//
// Projection of a mirror: the column is the state of the card (backlog and todo
// are one column), and the card revision, chain position, and dispatch position
// are the stored values, shifted by revisionBase (execution_kernel.go) from 2^31
// on so that order is kept.

// kernelMax is the bound every counter parameter of a coordinator kernel
// carries. A value above it is clamped to it, which keeps every comparison with a
// projected state value.
const kernelMax = math.MaxUint32

func clampKernel(v uint64) uint64 { return min(v, kernelMax) }

// groupKernel is the projected checkpoint of one group in one operation.
type groupKernel struct {
	state           coordinator.State
	params          coordinator.Params
	rev, epoch      uint64
	revBase, epBase uint64
}

// newGroupKernel projects the checkpoint of a group. members is the number of
// members the group has; a missing checkpoint projects to the initial state.
func newGroupKernel(c CoordinatorCheckpoint, exists bool, members int) *groupKernel {
	g := &groupKernel{rev: c.Revision, epoch: c.Authority.Epoch, revBase: counterBase(c.Revision), epBase: counterBase(c.Authority.Epoch)}
	g.params = coordinator.Params{MaxRev: kernelMax, MaxEpoch: kernelMax, Members: uint32(members)}
	g.state = coordinator.State{Exists: exists, Rev: shift(c.Revision, g.revBase), Epoch: shift(c.Authority.Epoch, g.epBase),
		Last: c.LastRequestHash != "", Blocked: uint64(len(c.BlockedMembers()))}
	return g
}

// counterArg maps a value a request names. Equal to the state it is the state's
// projection; below it, its own shift; above it, one more than the state.
func counterArg(v, raw, base, proj uint64) uint64 {
	switch {
	case v == raw:
		return proj
	case v < raw:
		return shift(v, base)
	}
	return proj + 1
}

func (g *groupKernel) revArg(v uint64) uint64 { return counterArg(v, g.rev, g.revBase, g.state.Rev) }
func (g *groupKernel) epochArg(v uint64) uint64 {
	return counterArg(v, g.epoch, g.epBase, g.state.Epoch)
}

// revOut maps the revision of an accepted state back: the raw revision moved by
// the distance the kernel moved it.
func (g *groupKernel) revOut(next uint64) uint64 { return g.rev + (next - g.state.Rev) }

// epochOut is revOut for the epoch.
func (g *groupKernel) epochOut(next uint64) uint64 { return g.epoch + (next - g.state.Epoch) }

func (g *groupKernel) step(e coordinator.Event) (coordinator.State, bool) {
	return coordinator.Step(g.state, e, g.params)
}

// probe is the kernel with one member blocked, so a check of the request can be
// asked on its own: the count of blocked members is not what it asks about.
func (g *groupKernel) probe() *groupKernel {
	p := *g
	p.state.Blocked = max(p.state.Blocked, 1)
	return &p
}

// claim builds the kernel event of a claim. same is whether its hash equals the
// stored ClaimHash, tokenSame whether the new token equals the current one.
func (g *groupKernel) claim(r CoordinatorClaim, same, tokenSame, membersOk, membership bool) coordinator.Claim {
	return coordinator.Claim{ExpRev: g.revArg(r.ExpectedRevision), ExpEpoch: g.epochArg(r.ExpectedEpoch), Same: same, TokenSame: tokenSame,
		Room: g.rev != math.MaxUint64 && g.epoch != math.MaxUint64, MembersOk: membersOk, Membership: membership}
}

// reconcileFacts are the facts of a reconcile that the shell computes, in the
// order it computes them; a field is true or false as that check passed.
type reconcileFacts struct {
	membersOk, membership, recoveries, reqFail, changed bool
	blockedAfter                                        int
}

// reconcile builds the kernel event of a reconcile of checkpoint c.
func (g *groupKernel) reconcile(c CoordinatorCheckpoint, r CoordinatorReconcile, f reconcileFacts) coordinator.Reconcile {
	return coordinator.Reconcile{OwnerSame: c.Authority.Owner == r.Authority.Owner, TokenSame: c.Authority.Token == r.Authority.Token,
		ArgEpoch: g.epochArg(r.Authority.Epoch), ExpRev: g.revArg(r.ExpectedRevision), Same: c.LastRequestHash == coordinatorHash(r),
		MembersOk: f.membersOk, Membership: f.membership, Recoveries: f.recoveries, ReqFail: f.reqFail, Changed: f.changed,
		BlockedAfter: uint64(f.blockedAfter), Room: g.rev != math.MaxUint64}
}

// refuseAuthority reports whether a reconcile of r finds no checkpoint, or finds
// another authority than the checkpoint's.
func (g *groupKernel) refuseAuthority(c CoordinatorCheckpoint, r CoordinatorReconcile) bool {
	same := r
	same.ExpectedRevision = c.Revision
	_, ok := g.probe().step(g.reconcile(c, same, reconcileFacts{membersOk: true, membership: true, recoveries: true}))
	return !ok
}

// refuseRevision reports whether a reconcile that holds the authority read a
// revision that is neither the checkpoint's nor the retry of its last request.
func (g *groupKernel) refuseRevision(c CoordinatorCheckpoint, r CoordinatorReconcile) bool {
	_, ok := g.probe().step(g.reconcile(c, r, reconcileFacts{membersOk: true, membership: true, recoveries: true}))
	return !ok
}

// refuseRequest runs the event of a request one of whose members failed at
// request level. The kernel refuses it whatever the other facts say.
func (g *groupKernel) refuseRequest(c CoordinatorCheckpoint, r CoordinatorReconcile) bool {
	_, ok := g.step(g.reconcile(c, r, reconcileFacts{membersOk: true, membership: true, recoveries: true, reqFail: true, blockedAfter: int(g.state.Blocked)}))
	return !ok
}

// recoverFacts are the facts of a recovery, true as the shell found them.
func (g *groupKernel) recoverEvent(r CoordinatorRecover, c CoordinatorCheckpoint, known, memberBlocked, expect, memberOk bool) coordinator.Recover {
	return coordinator.Recover{ExpRev: g.revArg(r.ExpectedRevision), ExpEpoch: g.epochArg(r.ExpectedEpoch), Room: g.rev != math.MaxUint64,
		Known: known, MemberBlocked: memberBlocked, Expect: expect, MemberOk: memberOk}
}

// removeMember builds the kernel event of a removal from the facts the shell found.
func (g *groupKernel) removeMember(r CoordinatorRemoveMember, f removeFacts) coordinator.RemoveMember {
	return coordinator.RemoveMember{ExpRev: g.revArg(r.ExpectedRevision), ExpEpoch: g.epochArg(r.ExpectedEpoch), Room: g.rev != math.MaxUint64,
		Known: f.known, Unstarted: f.unstarted, Membership: f.membership, Remaining: f.remaining, NoDependents: f.noDependents,
		BlockedAfter: uint64(f.blockedAfter)}
}

// wrapUpBlocked is `refuseBlockedWrapUp`: a checkpoint with a blocked member.
func wrapUpBlocked(c CoordinatorCheckpoint) bool {
	return coordinator.WrapUpBlocked(newGroupKernel(c, true, len(c.Members)).state, coordinator.Params{Members: uint32(len(c.Members))})
}

// allBlocked reports that a committed checkpoint has every member blocked.
func allBlocked(c CoordinatorCheckpoint) bool {
	return coordinator.AllBlocked(newGroupKernel(c, true, len(c.Members)).state, coordinator.Params{Members: uint32(len(c.Members))})
}

// memberCol maps a card column to the kernel's.
func memberCol(state string) coordmember.Col {
	switch state {
	case "working":
		return coordmember.ColWorking
	case "review":
		return coordmember.ColReview
	case "done":
		return coordmember.ColDone
	case "archived":
		return coordmember.ColArchived
	case "trash":
		return coordmember.ColTrash
	}
	return coordmember.ColEarly
}

// mirrorKernel is the projected mirror of one member in one operation.
type mirrorKernel struct {
	state  coordmember.State
	params coordmember.Params
	rev    uint64
	tseq   uint64
	dseq   uint64
}

// newMirrorKernel projects a mirror. The bases come from the mirror itself, so
// every value the operation compares with it keeps its order.
func newMirrorKernel(old CoordinatorMember) *mirrorKernel {
	k := &mirrorKernel{rev: revisionBase(old.Revision), tseq: revisionBase(old.TransitionSeq), dseq: revisionBase(old.DispatchSeq)}
	k.params = coordmember.Params{MaxRev: kernelMax, MaxSeq: kernelMax}
	k.state = coordmember.State{Col: memberCol(old.State), Rev: k.revOf(old.Revision), Tseq: k.tseqOf(old.TransitionSeq),
		Dseq: k.dseqOf(old.DispatchSeq), Blocked: old.Blocked != nil, Confirmed: old.Confirmed != nil}
	return k
}

func (k *mirrorKernel) revOf(v uint64) uint64  { return clampKernel(shift(v, k.rev)) }
func (k *mirrorKernel) tseqOf(v uint64) uint64 { return clampKernel(shift(v, k.tseq)) }
func (k *mirrorKernel) dseqOf(v uint64) uint64 { return clampKernel(shift(v, k.dseq)) }

func (k *mirrorKernel) step(e coordmember.Event) (coordmember.State, bool) {
	return coordmember.Step(k.state, e, k.params)
}

// refuseRevision reports that the card of the member is below the mirror: a
// walking event that keeps the chain position refuses it.
func (k *mirrorKernel) refuseRevision(rev uint64) bool {
	_, ok := k.step(coordmember.Accept{NewCol: coordmember.ColWorking, NewRev: k.revOf(rev), NewTseq: k.state.Tseq, NewDseq: k.state.Dseq, Ok: true})
	return !ok
}

// refuseChain reports that the chain ends below the mirror's position, or that it
// misses the handoff of the mirror's cycle.
func (k *mirrorKernel) refuseChain(end uint64, missing bool) bool {
	_, ok := k.step(coordmember.Accept{NewCol: coordmember.ColWorking, NewRev: k.state.Rev, NewTseq: k.tseqOf(end), NewDseq: k.state.Dseq, Ok: true, Missing: missing})
	return !ok
}

// chainError is the refusal the walk of an execution chain gets from the kernel:
// the chain ended below the mirror's position, or (missing) it carries no handoff
// to the cycle the mirror recorded.
func (k *mirrorKernel) chainError(l executionLedger, missing bool) error {
	if !k.refuseChain(l.end(), missing) {
		return nil
	}
	if missing {
		return missingHandoffError()
	}
	return coordinatorError("execution chain regressed: " + l.task)
}

// confirmed is `memberConfirmed`: a finished card in the cycle a recovery
// confirmed. The card revision is not asked about here.
func (k *mirrorKernel) confirmed(s Snapshot, old CoordinatorMember) bool {
	_, ok := k.step(coordmember.AcceptConfirmed{NewCol: memberCol(s.Entry.State), NewRev: k.state.Rev, CycSame: old.Confirmed != nil && old.Confirmed.Cycle == planCycle(s)})
	return ok
}

// accepted is the kernel state of a member that took the confirmed short path.
func (k *mirrorKernel) acceptConfirmed(s Snapshot, old CoordinatorMember) (coordmember.State, bool) {
	return k.step(coordmember.AcceptConfirmed{NewCol: memberCol(s.Entry.State), NewRev: k.revOf(s.Revision), CycSame: old.Confirmed != nil && old.Confirmed.Cycle == planCycle(s)})
}

// accept steps the walk of a member that is neither confirmed nor terminal onto
// the mirror m the walk built.
func (k *mirrorKernel) accept(s Snapshot, old, m CoordinatorMember) (coordmember.State, bool) {
	return k.step(coordmember.Accept{NewCol: memberCol(s.Entry.State), NewRev: k.revOf(s.Revision), NewTseq: k.tseqOf(m.TransitionSeq), NewDseq: k.dseqOf(m.DispatchSeq),
		CycSame: old.Confirmed != nil && old.Confirmed.Cycle == planCycle(s), Ok: true})
}

// acceptTerminal steps a member in archived or trash onto the mirror m.
func (k *mirrorKernel) acceptTerminal(s Snapshot, old, m CoordinatorMember, hasRounds bool) (coordmember.State, bool) {
	return k.step(coordmember.AcceptTerminal{NewCol: memberCol(s.Entry.State), NewRev: k.revOf(s.Revision), NewTseq: k.tseqOf(m.TransitionSeq),
		NewDseq: k.dseqOf(m.DispatchSeq), HasRounds: hasRounds, CycSame: old.Confirmed != nil && old.Confirmed.Cycle == planCycle(s), Ok: true})
}

// block keeps the mirror and records that the member did not reconcile.
func (k *mirrorKernel) block() coordmember.State {
	next, _ := k.step(coordmember.Block{})
	return next
}

// refuseBlocked reports a member that is not blocked: a reset of a working card
// refuses it and nothing else.
func (k *mirrorKernel) refuseBlocked() bool {
	_, ok := k.step(coordmember.RecoverReset{CardCol: coordmember.ColWorking, NewRev: k.state.Rev, NewTseq: k.state.Tseq, NewDseq: k.state.Dseq, Ok: true})
	return !ok
}

// recoverReset steps the reset of a blocked member whose card is in working or
// review, with the revision, chain position, and round position after it.
func (k *mirrorKernel) recoverReset(s Snapshot, m CoordinatorMember, ok bool) (coordmember.State, bool) {
	return k.step(coordmember.RecoverReset{CardCol: memberCol(s.Entry.State), NewRev: k.revOf(m.Revision), NewTseq: k.tseqOf(m.TransitionSeq),
		NewDseq: k.dseqOf(m.DispatchSeq), Ok: ok})
}

// recoverConfirm steps the confirmation of a blocked member whose card is
// finished. tseqRead and dseqRead say whether the chain and the rounds could be
// read; when not, the mirror keeps its position.
func (k *mirrorKernel) recoverConfirm(s Snapshot, m CoordinatorMember, tseqRead, dseqRead bool) (coordmember.State, bool) {
	return k.step(coordmember.RecoverConfirm{CardCol: memberCol(s.Entry.State), NewRev: k.revOf(m.Revision), TseqRead: tseqRead, NewTseq: k.tseqOf(m.TransitionSeq),
		DseqRead: dseqRead, NewDseq: k.dseqOf(m.DispatchSeq)})
}

// witnessKernel is the projected Git witness of one wrap-up dispatch.
type witnessKernel struct{ state wrapupwitness.State }

func newWitnessKernel(written bool) witnessKernel {
	return witnessKernel{state: wrapupwitness.State{Written: written}}
}

func (w witnessKernel) step(e wrapupwitness.Event) (wrapupwitness.State, bool) {
	return wrapupwitness.Step(w.state, e, wrapupwitness.Params{})
}
