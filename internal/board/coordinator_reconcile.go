package board

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
)

func reconcileCoordinatorMember(ctx context.Context, tx *Transaction, id string, old CoordinatorMember, o CoordinatorObservation, verify func(context.Context, CoordinatorGitFacts) error) (m CoordinatorMember, err error) {
	s, err := tx.Expect(id, "", o.Revision)
	if err != nil {
		return m, err
	}
	if s.Revision < old.Revision {
		return m, coordinatorError("member revision or execution cycle changed: " + id)
	}
	if s.Entry.State == "archived" || s.Entry.State == "trash" {
		return reconcileTerminalMember(ctx, tx, s, old, o)
	}
	if err := verifyCoordinatorHandoffs(tx, s, old); err != nil {
		return m, err
	}
	l, walk, err := coordinatorWalkChain(ctx, tx, s, old)
	if err != nil {
		return m, err
	}
	m = CoordinatorMember{Revision: s.Revision, State: s.Entry.State, Cycle: walk.cycle, AwaitingStart: walk.awaiting, StartAttempt: walk.attempt, DeliveryCommit: old.DeliveryCommit, TransitionSeq: walk.seq, DispatchSeq: old.DispatchSeq}

	m.ReleasedDispatches = append([]ArtifactReference(nil), old.ReleasedDispatches...)
	m.Handoffs = append([]lifecycleHandoff(nil), old.Handoffs...)
	handoffs, err := reclaimHandoffs(tx, s, walk)
	if err != nil {
		return m, err
	}
	m.Handoffs = append(m.Handoffs, handoffs...)
	if walk.cycle != old.Cycle && !walk.awaiting && !old.AwaitingStart && old.Cycle != emptyCycle(id) {
		// A previous cycle's delivery is history, never this executor's delivery.
		m.DeliveryCommit = ""
	}
	unconfirmed := l.current != nil && !l.current.ExecutionTerminal() && l.current.Status != ExecutionRunning
	if unconfirmed && o.DeliveryCommit != "" {
		return m, coordinatorError("unconfirmed start cannot establish delivery")
	}
	p, err := validateTaskReview(tx, id, false)
	if err != nil {
		return m, err
	}
	if p.PlanID != "" {
		plan, _, e := readTaskPlan(tx, id)
		if e != nil {
			return m, e
		}
		m.Review = &CoordinatorReview{PlanID: p.PlanID, Status: p.Status, Batches: []string{}, Runs: []ArtifactReference{}}
		for _, batch := range plan.Batches {
			if !containsID(batch.TaskIDs, id) {
				continue
			}
			m.Review.Batches = append(m.Review.Batches, batch.BatchID)
			var actual ReviewBatch
			if _, e = readReviewJSON(tx, reviewBatchName(batch.BatchID), &actual); e != nil {
				return m, e
			}
			runs, e := batchRuns(tx, actual)
			if e != nil {
				return m, e
			}
			for _, run := range runs {
				if run.Phase != "finalized" || !allPublished(run) {
					continue
				}
				if e = verifyPublishedReview(tx, run); e != nil {
					return m, e
				}
				m.Review.Runs = append(m.Review.Runs, ArtifactReference{id, reviewIndex(run).Report})
			}
		}
	}
	if unconfirmed {
		// Rounds wait with the member: the mirror adopts none of them until a
		// durable result ends the attempt, and the chain walk catches up then.
		m.Dispatch = old.Dispatch
		return m, nil
	}
	rounds, err := dispatchRounds(tx, s)
	if err != nil {
		return m, err
	}
	a := authFrom(s.Text)
	if err = coordinatorAdvanceRounds(tx, s, &m, old, rounds, a.DispatchID); err != nil {
		return m, err
	}
	if a == (ExecutionAuthorization{}) {
		if o.DispatchID != "" || o.Epoch != 0 || o.Base != "" {
			return m, coordinatorError("unbound member cannot acknowledge a dispatch: " + id)
		}
		if o.DeliveryCommit != "" {
			branch := MetadataFrom(s.Text, "TASK_BRANCH")
			if s.Entry.State != "review" || branch == "" || !dispatchCommitPattern.MatchString(o.DeliveryCommit) || verify == nil {
				return m, coordinatorError("first delivery requires review state, task branch and Git verification")
			}
			if err = verify(ctx, CoordinatorGitFacts{Delivery: &CoordinatorDelivery{TaskID: id, Branch: branch, Commit: o.DeliveryCommit}}); err != nil {
				return m, err
			}
			m.DeliveryCommit = o.DeliveryCommit
		}
		// State alone never supplies a delivery or a business receipt.
		return m, nil
	}
	if a != (ExecutionAuthorization{DispatchID: o.DispatchID, Epoch: o.Epoch}) {
		return m, coordinatorError("wrong dispatch round: " + id)
	}
	d, err := readDispatch(tx, id, a.DispatchID)
	if err != nil {
		return m, err
	}
	if d.Authorization != a || d.Input.Base != o.Base {
		return m, coordinatorError("dispatch base or epoch: " + id)
	}
	if d.Input.Kind == "fix" && d.State == DispatchCompleted {
		// A closed later round may supersede the batch target. Verify immutable
		// lineage without requiring an old completed intent to be sendable again.
		if err = validateDispatchFix(tx, d.Input, false, true); err != nil {
			return m, err
		}
	} else if err = validateDispatchEvidence(tx, d.Input, false); err != nil {
		return m, err
	}
	if _, err = snapshotDispatch(tx, id, s.Text, s.Revision); err != nil {
		return m, err
	}
	m.Dispatch = mirrorDispatch(id, d)
	if d.Completed != nil {
		if d.Completed.DeliveryCommit != o.DeliveryCommit || !dispatchCommitPattern.MatchString(o.DeliveryCommit) || d.Completed.State != "review" && d.Completed.State != "done" {
			return m, coordinatorError("completion delivery mismatch: " + id)
		}
		m.DeliveryCommit = d.Completed.DeliveryCommit
	} else if o.DeliveryCommit != "" {
		return m, coordinatorError("delivery has no completed receipt: " + id)
	}
	if w := d.Input.Evidence.WrapUp; w != nil {
		if verify == nil {
			return m, coordinatorError("Git-aware reconciliation required")
		}
		facts := CoordinatorGitFacts{Integration: w.Git}
		plan, _, e := readTaskPlan(tx, id)
		if e != nil {
			return m, e
		}
		for _, b := range plan.Batches {
			var closure ReviewClosure
			ok, e := readReviewJSON(tx, closureName(b.BatchID), &closure)
			if e != nil {
				return m, e
			}
			if !ok {
				return m, coordinatorError("closed batch missing")
			}
			facts.Closures = append(facts.Closures, closure)
		}
		if err = verify(ctx, facts); err != nil {
			return m, err
		}
		if d.State == DispatchCompleted && (s.Entry.State != "done" && s.Entry.State != "archived" || MetadataFrom(s.Text, "RESULT") != "completed") {
			return m, coordinatorError("wrap-up completion state: " + id)
		}
	}
	return m, ctx.Err()
}

// mirrorDispatch copies the facts of one round into a checkpoint.
func mirrorDispatch(id string, d Dispatch) *CoordinatorDispatch {
	m := &CoordinatorDispatch{ID: d.Input.ID, Epoch: d.Authorization.Epoch, Base: d.Input.Base, Kind: d.Input.Kind, Revision: d.Revision, State: d.State, Intent: ArtifactReference{id, dispatchPath(d.Input.ID, "intent")}}
	terminal := coordinatorDispatchTerminal(d.State)
	m.PendingConfirmation = !terminal && d.Accepted == nil
	m.PendingDelivery = !terminal && d.Input.Kind != "wrap-up"
	m.PendingWrapUp = !terminal && d.Input.Kind == "wrap-up"
	if w := d.Input.Evidence.WrapUp; w != nil {
		ref := w.Artifact
		m.Integration = &ref
		if d.WrapUpAuthority != nil {
			ref := ArtifactReference{id, dispatchPath(d.Input.ID, "wrap-up-authority-"+strconv.FormatUint(d.Authorization.Epoch, 10))}
			m.WrapUpAuthority = &ref
		}
	}
	return m
}

func coordinatorDispatchAdvance(tx *Transaction, id string, old CoordinatorDispatch, d Dispatch) error {
	if old.ID != d.Input.ID {
		previous, err := readDispatch(tx, id, old.ID)
		if err != nil {
			return err
		}
		if previous.Authorization.Epoch < old.Epoch || !coordinatorDispatchTerminal(previous.State) {
			return coordinatorError("unfinished previous dispatch: " + id)
		}
		return nil
	}
	if old.Epoch == d.Authorization.Epoch {
		if d.Revision < old.Revision || coordinatorDispatchTerminal(old.State) && d.State != old.State {
			return coordinatorError("dispatch facts regressed: " + id)
		}
		return nil
	}
	if old.Epoch > d.Authorization.Epoch {
		return coordinatorError("old execution epoch: " + id)
	}
	// Only the dispatch producer can archive and fence an earlier grant. Merely
	// observing a newer epoch value is insufficient; consume that exact original.
	raw, err := tx.Read(id, dispatchPath(d.Input.ID, "execution-"+strconv.FormatUint(old.Epoch, 10)))
	if err != nil {
		return err
	}
	var previous Dispatch
	if json.Unmarshal([]byte(raw), &previous) != nil || previous.Authorization != (ExecutionAuthorization{DispatchID: old.ID, Epoch: old.Epoch}) || !reflect.DeepEqual(previous.Input, d.Input) {
		return coordinatorError("missing fenced execution original: " + id)
	}
	return nil
}

func coordinatorDispatchTerminal(state DispatchState) bool {
	return state == DispatchCompleted || state == DispatchFailed || state == DispatchCancelled
}
