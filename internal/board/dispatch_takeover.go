package board

import (
	"strings"

	dispatchkernel "github.com/dualface/kander/internal/statemachine/dispatch"
)

// DispatchTakeover binds a distributed ownership handoff to the exact business
// round the successor inspected. Retaining a round never authorizes a replay of
// its external action; completed work is settled with the handoff decision.
type DispatchTakeover struct {
	Authorization ExecutionAuthorization `json:"authorization"`
	Revision      uint64                 `json:"revision"`
	Mode          string                 `json:"mode"`
}

func stageTakeoverDispatch(tx *Transaction, s Snapshot, context *DispatchTakeover, decision string) (string, bool, error) {
	if context == nil {
		return s.Text, false, nil
	}
	// The kernel decides the retained round. The checks run in the order they
	// have always run: the binding and the decision before the record is read, then
	// its revision and authorization, the cycle, and the phase.
	v := projectDispatch(s, nil)
	round, epoch := v.pair(context.Authorization)
	retain := func(rev, rec, cycle bool) dispatchkernel.Retain {
		return dispatchkernel.Retain{AuthRound: round, AuthEpoch: epoch, Decided: strings.TrimSpace(decision) != "", Rev: rev, Rec: rec, Cycle: cycle}
	}
	open := v.state
	open.Phase = dispatchkernel.PhasePrepared
	if !v.allows(open, retain(true, true, true)) {
		return "", false, dispatchError(context.Authorization.DispatchID)
	}
	dispatch, err := readDispatch(tx, s.Entry.TaskID, context.Authorization.DispatchID)
	if err != nil {
		return "", false, err
	}
	if !v.allows(open, retain(dispatch.Revision == context.Revision, dispatch.Authorization == context.Authorization, true)) {
		return "", false, dispatchError(context.Authorization.DispatchID)
	}
	cycleErr := requireDispatchCycle(dispatch, s)
	if !v.allows(open, retain(true, true, cycleErr == nil)) {
		return "", false, cycleErr
	}
	switch context.Mode {
	case "retain":
		if _, ok := projectDispatch(s, &dispatch).next(retain(true, true, true)); !ok {
			return "", false, dispatchError(dispatch.Input.ID)
		}
		return s.Text, false, nil
	case "settle-completed":
		if dispatch.State != DispatchCompleted || dispatch.Settlement != nil {
			return "", false, dispatchError(dispatch.Input.ID)
		}
		if err := stageDispatchSettlement(tx, s, dispatch, decision); err != nil {
			return "", false, err
		}
		text, err := clearDispatchBinding(s.Text)
		return text, true, err
	default:
		return "", false, dispatchError(dispatch.Input.ID)
	}
}
