package board

import (
	"time"

	dispatchkernel "github.com/dualface/kander/internal/statemachine/dispatch"
)

// The acceptance and refusal decisions of the dispatch rounds of a card are
// made by the generated kernel in internal/statemachine/dispatch
// (formal/KanderFormal/Machines/Dispatch.lean). This file projects a card and
// the dispatch record it names into a kernel state and runs kernel events. See
// docs/formal-verification.md.
//
// The kernel models the slot a card offers one round of dispatch. Projection
// rules:
//   - The round is relative: 1 when the card has a binding or the projection
//     carries a record, else 0. The kernel only compares a presented round with
//     the binding and adds one on a new round; the global uniqueness of dispatch
//     IDs belongs to the registry.
//   - The epoch of the state is the card's EXECUTION_EPOCH when bound, else the
//     epoch of the record, else 0. DISPATCH_ID and EXECUTION_EPOCH are written and
//     cleared together, so a card never has an epoch without a binding. From
//     2^31 on an epoch projects to 1, and the state, the presented epochs, and
//     the result use that one shift; the ceiling of the uint64 representation is
//     the Room fact.
//   - The presented pair names the card's binding when the presented ID and epoch
//     equal it; any other non-empty ID or epoch projects to a value that differs
//     from the binding's, and an empty one to 0.
//   - A released round leaves no trace on the card: a card without binding projects
//     to the empty state unless the entrance holds the released record.

var kernelDispatchPhases = map[DispatchState]dispatchkernel.Phase{
	DispatchPrepared: dispatchkernel.PhasePrepared, DispatchUnknown: dispatchkernel.PhaseUnknown,
	DispatchAccepted: dispatchkernel.PhaseAccepted, DispatchCompleted: dispatchkernel.PhaseCompleted,
	DispatchFailed: dispatchkernel.PhaseFailed, DispatchCancelled: dispatchkernel.PhaseCancelled,
}

// dispatchStates is the inverse of kernelDispatchPhases.
var dispatchStates = [dispatchkernel.PhaseCount]DispatchState{
	dispatchkernel.PhasePrepared: DispatchPrepared, dispatchkernel.PhaseUnknown: DispatchUnknown,
	dispatchkernel.PhaseAccepted: DispatchAccepted, dispatchkernel.PhaseCompleted: DispatchCompleted,
	dispatchkernel.PhaseFailed: DispatchFailed, dispatchkernel.PhaseCancelled: DispatchCancelled,
}

var kernelDispatchKinds = map[string]dispatchkernel.Kind{
	"fix": dispatchkernel.KindFix, "sync": dispatchkernel.KindSync, "wrap-up": dispatchkernel.KindWrapUp,
}

// kernelMaxRound bounds the relative rounds of one operation: the state's, and
// the one a presented foreign ID projects to.
const kernelMaxRound = 3

// kernelDispatchCol maps a board state to the columns the kernel tells apart.
func kernelDispatchCol(state string) dispatchkernel.Col {
	switch state {
	case "working":
		return dispatchkernel.ColWorking
	case "review":
		return dispatchkernel.ColReview
	}
	return dispatchkernel.ColOther
}

// dispatchView is the projected dispatch slot of one card in one operation.
type dispatchView struct {
	state   dispatchkernel.State
	params  dispatchkernel.Params
	binding ExecutionAuthorization // DISPATCH_ID and EXECUTION_EPOCH of the card
	epoch   uint64                 // the epoch that state.Epoch projects
	base    uint64                 // the shift of this operation's epochs
}

// projectDispatch is the slot of the card s. d is the record the entrance read
// for it, or nil when the entrance has not read one.
func projectDispatch(s Snapshot, d *Dispatch) *dispatchView { return projectDispatchBase(s, d, nil) }

// projectDispatchBase is projectDispatch with the epoch shift of an operation,
// when it is fixed by the state before the operation; otherwise the state fixes it.
func projectDispatchBase(s Snapshot, d *Dispatch, base *uint64) *dispatchView {
	v := &dispatchView{binding: authFrom(s.Text)}
	v.state.Bound = v.binding.DispatchID != ""
	switch {
	case v.state.Bound:
		v.epoch = v.binding.Epoch
	case d != nil:
		v.epoch = d.Authorization.Epoch
	}
	if v.state.Bound || d != nil {
		v.state.Round = 1
	}
	if d != nil {
		v.state.Phase, v.state.Kind = kernelDispatchPhases[d.State], kernelDispatchKinds[d.Input.Kind]
		v.state.Released = d.Release != nil
	}
	v.base = counterBase(v.epoch)
	if base != nil {
		v.base = *base
	}
	v.state.Epoch = shift(v.epoch, v.base)
	v.params = dispatchkernel.Params{MaxRound: kernelMaxRound, MaxEpoch: uint32(v.state.Epoch + 2)}
	return v
}

// emptyDispatchView is the slot of a card with no round, for the checks an
// entrance makes before it reads the card's binding.
func emptyDispatchView() *dispatchView {
	return &dispatchView{params: dispatchkernel.Params{MaxRound: kernelMaxRound, MaxEpoch: 2}}
}

// pair projects an authorization a request presents.
func (v *dispatchView) pair(a ExecutionAuthorization) (round, epoch uint64) {
	switch {
	case a.DispatchID == "":
	case a.DispatchID == v.binding.DispatchID:
		round = 1
	default:
		round = 2
	}
	switch {
	case a.Epoch == 0:
	case a.Epoch == v.epoch:
		epoch = v.state.Epoch
	default:
		epoch = v.state.Epoch + 1
	}
	return round, epoch
}

// real maps an epoch of the kernel back to the card's.
func (v *dispatchView) real(epoch uint64) uint64 {
	if v.base == 0 {
		return epoch
	}
	return epoch + v.base - 1
}

// permissive is a state bound to the pair a presents, in phase ph, that refuses
// no event for its fencing or phase: a check that must see one fact alone, and
// not the ones an entrance has always evaluated later, runs on it.
func (v *dispatchView) permissive(a ExecutionAuthorization, ph dispatchkernel.Phase, kind dispatchkernel.Kind) dispatchkernel.State {
	round, epoch := v.pair(a)
	return dispatchkernel.State{Round: round, Epoch: epoch, Bound: true, Phase: ph, Kind: kind}
}

// next runs one kernel event on the projected state.
func (v *dispatchView) next(e dispatchkernel.Event) (dispatchkernel.State, bool) {
	return dispatchkernel.Step(v.state, e, v.params)
}

// allows reports whether the kernel accepts e in the state st.
func (v *dispatchView) allows(st dispatchkernel.State, e dispatchkernel.Event) bool {
	_, ok := dispatchkernel.Step(st, e, v.params)
	return ok
}

// dispatchRetryAllowed reports whether the same dispatch ID may be prepared
// again: only with an equal input.
func dispatchRetryAllowed(same bool) bool {
	_, ok := dispatchkernel.Step(dispatchkernel.State{}, dispatchkernel.Retry{Same: same}, emptyDispatchView().params)
	return ok
}

// dispatchBefore reports whether the server clock is before the acceptance
// deadline of d.
func dispatchBefore(d Dispatch) bool { return time.Now().Before(d.AcceptBefore()) }
