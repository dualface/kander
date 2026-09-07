package board

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// DispatchIntegration is produced by the Git-aware launch layer. Board verifies
// its structure and immutable copies, never interprets Git or claims integration.
type DispatchIntegration struct {
	DispatchID   string    `json:"dispatch_id"`
	TaskID       string    `json:"task_id"`
	CWD          string    `json:"cwd"`
	SourceCommit string    `json:"source_commit"`
	ReviewTarget string    `json:"review_target"`
	ReviewBase   string    `json:"review_base"`
	RebasedBase  string    `json:"rebased_base,omitempty"`
	TargetCommit string    `json:"target_commit"`
	TargetRef    string    `json:"target_ref"`
	Author       string    `json:"author"`
	Basis        string    `json:"basis"`
	VerifiedAt   time.Time `json:"verified_at"`
}
type DispatchWrapUpBinding struct {
	Artifact ArtifactReference   `json:"artifact"`
	Git      DispatchIntegration `json:"git"`
}

func validateDispatchWrapUp(tx *Transaction, in DispatchInput, published bool) error {
	w := in.Evidence.WrapUp
	if w == nil {
		return dispatchEvidenceError("wrap-up binding required")
	}
	g := w.Git
	if w.Artifact != (ArtifactReference{in.TaskID, dispatchPath(in.ID, "integration")}) || g.DispatchID != in.ID || g.TaskID != in.TaskID || !filepath.IsAbs(g.CWD) || g.SourceCommit != in.Base || !dispatchCommitPattern.MatchString(g.TargetCommit) || (g.TargetRef != "refs/heads/develop" && g.TargetRef != "refs/remotes/origin/develop") || strings.TrimSpace(g.Author) == "" || strings.TrimSpace(g.Basis) == "" || g.VerifiedAt.IsZero() || g.VerifiedAt.After(time.Now()) {
		return dispatchEvidenceError("integration binding")
	}
	reviewRange, err := dispatchReviewRange(tx, in.TaskID)
	if err != nil {
		return err
	}
	if g.RebasedBase == "" && g.SourceCommit != g.ReviewTarget || g.ReviewTarget != reviewRange.Descendant || g.ReviewBase != reviewRange.Ancestor || g.RebasedBase != "" && !dispatchCommitPattern.MatchString(g.RebasedBase) {
		return dispatchEvidenceError("integration does not bind final review target")
	}
	if published {
		raw, err := tx.Read(in.TaskID, w.Artifact.Path)
		if err != nil {
			return err
		}
		if raw != reviewJSON(g) {
			return dispatchEvidenceError("integration original mismatch")
		}
	}
	return nil
}

// WrapUpExitEvidence carries a freshly verified observation, not an error code,
// timeout or lease expiration. Reclaimed records additionally cite prior user authority.
type WrapUpExitEvidence struct {
	Outcome      string    `json:"outcome"`
	Decision     string    `json:"decision,omitempty"`
	CardRevision uint64    `json:"card_revision"`
	Session      string    `json:"session"`
	Window       string    `json:"window"`
	Owner        string    `json:"owner"`
	StartedAt    string    `json:"started_at"`
	ObservedAt   time.Time `json:"observed_at"`
}
type WrapUpAuthority struct {
	Epoch     uint64                 `json:"epoch"`
	Previous  ExecutionAuthorization `json:"previous"`
	Author    string                 `json:"author"`
	Reason    string                 `json:"reason"`
	Exit      WrapUpExitEvidence     `json:"exit"`
	IssuedAt  time.Time              `json:"issued_at"`
	ConfirmBy time.Time              `json:"confirm_by"`
}

func dispatchAcceptBefore(d Dispatch) time.Time {
	if d.WrapUpAuthority != nil {
		return d.WrapUpAuthority.ConfirmBy
	}
	return d.Input.ConfirmBy
}

// AuthorizeDispatchWrapUp reconciles the same dispatch under CAS, archives its
// old epoch, and fences it before issuing a cleanup-and-records-only grant.
// The caller owns the delivery lease and verifies the exit observation first.
func AuthorizeDispatchWrapUp(root, task, id string, expected uint64, author, reason string, exit WrapUpExitEvidence) (d Dispatch, err error) {
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
		s, e := tx.Expect(task, "", exit.CardRevision)
		if e != nil {
			return e
		}
		if d.Input.Kind != "wrap-up" || d.Revision != expected || d.Authorization != authFrom(s.Text) || d.State == DispatchCompleted || d.State == DispatchFailed || d.State == DispatchCancelled || d.WrapUpAuthority != nil || d.Authorization.Epoch == ^uint64(0) || (s.Entry.State != "review" && s.Entry.State != "working") || strings.TrimSpace(author) == "" || strings.TrimSpace(reason) == "" {
			return dispatchEvidenceError("wrap-up authority conflict")
		}
		if (exit.Outcome != "stopped" && exit.Outcome != "reclaimed") || exit.Outcome == "reclaimed" && strings.TrimSpace(exit.Decision) == "" || exit.Session == "" || exit.Session != MetadataFrom(s.Text, FieldSession) || exit.Window != MetadataFrom(s.Text, FieldWindow) || exit.Owner != MetadataFrom(s.Text, FieldOwner) || exit.StartedAt != MetadataFrom(s.Text, FieldStartedAt) || exit.ObservedAt.IsZero() || exit.ObservedAt.After(time.Now()) || time.Since(exit.ObservedAt) > 30*time.Second {
			return dispatchEvidenceError("fresh confirmed exit required")
		}
		if e = validateDispatchEvidence(tx, d.Input, false); e != nil {
			return e
		}
		if e = tx.Put(task, dispatchPath(id, "execution-"+strconv.FormatUint(d.Authorization.Epoch, 10)), reviewJSON(d)); e != nil {
			return e
		}
		now := time.Now().UTC()
		grant := WrapUpAuthority{Epoch: d.Authorization.Epoch + 1, Previous: d.Authorization, Author: author, Reason: reason, Exit: exit, IssuedAt: now, ConfirmBy: now.Add(120 * time.Second)}
		d.Authorization.Epoch++
		d.Revision++
		d.State = DispatchPrepared
		d.Accepted, d.Completed = nil, nil
		d.Attempts = 0
		d.WrapUpAuthority = &grant
		text, e := setMetadata(s.Text, "EXECUTION_EPOCH", strconv.FormatUint(d.Authorization.Epoch, 10))
		if e != nil {
			return e
		}
		if e = tx.Put(task, dispatchPath(id, "wrap-up-authority-"+strconv.FormatUint(d.Authorization.Epoch, 10)), reviewJSON(grant)); e != nil {
			return e
		}
		if e = putDispatch(tx, d); e != nil {
			return e
		}
		return tx.Put(task, "spec.md", text)
	})
	return
}

func validateWrapUpAuthority(tx *Transaction, d Dispatch) error {
	a := d.WrapUpAuthority
	if a == nil {
		return nil
	}
	if d.Input.Kind != "wrap-up" || a.Epoch != d.Authorization.Epoch || a.Previous.DispatchID != d.Input.ID || a.Previous.Epoch == ^uint64(0) || a.Previous.Epoch+1 != a.Epoch || a.Author == "" || a.Reason == "" || a.IssuedAt.IsZero() || !a.ConfirmBy.After(a.IssuedAt) {
		return dispatchEvidenceError("wrap-up authority binding")
	}
	raw, err := tx.Read(d.Input.TaskID, dispatchPath(d.Input.ID, "wrap-up-authority-"+strconv.FormatUint(a.Epoch, 10)))
	if err != nil {
		return err
	}
	if raw != reviewJSON(a) {
		return dispatchEvidenceError("wrap-up authority original mismatch")
	}
	var previous Dispatch
	raw, err = tx.Read(d.Input.TaskID, dispatchPath(d.Input.ID, "execution-"+strconv.FormatUint(a.Previous.Epoch, 10)))
	if err != nil {
		return err
	}
	if json.Unmarshal([]byte(raw), &previous) != nil || previous.Authorization != a.Previous || !reflect.DeepEqual(previous.Input, d.Input) {
		return dispatchEvidenceError("isolated epoch original mismatch")
	}
	return nil
}

func (tx *Transaction) requireFullExecution(s Snapshot) error {
	a := authFrom(s.Text)
	if a.DispatchID == "" {
		return nil
	}
	d, err := readDispatch(tx, s.Entry.TaskID, a.DispatchID)
	if err != nil {
		return err
	}
	if d.WrapUpAuthority != nil {
		return dispatchEvidenceError("wrap-up-only authority cannot modify code, runtime identity or author conclusions")
	}
	return nil
}

func (tx *Transaction) validateWrapUpUpdate(s Snapshot, o UpdateOptions) error {
	a := authFrom(s.Text)
	if a.DispatchID == "" {
		return nil
	}
	d, err := readDispatch(tx, s.Entry.TaskID, a.DispatchID)
	if err != nil || d.WrapUpAuthority == nil {
		return err
	}
	if o.ContractDecision != "" {
		return dispatchEvidenceError("wrap-up cannot change contract")
	}
	// Dedicated append-only records keep predecessor text and authorship intact.
	if o.Document == "spec.md" {
		prefix := strings.TrimRight(s.Text, "\n") + "\n\n## WRAP_UP_RECORDS\n\n"
		if !strings.HasPrefix(o.Text, prefix) || strings.TrimSpace(strings.TrimPrefix(o.Text, prefix)) == "" {
			return dispatchEvidenceError("wrap-up spec update must append WRAP_UP_RECORDS")
		}
		return nil
	}
	if o.Document != "wrap-up/"+d.Input.ID+"-"+strconv.FormatUint(a.Epoch, 10)+".md" {
		return dispatchEvidenceError("wrap-up-only record path required")
	}
	// Do not overwrite a prior record even within the same authorization.
	raw, err := tx.Read(s.Entry.TaskID, o.Document)
	if err == nil && raw != o.Text {
		return dispatchEvidenceError("immutable wrap-up record")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func dispatchReviewRange(tx *Transaction, task string) (ReviewGitEdge, error) {
	if _, err := validateTaskReview(tx, task, true); err != nil {
		return ReviewGitEdge{}, err
	}
	plan, exists, err := readTaskPlan(tx, task)
	if err != nil {
		return ReviewGitEdge{}, err
	}
	if !exists || len(plan.Batches) == 0 {
		return ReviewGitEdge{}, dispatchEvidenceError("sealed review plan required")
	}
	return ReviewGitEdge{Ancestor: plan.Batches[0].Base, Descendant: plan.Batches[len(plan.Batches)-1].TargetCommit}, nil
}

// DispatchReviewRange identifies the closed plan range to bind into Git evidence.
func DispatchReviewRange(root, task string) (reviewRange ReviewGitEdge, err error) {
	scope, err := reviewGateScope(root, task, false)
	if err != nil {
		return ReviewGitEdge{}, err
	}
	scope.ReadOnly = true
	err = WithTransaction(root, scope, func(tx *Transaction) error { var e error; reviewRange, e = dispatchReviewRange(tx, task); return e })
	return
}
