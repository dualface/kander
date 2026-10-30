package board

import (
	"reflect"
	"time"
)

// DispatchSettlement ends a completed binding. The dispatch stays completed and
// keeps its delivery commit; only the authorization goes away.
type DispatchSettlement struct {
	Authorization ExecutionAuthorization `json:"authorization"`
	Cycle         string                 `json:"cycle"`
	CardRevision  uint64                 `json:"card_revision"`
	Decision      string                 `json:"decision_reference,omitempty"`
	At            string                 `json:"at"`
}

// SettleDispatch gives a completed binding of the current cycle a direct exit.
// Without it the only way out was to prepare another dispatch and end it.
func SettleDispatch(root, task, id string, expected uint64) error {
	return WithTransaction(root, LockScope{Tasks: []string{task}}, func(tx *Transaction) error {
		d, err := readDispatch(tx, task, id)
		if err != nil {
			return err
		}
		s, err := tx.Snapshot(task)
		if err != nil {
			return err
		}
		if d.Revision != expected || authFrom(s.Text) != d.Authorization || d.State != DispatchCompleted || d.Settlement != nil || s.Entry.State != "working" && s.Entry.State != "review" {
			return dispatchError(id)
		}
		if err = requireDispatchCycle(d, s); err != nil {
			return err
		}
		if err = stageDispatchSettlement(tx, s, d, ""); err != nil {
			return err
		}
		text, err := clearDispatchBinding(s.Text)
		if err != nil {
			return err
		}
		return tx.Put(task, "spec.md", text)
	})
}

// stageDispatchSettlement writes the immutable original beside the dispatch. The
// caller clears the card's binding in the same transaction.
func stageDispatchSettlement(tx *Transaction, s Snapshot, d Dispatch, decision string) error {
	settlement := DispatchSettlement{Authorization: d.Authorization, Cycle: planCycle(s), CardRevision: s.Revision + 1, Decision: decision, At: time.Now().UTC().Format(time.RFC3339Nano)}
	d.Settlement = &settlement
	d.Revision++
	if err := tx.Put(s.Entry.TaskID, dispatchPath(d.Input.ID, "settlement"), reviewJSON(settlement)); err != nil {
		return err
	}
	return putDispatch(tx, d)
}

func validateDispatchSettlement(tx *Transaction, d Dispatch) error {
	r := d.Settlement
	if r == nil {
		return nil
	}
	if d.State != DispatchCompleted || r.Authorization != d.Authorization || r.Cycle == "" || d.Cycle != "" && r.Cycle != d.Cycle || r.CardRevision == 0 || r.At == "" {
		return dispatchError(d.Input.ID)
	}
	raw, err := tx.Read(d.Input.TaskID, dispatchPath(d.Input.ID, "settlement"))
	if err != nil {
		return err
	}
	var stored DispatchSettlement
	if DecodeReviewJSON([]byte(raw), &stored) != nil || !reflect.DeepEqual(stored, *r) {
		return dispatchError(d.Input.ID)
	}
	return nil
}
