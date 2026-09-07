package board

import (
	"encoding/json"
	"reflect"
	"strconv"
	"time"
)

func (tx *Transaction) requireExecution(s Snapshot, a ExecutionAuthorization, author bool) error {
	current := authFrom(s.Text)
	if current.DispatchID == "" && current.Epoch == 0 && a == (ExecutionAuthorization{}) {
		return nil
	}
	if a != current || a.DispatchID == "" || a.Epoch == 0 {
		return dispatchError(a.DispatchID)
	}
	d, err := readDispatch(tx, s.Entry.TaskID, a.DispatchID)
	if err != nil {
		return err
	}
	if d.State != DispatchAccepted && !time.Now().Before(dispatchAcceptBefore(d)) {
		return dispatchError(a.DispatchID)
	}
	if d.Authorization != a || d.State == DispatchCompleted || d.State == DispatchFailed || d.State == DispatchCancelled || author && d.State != DispatchAccepted {
		return dispatchError(a.DispatchID)
	}
	return nil
}

// stageDispatchMove returns replay=true only for an identical durable receipt.
// The caller publishes the receipt and card relocation in this transaction.
func stageDispatchMove(tx *Transaction, s Snapshot, target string, o MoveOptions) (replay bool, err error) {
	a := o.Authorization
	if target == "archived" || target == "trash" {
		return false, stageDispatchTermination(tx, s, o)
	}
	if a.DispatchID != "" && (o.Reason != "" || o.Decision != "" || o.DuplicateOf != "" || target != "done" && o.Result != "" || target == "done" && o.Result != "completed") {
		return false, dispatchError(a.DispatchID)
	}
	if authFrom(s.Text) == (ExecutionAuthorization{}) && a == (ExecutionAuthorization{}) {
		if o.DeliveryCommit != "" || o.Disposition != nil {
			return false, dispatchError(a.DispatchID)
		}
		return false, nil
	}
	if a != authFrom(s.Text) || a.DispatchID == "" || a.Epoch == 0 || o.Owner != "" {
		return false, dispatchError(a.DispatchID)
	}
	d, err := readDispatch(tx, s.Entry.TaskID, a.DispatchID)
	if err != nil {
		return false, err
	}
	if d.Authorization != a {
		return false, dispatchError(a.DispatchID)
	}
	var name string
	receipt := DispatchReceipt{At: time.Now().UTC(), CardRevision: s.Revision + 1, State: target, DeliveryCommit: o.DeliveryCommit, Disposition: o.Disposition}
	switch target {
	case "working":
		if o.DeliveryCommit != "" || o.Disposition != nil {
			return false, dispatchError(a.DispatchID)
		}
		if d.State == DispatchAccepted || d.State == DispatchCompleted {
			return true, nil
		}
		if (d.State != DispatchPrepared && d.State != DispatchUnknown) || !time.Now().Before(dispatchAcceptBefore(d)) {
			return false, dispatchError(a.DispatchID)
		}
		d.State = DispatchAccepted
		d.Accepted = &receipt
		name = "accepted"
	case "review", "done":
		if (d.Input.Kind == "wrap-up") != (target == "done") || !dispatchCommitPattern.MatchString(o.DeliveryCommit) {
			return false, dispatchError(a.DispatchID)
		}
		if d.Input.Evidence.WrapUp != nil {
			if err := validateDispatchWrapUp(tx, d.Input, true); err != nil {
				return false, err
			}
		}
		if d.Input.Evidence.WrapUp != nil && (o.DeliveryCommit != d.Input.Evidence.WrapUp.Git.SourceCommit || o.Disposition != nil) {
			return false, dispatchEvidenceError("wrap-up completion must bind original integration source")
		}
		if o.Disposition != nil {
			if !validReference(*o.Disposition) || o.Disposition.TaskID != s.Entry.TaskID {
				return false, dispatchError(a.DispatchID)
			}
			if _, err := tx.Read(s.Entry.TaskID, o.Disposition.Path); err != nil {
				return false, err
			}
		}
		if d.State == DispatchCompleted {
			old := d.Completed
			if old != nil && old.State == target && old.DeliveryCommit == o.DeliveryCommit && reflect.DeepEqual(old.Disposition, o.Disposition) {
				return true, nil
			}
			return false, dispatchError(a.DispatchID)
		}
		if d.State != DispatchAccepted || s.Entry.State != "working" {
			return false, dispatchError(a.DispatchID)
		}
		d.State = DispatchCompleted
		d.Completed = &receipt
		name = "completed"
	default:
		return false, dispatchError(a.DispatchID)
	}
	d.Revision++
	raw, _ := json.Marshal(receipt)
	if err = tx.Put(s.Entry.TaskID, dispatchPath(a.DispatchID, name+"-"+strconv.FormatUint(a.Epoch, 10)), string(raw)+"\n"); err != nil {
		return false, err
	}
	return false, putDispatch(tx, d)
}

// Explicit lifecycle decisions remain available for bound cards. They cancel
// an unfinished grant in the same transaction, never authorize further work.
func stageDispatchTermination(tx *Transaction, s Snapshot, o MoveOptions) error {
	current := authFrom(s.Text)
	if current.DispatchID == "" {
		return nil
	}
	if o.Reason == "" || o.Decision == "" || o.Authorization != (ExecutionAuthorization{}) && o.Authorization != current {
		return dispatchError(current.DispatchID)
	}
	d, err := readDispatch(tx, s.Entry.TaskID, current.DispatchID)
	if err != nil {
		return err
	}
	if d.State == DispatchCompleted || d.State == DispatchFailed || d.State == DispatchCancelled {
		return nil
	}
	d.State = DispatchCancelled
	d.Reason = o.Reason
	d.Revision++
	return putDispatch(tx, d)
}
