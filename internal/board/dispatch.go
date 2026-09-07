package board

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// DispatchState separates durable business receipts from uncertain transport.
type DispatchState string

const (
	DispatchPrepared  DispatchState = "prepared"
	DispatchUnknown   DispatchState = "delivery-unknown"
	DispatchAccepted  DispatchState = "accepted"
	DispatchCompleted DispatchState = "completed"
	DispatchFailed    DispatchState = "failed"
	DispatchCancelled DispatchState = "cancelled"
)

// ArtifactReference survives card moves; Path is relative to the named card.
// Evidence producers, not this storage layer, interpret the referenced content.
type ArtifactReference struct {
	TaskID string `json:"task_id"`
	Path   string `json:"path"`
}

// DispatchInput is immutable. Empty timestamps select defaults only on creation.
type DispatchInput struct {
	ID         string              `json:"dispatch_id"`
	TaskID     string              `json:"task_id"`
	Kind       string              `json:"kind"`
	Message    string              `json:"message"`
	Base       string              `json:"base"`
	Evidence   DispatchEvidence    `json:"evidence,omitempty"`
	References []ArtifactReference `json:"references,omitempty"`
	CreatedAt  time.Time           `json:"created_at"`
	ConfirmBy  time.Time           `json:"confirm_by"`
}

// ExecutionAuthorization is explicit on author writes, never inferred from the
// latest snapshot. It is a fencing token, not a malicious-local-user boundary.
type ExecutionAuthorization struct {
	DispatchID string `json:"dispatch_id"`
	Epoch      uint64 `json:"epoch"`
}

// DispatchReceipt binds the state transition and its evidence to one revision.
type DispatchReceipt struct {
	At             time.Time          `json:"at"`
	CardRevision   uint64             `json:"card_revision"`
	State          string             `json:"state"`
	DeliveryCommit string             `json:"delivery_commit,omitempty"`
	Disposition    *ArtifactReference `json:"disposition,omitempty"`
}

// Dispatch is the committed protocol state. Transport never supplies Accepted.
type Dispatch struct {
	WrapUpAuthority *WrapUpAuthority       `json:"wrap_up_authority,omitempty"`
	Schema          int                    `json:"schema"`
	Input           DispatchInput          `json:"intent"`
	MessageHash     string                 `json:"message_hash"`
	Authorization   ExecutionAuthorization `json:"authorization"`
	Revision        uint64                 `json:"revision"`
	State           DispatchState          `json:"state"`
	Attempts        uint64                 `json:"attempts"`
	Accepted        *DispatchReceipt       `json:"accepted,omitempty"`
	Completed       *DispatchReceipt       `json:"completed,omitempty"`
	Reason          string                 `json:"reason,omitempty"`
}

const dispatchRegistry = "00000000-dispatch-group"

var dispatchIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var dispatchCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// NewDispatchID allocates a printable identity before any external send.
func NewDispatchID() (string, error)      { return operationID() }
func validDispatchID(id string) bool      { return len(id) <= 128 && dispatchIDPattern.MatchString(id) }
func dispatchError(id string) error       { return kanbanError("board.dispatch_conflict", id) }
func dispatchPath(id, name string) string { return "dispatches/" + id + "/" + name + ".json" }
func messageHash(s string) string         { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func authFrom(text string) ExecutionAuthorization {
	epoch, _ := strconv.ParseUint(MetadataFrom(text, "EXECUTION_EPOCH"), 10, 64)
	return ExecutionAuthorization{MetadataFrom(text, "DISPATCH_ID"), epoch}
}
func validReference(r ArtifactReference) bool {
	if _, err := NormalizeTaskID(r.TaskID); err != nil {
		return false
	}
	_, err := documentPath(Entry{Path: "card", Kind: "large"}, r.Path)
	return err == nil
}
func validateDispatchInput(in DispatchInput) error {
	if !validDispatchID(in.ID) || !taskIDRe.MatchString(in.TaskID) || (in.Kind != "fix" && in.Kind != "sync" && in.Kind != "wrap-up") || strings.TrimSpace(in.Message) == "" || !utf8.ValidString(in.Message) || !dispatchCommitPattern.MatchString(in.Base) {
		return dispatchError(in.ID)
	}
	for _, ref := range in.References {
		if !validReference(ref) {
			return dispatchError(in.ID)
		}
	}
	return nil
}
func sameDispatchInput(a, b DispatchInput) bool {
	// Retry defaults must not reset durable creation or acknowledgement deadlines.
	if a.CreatedAt.IsZero() {
		a.CreatedAt = b.CreatedAt
	}
	if a.ConfirmBy.IsZero() {
		a.ConfirmBy = b.ConfirmBy
	}
	return reflect.DeepEqual(a, b)
}
func putDispatch(tx *Transaction, d Dispatch) error {
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return tx.Put(d.Input.TaskID, dispatchPath(d.Input.ID, "state"), string(b)+"\n")
}
func readDispatch(tx *Transaction, task, id string) (Dispatch, error) {
	var d Dispatch
	if !validDispatchID(id) {
		return d, dispatchError(id)
	}
	raw, err := tx.Read(task, dispatchPath(id, "state"))
	if err != nil {
		return d, err
	}
	if err = json.Unmarshal([]byte(raw), &d); err != nil {
		return d, err
	}
	intent, err := tx.Read(task, dispatchPath(id, "intent"))
	if err != nil {
		return d, err
	}
	var original Dispatch
	if err = json.Unmarshal([]byte(intent), &original); err != nil {
		return d, err
	}
	if d.Schema != 1 || d.Input.TaskID != task || d.Input.ID != id || validateDispatchInput(d.Input) != nil || d.MessageHash != messageHash(d.Input.Message) || !reflect.DeepEqual(original.Input, d.Input) || original.MessageHash != d.MessageHash || d.Authorization.DispatchID != id || d.Authorization.Epoch == 0 || d.Revision == 0 || d.Input.CreatedAt.IsZero() || !d.Input.ConfirmBy.After(d.Input.CreatedAt) {
		return d, dispatchError(id)
	}
	switch d.State {
	case DispatchPrepared, DispatchUnknown, DispatchAccepted, DispatchCompleted, DispatchFailed, DispatchCancelled:
	default:
		return d, dispatchError(id)
	}
	for name, receipt := range map[string]*DispatchReceipt{"accepted": d.Accepted, "completed": d.Completed} {
		if receipt == nil {
			continue
		}
		raw, err := tx.Read(task, dispatchPath(id, name+"-"+strconv.FormatUint(d.Authorization.Epoch, 10)))
		if err != nil {
			return d, err
		}
		var stored DispatchReceipt
		if json.Unmarshal([]byte(raw), &stored) != nil || !reflect.DeepEqual(stored, *receipt) {
			return d, dispatchError(id)
		}
	}
	if (d.State == DispatchAccepted || d.State == DispatchCompleted) && d.Accepted == nil || d.State == DispatchCompleted && d.Completed == nil {
		return d, dispatchError(id)
	}
	if err := validateWrapUpAuthority(tx, d); err != nil {
		return d, err
	}
	return d, nil
}

// ReadDispatch reads intent and receipts under one shared card lock.
func ReadDispatch(root, task, id string) (d Dispatch, err error) {
	err = WithTransaction(root, LockScope{Tasks: []string{task}, ReadOnly: true}, func(tx *Transaction) error { var e error; d, e = readDispatch(tx, task, id); return e })
	return
}

// PrepareDispatch publishes a globally unique immutable intent and fences the
// previous completed execution. Only one nonterminal intent can own a card.
func PrepareDispatch(root string, in DispatchInput) (d Dispatch, err error) {
	if in.ID == "" {
		in.ID, err = NewDispatchID()
		if err != nil {
			return
		}
	}
	in.CreatedAt = in.CreatedAt.UTC()
	in.ConfirmBy = in.ConfirmBy.UTC()
	if len(in.References) == 0 {
		in.References = nil
	}
	if err = validateDispatchInput(in); err != nil {
		return
	}
	scope, e := dispatchEvidenceScope(root, in)
	if e != nil {
		return d, e
	}
	err = WithTransaction(root, scope, func(tx *Transaction) error {
		return tx.prepareDispatch(in, &d)
	})
	return
}

func (tx *Transaction) prepareDispatch(in DispatchInput, d *Dispatch) error {
	registered, exists, e := tx.ReadGroup(dispatchRegistry, in.ID+".json")
	if e != nil {
		return e
	}
	if exists {
		var old DispatchInput
		if json.Unmarshal(registered, &old) != nil || !sameDispatchInput(in, old) {
			return dispatchError(in.ID)
		}
		*d, e = readDispatch(tx, in.TaskID, in.ID)
		return e
	}
	if e = validateDispatchEvidence(tx, in, true); e != nil {
		return e
	}
	s, e := tx.Snapshot(in.TaskID)
	if e != nil {
		return e
	}
	if s.Entry.State != "working" && s.Entry.State != "review" {
		return dispatchError(in.ID)
	}
	current := authFrom(s.Text)
	if current.DispatchID != "" {
		previous, e := readDispatch(tx, in.TaskID, current.DispatchID)
		if e != nil {
			return e
		}
		if previous.State != DispatchCompleted && previous.State != DispatchFailed && previous.State != DispatchCancelled {
			return dispatchError(in.ID)
		}
	}
	if current.Epoch == ^uint64(0) {
		return dispatchError(in.ID)
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = time.Now().UTC()
	}
	if in.ConfirmBy.IsZero() {
		in.ConfirmBy = in.CreatedAt.Add(120 * time.Second)
	}
	if !in.ConfirmBy.After(in.CreatedAt) {
		return dispatchError(in.ID)
	}
	*d = Dispatch{Schema: 1, Input: in, MessageHash: messageHash(in.Message), Authorization: ExecutionAuthorization{in.ID, current.Epoch + 1}, Revision: 1, State: DispatchPrepared}
	text, e := setMetadata(s.Text, "DISPATCH_ID", in.ID)
	if e != nil {
		return e
	}
	text, e = setMetadata(text, "EXECUTION_EPOCH", strconv.FormatUint(d.Authorization.Epoch, 10))
	if e != nil {
		return e
	}
	b, _ := json.Marshal(in)
	if e = tx.PutGroup(dispatchRegistry, in.ID+".json", string(b)+"\n"); e != nil {
		return e
	}
	if w := in.Evidence.WrapUp; w != nil {
		if e = tx.Put(in.TaskID, w.Artifact.Path, reviewJSON(w.Git)); e != nil {
			return e
		}
	}
	original, _ := json.Marshal(d)
	if e = tx.Put(in.TaskID, dispatchPath(in.ID, "intent"), string(original)+"\n"); e != nil {
		return e
	}
	if e = putDispatch(tx, *d); e != nil {
		return e
	}
	return tx.Put(in.TaskID, "spec.md", text)
}

// BeginDispatchAttempt CASes an uncertain send before invoking any external
// transport. Retries require a new observation and the same intent revision.
func BeginDispatchAttempt(root, task, id string, expected uint64, cardRevision ...uint64) (d Dispatch, err error) {
	d, err = ReadDispatch(root, task, id)
	if err != nil {
		return
	}
	scope, err := dispatchEvidenceScope(root, d.Input)
	if err != nil {
		return d, err
	}
	err = WithTransaction(root, scope, func(tx *Transaction) error {
		var e error
		d, e = readDispatch(tx, task, id)
		if e != nil {
			return e
		}
		s, e := tx.Snapshot(task)
		if e != nil {
			return e
		}
		if d.WrapUpAuthority != nil {
			return dispatchEvidenceError("wrap-up-only grant cannot launch or deliver")
		}
		if e = validateDispatchEvidence(tx, d.Input, false); e != nil {
			return e
		}
		if len(cardRevision) > 0 && s.Revision != cardRevision[0] {
			return dispatchError(id)
		}
		if d.Revision != expected || authFrom(s.Text) != d.Authorization || (d.State != DispatchPrepared && d.State != DispatchUnknown) || !time.Now().Before(d.Input.ConfirmBy) {
			return dispatchError(id)
		}
		d.State = DispatchUnknown
		d.Revision++
		d.Attempts++
		return putDispatch(tx, d)
	})
	return
}

// EndDispatch records an explicit failure/cancellation decision; it never
// infers a terminal state merely from a transport timeout.
func EndDispatch(root, task, id string, expected uint64, state DispatchState, reason string) error {
	if (state != DispatchFailed && state != DispatchCancelled) || strings.TrimSpace(reason) == "" {
		return dispatchError(id)
	}
	return WithTransaction(root, LockScope{Tasks: []string{task}}, func(tx *Transaction) error {
		d, e := readDispatch(tx, task, id)
		if e != nil {
			return e
		}
		s, e := tx.Snapshot(task)
		if e != nil {
			return e
		}
		if d.Revision != expected || authFrom(s.Text) != d.Authorization || d.State == DispatchCompleted || d.State == DispatchCancelled || d.State == DispatchFailed {
			return dispatchError(id)
		}
		d.State = state
		d.Reason = reason
		d.Revision++
		return putDispatch(tx, d)
	})
}

// ReauthorizeDispatch fences the previous executor on an explicitly authorized
// takeover. Old receipts remain immutable in their epoch-specific paths.
// The original confirmation deadline is deliberately not extended.
func ReauthorizeDispatch(root, task, id string, expected uint64) (d Dispatch, err error) {
	err = WithTransaction(root, LockScope{Tasks: []string{task}}, func(tx *Transaction) error {
		var e error
		d, e = readDispatch(tx, task, id)
		if e != nil {
			return e
		}
		s, e := tx.Snapshot(task)
		if e != nil {
			return e
		}
		if d.WrapUpAuthority != nil || d.Revision != expected || authFrom(s.Text) != d.Authorization || d.State == DispatchCompleted || d.State == DispatchFailed || d.State == DispatchCancelled || d.Authorization.Epoch == ^uint64(0) || !time.Now().Before(d.Input.ConfirmBy) {
			return dispatchError(id)
		}
		b, _ := json.Marshal(d)
		if e = tx.Put(task, dispatchPath(id, "execution-"+strconv.FormatUint(d.Authorization.Epoch, 10)), string(b)+"\n"); e != nil {
			return e
		}
		d.Authorization.Epoch++
		d.Revision++
		d.State = DispatchPrepared
		d.Accepted = nil
		d.Completed = nil
		d.Attempts = 0
		text, e := setMetadata(s.Text, "EXECUTION_EPOCH", strconv.FormatUint(d.Authorization.Epoch, 10))
		if e != nil {
			return e
		}
		if e = tx.Put(task, "spec.md", text); e != nil {
			return e
		}
		return putDispatch(tx, d)
	})
	return
}
