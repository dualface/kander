package board

import (
	"encoding/json"
	"reflect"
	"strconv"
	"time"
)

// DispatchExecution records the current epoch's acceptance budget. Legacy
// records omit it and continue to use the immutable intent deadline.
type DispatchExecution struct {
	Epoch     uint64              `json:"epoch"`
	IssuedAt  time.Time           `json:"issued_at"`
	ConfirmBy time.Time           `json:"confirm_by"`
	Exit      *WrapUpExitEvidence `json:"exit,omitempty"`
}

// AcceptBefore is the effective acceptance deadline for all dispatch consumers.
// Acceptance ends this deadline's role; it is never a work-completion deadline.
func (d Dispatch) AcceptBefore() time.Time {
	if d.WrapUpAuthority != nil {
		return d.WrapUpAuthority.ConfirmBy
	}
	if d.Execution != nil {
		return d.Execution.ConfirmBy
	}
	return d.Input.ConfirmBy
}

func validDispatchExit(s Snapshot, exit WrapUpExitEvidence, now time.Time) bool {
	return exit.CardRevision == s.Revision && exit.Session != "" &&
		exit.Session == MetadataFrom(s.Text, FieldSession) &&
		exit.Window == MetadataFrom(s.Text, FieldWindow) &&
		exit.Owner == MetadataFrom(s.Text, FieldOwner) &&
		exit.StartedAt == MetadataFrom(s.Text, FieldStartedAt) &&
		!exit.ObservedAt.IsZero() && !exit.ObservedAt.After(now) &&
		now.Sub(exit.ObservedAt) <= 30*time.Second
}

func validateDispatchExecution(tx *Transaction, d Dispatch) error {
	e := d.Execution
	if e == nil {
		return nil
	}
	if d.WrapUpAuthority != nil || e.Epoch != d.Authorization.Epoch || e.IssuedAt.IsZero() || !e.ConfirmBy.After(e.IssuedAt) {
		return dispatchEvidenceError("execution deadline binding")
	}
	raw, err := tx.Read(d.Input.TaskID, dispatchPath(d.Input.ID, "execution-"+strconv.FormatUint(e.Epoch, 10)))
	if err != nil {
		return err
	}
	var original Dispatch
	if json.Unmarshal([]byte(raw), &original) != nil || original.Authorization != d.Authorization || !reflect.DeepEqual(original.Input, d.Input) || !reflect.DeepEqual(original.Execution, e) {
		return dispatchEvidenceError("execution original mismatch")
	}
	return nil
}
