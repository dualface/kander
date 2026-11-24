package board

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	dispatchkernel "github.com/dualface/kander/internal/statemachine/dispatch"
)

// DispatchIntegration is produced by the Git-aware launch layer. Board verifies
// its structure and immutable copies, never interprets Git or claims integration.
type DispatchIntegration struct {
	IntegrationID         string    `json:"integration_id,omitempty"`
	IntegrationExecution  string    `json:"integration_execution,omitempty"`
	IntegrationGeneration uint64    `json:"integration_generation,omitzero"`
	DispatchID            string    `json:"dispatch_id"`
	TaskID                string    `json:"task_id"`
	CWD                   string    `json:"cwd"`
	SourceCommit          string    `json:"source_commit"`
	ReviewTarget          string    `json:"review_target"`
	ReviewBase            string    `json:"review_base"`
	RebasedBase           string    `json:"rebased_base,omitempty"`
	TargetCommit          string    `json:"target_commit"`
	TargetRef             string    `json:"target_ref"`
	Author                string    `json:"author"`
	Basis                 string    `json:"basis"`
	VerifiedAt            time.Time `json:"verified_at"`
}
type DispatchWrapUpBinding struct {
	Artifact ArtifactReference   `json:"artifact"`
	Git      DispatchIntegration `json:"git"`
}

// mainlineTargetRef accepts the local or origin ref of either mainline branch
// git.branch_mode can select. The structural check stays configuration-free;
// the Git verifier in launch narrows it to the configured branch.
func mainlineTargetRef(ref string) bool {
	switch ref {
	case "refs/heads/develop", "refs/remotes/origin/develop", "refs/heads/main", "refs/remotes/origin/main":
		return true
	}
	return false
}

// integrationTargetRef accepts the local or origin ref of a card's integration
// target. A card without a target field keeps the mainline rule. The Git
// verifier in launch narrows both to the one branch this checkout allows.
func integrationTargetRef(ref, target string) bool {
	if target == "" {
		return mainlineTargetRef(ref)
	}
	return ref == "refs/heads/"+target || ref == "refs/remotes/origin/"+target
}

// wrapUpCompletionError explains a refused wrap-up completion. The completion
// binds the integration source recorded in the evidence, not the card's own
// final commit, and the integration file is read-only evidence rather than a
// disposition, so both mistakes name the commit the completion needs.
func wrapUpCompletionError(source string, o MoveOptions) error {
	if o.Disposition != nil {
		return dispatchEvidenceError(fmt.Sprintf("wrap-up completion takes no --disposition (dispatches/<id>/integration.json is read-only evidence); complete with --delivery-commit %s and no --disposition", source))
	}
	if o.DeliveryCommit != source {
		return dispatchEvidenceError(fmt.Sprintf("wrap-up completion must bind the integration source: expected --delivery-commit %s (source_commit of the wrap-up evidence), got %q; read it from intent.evidence.wrap_up.git.source_commit of kander dispatch show", source, o.DeliveryCommit))
	}
	return nil
}

// validateDispatchWrapUp checks the integration of a wrap-up. A completed round
// (historical) was bound to the plan of its day: a batch appended later for
// another member's rebound cycle moves the end of the plan, not this delivery.
func validateDispatchWrapUp(tx *Transaction, in DispatchInput, published, historical bool) error {
	w := in.Evidence.WrapUp
	if w == nil {
		return dispatchEvidenceError("wrap-up binding required")
	}
	g := w.Git
	card, err := tx.Snapshot(in.TaskID)
	if err != nil {
		return err
	}
	target, err := IntegrationTargetOf(card.Text)
	if err != nil {
		return err
	}
	if w.Artifact != (ArtifactReference{in.TaskID, dispatchPath(in.ID, "integration")}) || g.DispatchID != in.ID || g.TaskID != in.TaskID || !validDispatchLocation(g.CWD) || g.SourceCommit != in.Base || !dispatchCommitPattern.MatchString(g.TargetCommit) || !validWrapUpTarget(g, target) || strings.TrimSpace(g.Author) == "" || strings.TrimSpace(g.Basis) == "" || g.VerifiedAt.IsZero() || g.VerifiedAt.After(time.Now()) {
		return dispatchEvidenceError("integration binding")
	}
	var reviewRange ReviewGitEdge
	if historical {
		plan, _, err := readTaskPlan(tx, in.TaskID)
		if err != nil {
			return err
		}
		reviewRange.Descendant = g.ReviewTarget
		if slices.ContainsFunc(plan.Batches, func(b ReviewPlanBatch) bool { return b.TargetCommit == g.ReviewTarget }) {
			reviewRange.Ancestor = plan.Batches[0].Base
		}
	} else {
		if reviewRange, err = dispatchReviewRange(tx, in.TaskID); err != nil {
			return err
		}
	}
	if g.RebasedBase == "" && g.SourceCommit != g.ReviewTarget || g.ReviewTarget != reviewRange.Descendant || g.ReviewBase != reviewRange.Ancestor || g.RebasedBase != "" && !dispatchCommitPattern.MatchString(g.RebasedBase) {
		return dispatchEvidenceError("integration does not bind final review target " + reviewRange.Descendant)
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

// AuthorizeDispatchWrapUp reconciles the same dispatch under CAS, archives its
// old epoch, and fences it before issuing a cleanup-and-records-only grant.
// The caller owns the delivery lease and verifies the exit observation first.
func AuthorizeDispatchWrapUpWithTransaction(root, task, id string, expected uint64, author, reason string, exit WrapUpExitEvidence, extensions ...TransactionExtension) (d Dispatch, err error) {
	d, err = ReadDispatch(root, task, id)
	if err != nil {
		return
	}
	scope, err := dispatchEvidenceScope(root, d.Input)
	if err != nil {
		return d, err
	}
	err = WithTransaction(root, scope, func(tx *Transaction) error {
		defer func() { tx.SetOperationResult(d) }()
		var e error
		d, e = readDispatch(tx, task, id)
		if e != nil {
			return e
		}
		s, e := tx.Expect(task, "", exit.CardRevision)
		if e != nil {
			return e
		}
		// The kernel decides the grant. The checks run in the order they have always
		// run, each probe holding one group of facts with the others true: the
		// conflict facts, the coordinator and the cycle, the exit, then the evidence.
		v := projectDispatch(s, &d)
		round, epoch := v.pair(d.Authorization)
		authorize := dispatchkernel.AuthorizeWrapUp{AuthRound: round, AuthEpoch: epoch, Cas: d.Revision == expected,
			Room: d.Authorization.Epoch != ^uint64(0), Col: kernelDispatchCol(s.Entry.State),
			Named: strings.TrimSpace(author) != "" && strings.TrimSpace(reason) != "", Cycle: true,
			Outcome: dispatchkernel.ExitStopped, Decided: true, Fresh: true, Evidence: true}
		if !v.allows(v.state, authorize) {
			return wrapUpRefusal(d, s, expected, authorize.Named)
		}
		if e = refuseBlockedWrapUp(tx, d.Input); e != nil {
			return e
		}
		cycleErr := requireDispatchCycle(d, s)
		authorize.Cycle = cycleErr == nil
		if !v.allows(v.state, authorize) {
			return cycleErr
		}
		authorize.Outcome, authorize.Decided = kernelExitOutcome(exit), strings.TrimSpace(exit.Decision) != ""
		authorize.Fresh = validDispatchExit(s, exit, time.Now())
		if !v.allows(v.state, authorize) {
			return dispatchEvidenceError("fresh confirmed exit required")
		}
		evidenceErr := validateDispatchEvidence(tx, d.Input, false)
		authorize.Evidence = evidenceErr == nil
		next, ok := v.next(authorize)
		if !ok {
			return evidenceErr
		}
		if e = tx.Put(task, dispatchPath(id, "execution-"+strconv.FormatUint(d.Authorization.Epoch, 10)), reviewJSON(d)); e != nil {
			return e
		}
		now := time.Now().UTC()
		grant := WrapUpAuthority{Epoch: v.real(next.Epoch), Previous: d.Authorization, Author: author, Reason: reason, Exit: exit, IssuedAt: now, ConfirmBy: now.Add(120 * time.Second)}
		d.Authorization.Epoch = grant.Epoch
		d.Revision++
		d.State = dispatchStates[next.Phase]
		d.Accepted, d.Completed = nil, nil
		d.Attempts = 0
		d.WrapUpAuthority = &grant
		d.Execution = nil
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
	}, extensions...)
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

// kernelExitOutcome maps the outcome of an exit observation to the kernel's.
func kernelExitOutcome(exit WrapUpExitEvidence) dispatchkernel.Exit {
	switch exit.Outcome {
	case "stopped":
		return dispatchkernel.ExitStopped
	case "reclaimed":
		return dispatchkernel.ExitReclaimed
	}
	return dispatchkernel.ExitOther
}

// requireFullExecution refuses a write that needs full execution (a start
// rollback, an execution identity write, an author conclusion) while the card's
// binding names a round that holds a wrap-up grant. The kernel decides; a card
// without binding has no record to read.
func (tx *Transaction) requireFullExecution(s Snapshot) error {
	v := projectDispatch(s, nil)
	if v.state.Bound {
		d, err := readDispatch(tx, s.Entry.TaskID, v.binding.DispatchID)
		if err != nil {
			return err
		}
		v = projectDispatch(s, &d)
	}
	if _, ok := v.next(dispatchkernel.Full{}); !ok {
		return dispatchEvidenceError("wrap-up-only authority cannot modify code, runtime identity or author conclusions")
	}
	return nil
}

// validateWrapUpUpdate limits a document write under a wrap-up grant to records:
// no contract decision, and an append to spec.md or the dedicated record path.
// The kernel decides; the facts it takes are computed in the order the checks
// have always run, each probe holding one fact with the others true, and the
// content of the record path is read only when the kernel needs it.
func (tx *Transaction) validateWrapUpUpdate(s Snapshot, o UpdateOptions) error {
	v := projectDispatch(s, nil)
	if v.state.Bound {
		d, err := readDispatch(tx, s.Entry.TaskID, v.binding.DispatchID)
		if err != nil {
			return err
		}
		v = projectDispatch(s, &d)
	}
	write := dispatchkernel.WrapUpWrite{Contract: o.ContractDecision != "", Spec: o.Document == "spec.md", Appends: true, Path: true, Same: true}
	if !v.allows(v.state, write) {
		return dispatchEvidenceError("wrap-up cannot change contract")
	}
	write.Contract = false
	// Dedicated append-only records keep predecessor text and authorship intact.
	prefix := strings.TrimRight(s.Text, "\n") + "\n\n## WRAP_UP_RECORDS\n\n"
	write.Appends = strings.HasPrefix(o.Text, prefix) && strings.TrimSpace(strings.TrimPrefix(o.Text, prefix)) != ""
	if !v.allows(v.state, write) {
		return dispatchEvidenceError("wrap-up spec update must append WRAP_UP_RECORDS: " + t("board.wrap_up_grant_hint"))
	}
	write.Path = o.Document == "wrap-up/"+v.binding.DispatchID+"-"+strconv.FormatUint(v.binding.Epoch, 10)+".md"
	if !v.allows(v.state, write) {
		return dispatchEvidenceError("wrap-up-only record path required: " + t("board.wrap_up_grant_hint"))
	}
	write.Same = false
	if v.allows(v.state, write) {
		return nil
	}
	// Do not overwrite a prior record even within the same authorization.
	raw, err := tx.Read(s.Entry.TaskID, o.Document)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	write.Same = err != nil || raw == o.Text
	if !v.allows(v.state, write) {
		return dispatchEvidenceError("immutable wrap-up record")
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
	// A batch with no member left never closes, so the plan ends before it.
	for _, b := range slices.Backward(plan.Batches) {
		void, err := voidReviewBatch(tx, b.TaskIDs)
		if err != nil {
			return ReviewGitEdge{}, err
		}
		if !void {
			return ReviewGitEdge{Ancestor: plan.Batches[0].Base, Descendant: b.TargetCommit}, nil
		}
	}
	return ReviewGitEdge{}, dispatchEvidenceError("sealed review plan required")
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

// validDispatchLocation retains historical machine paths while allowing new
// service records to name their project without importing client storage.
func validDispatchLocation(value string) bool {
	if project, ok := strings.CutPrefix(value, "project:"); ok {
		return ValidReviewID(project)
	}
	return filepath.IsAbs(value)
}

// Portable targets require a service identity and a complete receipt binding.
// The service independently checks the published immutable integration record.
func validWrapUpTarget(g DispatchIntegration, target string) bool {
	hasReceipt := g.IntegrationID != "" || g.IntegrationExecution != "" || g.IntegrationGeneration != 0
	if !hasReceipt {
		return integrationTargetRef(g.TargetRef, target)
	}
	if target != "" && g.TargetRef != "refs/heads/"+target {
		return false
	}
	project, portable := strings.CutPrefix(g.CWD, "project:")
	return portable && ValidReviewID(project) && strings.HasPrefix(g.TargetRef, "refs/heads/") && strings.HasPrefix(g.IntegrationID, "i-") && len(g.IntegrationID) == 66 && commitPattern.MatchString(strings.TrimPrefix(g.IntegrationID, "i-")) && len(g.IntegrationExecution) > 0 && len(g.IntegrationExecution) <= 128 && g.IntegrationGeneration > 0
}
