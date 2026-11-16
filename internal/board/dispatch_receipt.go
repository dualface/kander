package board

import (
	"encoding/json"
	"reflect"
	"strconv"
	"time"

	dispatchkernel "github.com/dualface/kander/internal/statemachine/dispatch"
)

// requireExecution checks a write of the executor against the card's binding
// and its dispatch record. The kernel decides: an empty authorization on a card
// with no binding passes; otherwise the authorization is the binding's and the
// round is open, accepted or before its deadline, and accepted for an author
// write.
func (tx *Transaction) requireExecution(s Snapshot, a ExecutionAuthorization, author bool) error {
	v := projectDispatch(s, nil)
	round, epoch := v.pair(a)
	// The binding is compared before the record is read; the phase of a bound
	// card is not known yet, so the probe takes an accepted one.
	fence := v.state
	if fence.Bound {
		fence.Phase = dispatchkernel.PhaseAccepted
	}
	if !v.allows(fence, dispatchkernel.Exec{AuthRound: round, AuthEpoch: epoch, Rec: true, Before: true}) {
		return dispatchError(a.DispatchID)
	}
	if !v.state.Bound {
		return nil
	}
	d, err := readDispatch(tx, s.Entry.TaskID, a.DispatchID)
	if err != nil {
		return err
	}
	v = projectDispatch(s, &d)
	if _, ok := v.next(dispatchkernel.Exec{AuthRound: round, AuthEpoch: epoch, Rec: d.Authorization == a, Before: dispatchBefore(d), Author: author}); !ok {
		return dispatchError(a.DispatchID)
	}
	return nil
}

// stageDispatchMove returns replay=true only for an identical durable receipt.
// The caller publishes the receipt and card relocation in this transaction. The
// kernel decides each acceptance, replay, and refusal; the checks of one
// request keep the order they have always run in, so a request that fails
// several returns the error of the first. A probe isolates one fact on a state
// that no other fact can refuse.
func stageDispatchMove(tx *Transaction, s Snapshot, target string, o MoveOptions) (replay, settled bool, err error) {
	a := o.Authorization
	if target == "archived" || target == "trash" {
		settled, err = stageDispatchTermination(tx, s, o)
		return false, settled, err
	}
	if a.DispatchID != "" && (o.Reason != "" || o.Decision != "" || o.DuplicateOf != "" || target != "done" && o.Result != "" || target == "done" && o.Result != "completed") {
		return false, false, dispatchError(a.DispatchID)
	}
	if authFrom(s.Text) == (ExecutionAuthorization{}) && a == (ExecutionAuthorization{}) {
		if o.DeliveryCommit != "" || o.Disposition != nil {
			return false, false, dispatchError(a.DispatchID)
		}
		return false, false, nil
	}
	v := projectDispatch(s, nil)
	round, epoch := v.pair(a)
	accept := func(rec, cycle, before bool) dispatchkernel.Accept {
		return dispatchkernel.Accept{AuthRound: round, AuthEpoch: epoch, Rec: rec, Cycle: cycle, Before: before}
	}
	// The binding is compared before the record is read.
	fence := v.state
	fence.Phase = dispatchkernel.PhaseAccepted
	if o.Owner != "" || !v.allows(fence, accept(true, true, true)) {
		return false, false, dispatchError(a.DispatchID)
	}
	d, err := readDispatch(tx, s.Entry.TaskID, a.DispatchID)
	if err != nil {
		return false, false, err
	}
	v = projectDispatch(s, &d)
	probe := v.permissive(a, dispatchkernel.PhaseAccepted, v.state.Kind)
	if !v.allows(probe, accept(d.Authorization == a, true, true)) {
		return false, false, dispatchError(a.DispatchID)
	}
	cycleErr := requireDispatchCycle(d, s)
	if !v.allows(probe, accept(true, cycleErr == nil, true)) {
		return false, false, cycleErr
	}
	var name string
	receipt := DispatchReceipt{At: time.Now().UTC(), CardRevision: s.Revision + 1, State: target, DeliveryCommit: o.DeliveryCommit, Disposition: o.Disposition}
	switch target {
	case "working":
		if o.DeliveryCommit != "" || o.Disposition != nil {
			return false, false, dispatchError(a.DispatchID)
		}
		next, ok := v.next(accept(true, true, dispatchBefore(d)))
		if !ok {
			return false, false, dispatchError(a.DispatchID)
		}
		if next.Phase == v.state.Phase {
			return true, false, nil
		}
		d.State = dispatchStates[next.Phase]
		d.Accepted = &receipt
		name = "accepted"
	case "review", "done":
		complete := func(disp, same bool, col dispatchkernel.Col, commit bool) dispatchkernel.Complete {
			return dispatchkernel.Complete{AuthRound: round, AuthEpoch: epoch, Rec: true, Cycle: true, ToDone: target == "done",
				Commit: commit, Evidence: true, Disp: disp, Same: same, Col: col}
		}
		commit := dispatchCommitPattern.MatchString(o.DeliveryCommit)
		if !v.allows(probe, complete(true, true, dispatchkernel.ColWorking, commit)) {
			return false, false, dispatchError(a.DispatchID)
		}
		if d.Input.Evidence.WrapUp != nil {
			if err := validateDispatchWrapUp(tx, d.Input, true, false); err != nil {
				return false, false, err
			}
		}
		if d.Input.Evidence.WrapUp != nil && (o.DeliveryCommit != d.Input.Evidence.WrapUp.Git.SourceCommit || o.Disposition != nil) {
			return false, false, dispatchEvidenceError("wrap-up completion must bind original integration source")
		}
		disp := o.Disposition == nil || validReference(*o.Disposition) && o.Disposition.TaskID == s.Entry.TaskID
		if !v.allows(probe, complete(disp, true, dispatchkernel.ColWorking, commit)) {
			return false, false, dispatchError(a.DispatchID)
		}
		if o.Disposition != nil {
			if _, err := tx.Read(s.Entry.TaskID, o.Disposition.Path); err != nil {
				return false, false, err
			}
		}
		old := d.Completed
		same := old != nil && old.State == target && old.DeliveryCommit == o.DeliveryCommit && reflect.DeepEqual(old.Disposition, o.Disposition)
		next, ok := v.next(complete(true, same, kernelDispatchCol(s.Entry.State), commit))
		if !ok {
			return false, false, dispatchError(a.DispatchID)
		}
		if next.Phase == v.state.Phase {
			return true, false, nil
		}
		d.State = dispatchStates[next.Phase]
		d.Completed = &receipt
		name = "completed"
	default:
		return false, false, dispatchError(a.DispatchID)
	}
	d.Revision++
	raw, _ := json.Marshal(receipt)
	if err = tx.Put(s.Entry.TaskID, dispatchPath(a.DispatchID, name+"-"+strconv.FormatUint(a.Epoch, 10)), string(raw)+"\n"); err != nil {
		return false, false, err
	}
	return false, false, putDispatch(tx, d)
}

// Explicit lifecycle decisions remain available for bound cards. They cancel
// an unfinished grant in the same transaction, never authorize further work. A
// completed binding has nothing left to cancel: it is settled instead, which
// also empties the authorization. The kernel decides the cancellation; the
// settlement is not modeled yet.
func stageDispatchTermination(tx *Transaction, s Snapshot, o MoveOptions) (settled bool, err error) {
	current := authFrom(s.Text)
	if current.DispatchID == "" {
		return false, nil
	}
	v := projectDispatch(s, nil)
	cancel := func(cycle bool) dispatchkernel.Cancel {
		authOk := o.Authorization == (ExecutionAuthorization{}) || o.Authorization == current
		return dispatchkernel.Cancel{Reasoned: o.Reason != "" && o.Decision != "", AuthOk: authOk, Cycle: cycle}
	}
	open := v.state
	open.Phase = dispatchkernel.PhasePrepared
	if !v.allows(open, cancel(true)) {
		return false, dispatchError(current.DispatchID)
	}
	d, err := readDispatch(tx, s.Entry.TaskID, current.DispatchID)
	if err != nil {
		return false, err
	}
	cycleErr := requireDispatchCycle(d, s)
	if !v.allows(open, cancel(cycleErr == nil)) {
		return false, cycleErr
	}
	if d.State == DispatchCompleted {
		return true, stageDispatchSettlement(tx, s, d, o.Decision)
	}
	v = projectDispatch(s, &d)
	next, ok := v.next(cancel(true))
	if !ok {
		return false, dispatchError(current.DispatchID)
	}
	if next.Phase == v.state.Phase {
		return false, nil
	}
	d.State = dispatchStates[next.Phase]
	d.Reason = o.Reason
	d.Revision++
	return false, putDispatch(tx, d)
}
