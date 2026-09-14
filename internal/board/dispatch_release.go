package board

import (
	"encoding/json"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

// DispatchTermination preserves the explicit terminal decision independently of
// mutable dispatch state. Legacy releases retain the already-terminal facts.
type DispatchTermination struct {
	Authorization ExecutionAuthorization `json:"authorization"`
	State         DispatchState          `json:"state"`
	Revision      uint64                 `json:"revision"`
	Reason        string                 `json:"reason"`
	Decision      string                 `json:"decision_reference,omitempty"`
}

// DispatchRelease binds the removal of authority to one card revision and cycle.
type DispatchRelease struct {
	Termination  DispatchTermination `json:"termination"`
	Cycle        string              `json:"cycle"`
	CardRevision uint64              `json:"card_revision"`
	Decision     string              `json:"decision_reference,omitempty"`
	At           string              `json:"at"`
	Legacy       bool                `json:"legacy,omitempty"`
}

// EndDispatch records explicit termination and removes the active binding in
// one transaction. A legacy terminal binding needs a decision to release only.
func EndDispatch(root, task, id string, expected uint64, state DispatchState, reason string, decisions ...string) error {
	decision := ""
	if len(decisions) > 1 {
		return dispatchError(id)
	}
	if len(decisions) == 1 {
		decision = decisions[0]
	}
	if (state != DispatchFailed && state != DispatchCancelled) || strings.TrimSpace(reason) == "" || !utf8.ValidString(reason) || !utf8.ValidString(decision) {
		return dispatchError(id)
	}
	return WithTransaction(root, LockScope{Tasks: []string{task}}, func(tx *Transaction) error {
		d, err := readDispatch(tx, task, id)
		if err != nil {
			return err
		}
		s, err := tx.Snapshot(task)
		if err != nil {
			return err
		}
		if d.Revision != expected || authFrom(s.Text) != d.Authorization || d.State == DispatchCompleted || d.Release != nil || d.Revision == ^uint64(0) {
			return dispatchError(id)
		}
		legacy := d.State == DispatchFailed || d.State == DispatchCancelled
		if legacy && (d.State != state || strings.TrimSpace(decision) == "") {
			return dispatchError(id)
		}
		terminal := DispatchTermination{Authorization: d.Authorization, State: d.State, Revision: d.Revision, Reason: d.Reason}
		d.Revision++
		if !legacy {
			d.State, d.Reason = state, reason
			terminal = DispatchTermination{Authorization: d.Authorization, State: state, Revision: d.Revision, Reason: reason, Decision: decision}
			if err := tx.Put(task, dispatchPath(id, "termination"), reviewJSON(terminal)); err != nil {
				return err
			}
		}
		release := DispatchRelease{Termination: terminal, Cycle: planCycle(s), CardRevision: s.Revision + 1, Decision: decision, At: time.Now().UTC().Format(time.RFC3339Nano), Legacy: legacy}
		d.Release = &release
		if err := tx.Put(task, dispatchPath(id, "release"), reviewJSON(release)); err != nil {
			return err
		}
		if err := putDispatch(tx, d); err != nil {
			return err
		}
		text, err := setMetadata(s.Text, "DISPATCH_ID", "")
		if err != nil {
			return err
		}
		text, err = setMetadata(text, "EXECUTION_EPOCH", "")
		if err != nil {
			return err
		}
		text = appendLifecycleRecord(text, "DISPATCHES", lifecycleJSON(release))
		return tx.Put(task, "spec.md", text)
	})
}

func validateDispatchRelease(tx *Transaction, d Dispatch) error {
	r := d.Release
	if r == nil {
		return nil
	}
	t := r.Termination
	if (d.State != DispatchFailed && d.State != DispatchCancelled) || t.State != d.State || t.Authorization != d.Authorization || t.Reason != d.Reason || r.Cycle == "" || r.CardRevision == 0 || r.At == "" || (!r.Legacy && (t.Revision != d.Revision || t.Decision != r.Decision)) || (r.Legacy && (strings.TrimSpace(r.Decision) == "" || t.Revision+1 != d.Revision)) {
		return dispatchError(d.Input.ID)
	}
	raw, err := tx.Read(d.Input.TaskID, dispatchPath(d.Input.ID, "release"))
	if err != nil {
		return err
	}
	var stored DispatchRelease
	if DecodeReviewJSON([]byte(raw), &stored) != nil || !reflect.DeepEqual(stored, *r) {
		return dispatchError(d.Input.ID)
	}
	if !r.Legacy {
		raw, err = tx.Read(d.Input.TaskID, dispatchPath(d.Input.ID, "termination"))
		if err != nil {
			return err
		}
		var original DispatchTermination
		if DecodeReviewJSON([]byte(raw), &original) != nil || original != t {
			return dispatchError(d.Input.ID)
		}
	}
	return nil
}

func cardReleases(s Snapshot) ([]DispatchRelease, error) {
	body, _ := SectionBody(s.Text, "DISPATCHES")
	var releases []DispatchRelease
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r DispatchRelease
		if json.Unmarshal([]byte(line), &r) != nil || r.Termination.Authorization.DispatchID == "" {
			return nil, dispatchError(s.Entry.TaskID)
		}
		releases = append(releases, r)
	}
	return releases, nil
}

func verifyCardRelease(tx *Transaction, s Snapshot, r DispatchRelease) error {
	d, err := readDispatch(tx, s.Entry.TaskID, r.Termination.Authorization.DispatchID)
	if err != nil {
		return err
	}
	if d.Release == nil || !reflect.DeepEqual(*d.Release, r) || r.CardRevision > s.Revision {
		return dispatchError(d.Input.ID)
	}
	return nil
}
