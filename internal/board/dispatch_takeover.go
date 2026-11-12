package board

import "strings"

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
	if context.Authorization != authFrom(s.Text) || strings.TrimSpace(decision) == "" {
		return "", false, dispatchError(context.Authorization.DispatchID)
	}
	dispatch, err := readDispatch(tx, s.Entry.TaskID, context.Authorization.DispatchID)
	if err != nil {
		return "", false, err
	}
	if dispatch.Revision != context.Revision || dispatch.Authorization != context.Authorization {
		return "", false, dispatchError(context.Authorization.DispatchID)
	}
	if err := requireDispatchCycle(dispatch, s); err != nil {
		return "", false, err
	}
	switch context.Mode {
	case "retain":
		if dispatch.State != DispatchPrepared && dispatch.State != DispatchUnknown && dispatch.State != DispatchAccepted {
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
