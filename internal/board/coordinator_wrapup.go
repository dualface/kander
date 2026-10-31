package board

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"time"
)

// WrapUpGitEvidence records that the Git facts of one wrap-up held once. It lives
// in the checkpoint directory of the group, so archive cleanup removes it with the
// checkpoint and no card write can reach it. It is never rewritten.
type WrapUpGitEvidence struct {
	Schema      int                 `json:"schema"`
	DispatchID  string              `json:"dispatch_id"`
	TaskID      string              `json:"task_id"`
	Epoch       uint64              `json:"epoch"`
	Integration DispatchIntegration `json:"integration"`
	VerifiedBy  string              `json:"verified_by"`
	VerifiedAt  string              `json:"verified_at"`
}

const (
	wrapUpEvidenceSchema        = 1
	wrapUpVerifiedByCompletion  = "completion"
	wrapUpVerifiedByReconcile   = "reconcile"
	wrapUpCompletionGitDeadline = 30 * time.Second
)

// WrapUpGitVerifier checks the Git facts of a wrap-up for the given board root.
type WrapUpGitVerifier func(ctx context.Context, root string, facts CoordinatorGitFacts) error

// ErrWrapUpGitDeferred tells a completion that no repository holding the facts is
// in reach, which says nothing about the facts. The move goes on, and the next
// reconcile verifies once, with the cwd its caller names.
var ErrWrapUpGitDeferred = errors.New("wrap-up Git verification deferred")

var wrapUpGitVerifier atomic.Pointer[WrapUpGitVerifier]

// SetWrapUpGitVerifier installs the Git-aware check of a wrap-up completion. Board
// never runs Git itself; a binary without a verifier leaves the first check to
// the next reconcile.
func SetWrapUpGitVerifier(fn WrapUpGitVerifier) { wrapUpGitVerifier.Store(&fn) }

func wrapUpEvidenceName(id string) string { return "wrap-up-git/" + id + ".json" }

func newWrapUpEvidence(d Dispatch, by string) WrapUpGitEvidence {
	return WrapUpGitEvidence{Schema: wrapUpEvidenceSchema, DispatchID: d.Input.ID, TaskID: d.Input.TaskID, Epoch: d.Authorization.Epoch, Integration: d.Input.Evidence.WrapUp.Git, VerifiedBy: by, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano)}
}

// readWrapUpEvidence reports whether the wrap-up has its record, and checks the
// structure of that record against the dispatch. Reconcile reads no Git here.
func readWrapUpEvidence(tx *Transaction, group string, d Dispatch) (bool, error) {
	if group == "" {
		return false, nil
	}
	b, ok, err := tx.ReadGroup(group, wrapUpEvidenceName(d.Input.ID))
	if err != nil || !ok {
		return false, err
	}
	var e WrapUpGitEvidence
	if err = DecodeReviewJSON(b, &e); err != nil {
		return true, err
	}
	if _, err = time.Parse(time.RFC3339Nano, e.VerifiedAt); err != nil || e.Schema != wrapUpEvidenceSchema || e.DispatchID != d.Input.ID || e.TaskID != d.Input.TaskID || e.Epoch != d.Authorization.Epoch || !reflect.DeepEqual(e.Integration, d.Input.Evidence.WrapUp.Git) || e.VerifiedBy != wrapUpVerifiedByCompletion && e.VerifiedBy != wrapUpVerifiedByReconcile {
		return true, dispatchEvidenceError("wrap-up Git evidence does not match its dispatch")
	}
	return true, nil
}

func putWrapUpEvidence(tx *Transaction, group string, e WrapUpGitEvidence) error {
	name := wrapUpEvidenceName(e.DispatchID)
	if _, ok, err := tx.ReadGroup(group, name); err != nil {
		return err
	} else if ok {
		return dispatchEvidenceError("immutable wrap-up Git evidence")
	}
	return tx.PutGroup(group, name, reviewJSON(e))
}

// wrapUpGitFacts names the facts a wrap-up rests on: its integration and the
// closures of the plan batches that bind it.
func wrapUpGitFacts(tx *Transaction, id string, d Dispatch) (CoordinatorGitFacts, error) {
	facts := CoordinatorGitFacts{Integration: d.Input.Evidence.WrapUp.Git}
	plan, _, err := readTaskPlan(tx, id)
	if err != nil {
		return facts, err
	}
	for _, b := range plan.Batches {
		if d.State == DispatchCompleted && !containsID(b.TaskIDs, id) {
			continue
		}
		var closure ReviewClosure
		ok, err := readReviewJSON(tx, closureName(b.BatchID), &closure)
		if err != nil {
			return facts, err
		}
		if !ok {
			void, err := voidReviewBatch(tx, b.TaskIDs)
			if err != nil {
				return facts, err
			}
			if void {
				continue
			}
			return facts, coordinatorError("closed batch missing")
		}
		facts.Closures = append(facts.Closures, closure)
	}
	return facts, nil
}

// verifyWrapUpCompletion runs before the transaction of a bound move into done, so
// Git runs with no lock held. The result binds the epoch; the move fails when the
// binding changed. It returns nil when no record applies: another kind of round, a
// replayed completion, a group with no checkpoint, a binary with no verifier, or a
// verifier with no repository in reach.
func verifyWrapUpCompletion(root string, entry Entry, o MoveOptions) (*wrapUpCompletion, error) {
	verifier := wrapUpGitVerifier.Load()
	if verifier == nil || o.Authorization.DispatchID == "" {
		return nil, nil
	}
	var result *wrapUpCompletion
	var facts CoordinatorGitFacts
	var d Dispatch
	err := WithTransaction(root, LockScope{Tasks: []string{entry.TaskID}, Groups: []string{reviewControlGroup}, ReadOnly: true}, func(tx *Transaction) error {
		s, e := tx.Snapshot(entry.TaskID)
		if e != nil {
			return e
		}
		if d, e = readDispatch(tx, entry.TaskID, o.Authorization.DispatchID); e != nil {
			return e
		}
		group := TaskGroupFrom(s.Text)
		if d.Input.Kind != "wrap-up" || d.Input.Evidence.WrapUp == nil || d.State == DispatchCompleted || group == "" {
			return nil
		}
		facts, e = wrapUpGitFacts(tx, entry.TaskID, d)
		result = &wrapUpCompletion{group: group}
		return e
	})
	if err != nil || result == nil {
		return nil, err
	}
	var exists bool
	if err = WithTransaction(root, LockScope{Groups: []string{result.group}, ReadOnly: true}, func(tx *Transaction) error {
		var e error
		_, exists, e = tx.ReadGroup(result.group, "checkpoint.json")
		return e
	}); err != nil || !exists {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), wrapUpCompletionGitDeadline)
	defer cancel()
	if err = (*verifier)(ctx, root, facts); errors.Is(err, ErrWrapUpGitDeferred) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	result.evidence = newWrapUpEvidence(d, wrapUpVerifiedByCompletion)
	return result, nil
}

// wrapUpCompletion is a verified record waiting for the transaction of its move.
type wrapUpCompletion struct {
	group    string
	evidence WrapUpGitEvidence
}

// stage writes the record with the move. A later epoch means another grant took
// the round after the check, so the check says nothing about it.
func (w *wrapUpCompletion) stage(tx *Transaction, task string) error {
	d, err := readDispatch(tx, task, w.evidence.DispatchID)
	if err != nil {
		return err
	}
	if d.Authorization.Epoch != w.evidence.Epoch {
		return kanbanError("board.transaction_conflict", task)
	}
	return putWrapUpEvidence(tx, w.group, w.evidence)
}
