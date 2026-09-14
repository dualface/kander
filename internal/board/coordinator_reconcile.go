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
	if err := verifyCoordinatorHandoffs(tx, s, old); err != nil {
		return m, err
	}
	cycle, awaiting, attempt, err := coordinatorStartCycle(ctx, tx, id, &old, s)
	if err != nil {
		return m, err
	}
	m = CoordinatorMember{Revision: s.Revision, State: s.Entry.State, Cycle: cycle, AwaitingStart: awaiting, StartAttempt: attempt, DeliveryCommit: old.DeliveryCommit}

	m.ReleasedDispatches = append([]ArtifactReference(nil), old.ReleasedDispatches...)
	m.Handoffs = append([]lifecycleHandoff(nil), old.Handoffs...)
	if cycle != old.Cycle && !awaiting && !old.AwaitingStart && old.Cycle != ReviewDigest([]byte(id+"\n")) {
		handoffs, e := lifecycleHandoffs(tx, s, old.Cycle)
		if e != nil {
			return m, e
		}
		m.Handoffs = append(m.Handoffs, handoffs...)
		// A previous cycle's delivery is history, never this executor's delivery.
		m.DeliveryCommit = ""
	}
	if awaiting && attempt != "" && o.DeliveryCommit != "" {
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
	a := authFrom(s.Text)
	if a == (ExecutionAuthorization{}) {
		if o.DispatchID != "" || o.Epoch != 0 || o.Base != "" {
			return m, coordinatorError("unbound member cannot acknowledge a dispatch: " + id)
		}

		if old.Dispatch != nil {
			if err := coordinatorReleasedDispatch(tx, s, *old.Dispatch); err != nil {
				return m, err
			}
			m.ReleasedDispatches = append(m.ReleasedDispatches, ArtifactReference{id, dispatchPath(old.Dispatch.ID, "release")})
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
	if old.Dispatch != nil {
		if err = coordinatorDispatchAdvance(tx, id, *old.Dispatch, d); err != nil {
			return m, err
		}
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
	m.Dispatch = &CoordinatorDispatch{ID: d.Input.ID, Epoch: a.Epoch, Base: d.Input.Base, Kind: d.Input.Kind, Revision: d.Revision, State: d.State, Intent: ArtifactReference{id, dispatchPath(d.Input.ID, "intent")}}
	terminal := d.State == DispatchCompleted || d.State == DispatchCancelled || d.State == DispatchFailed
	m.Dispatch.PendingConfirmation = !terminal && d.Accepted == nil
	m.Dispatch.PendingDelivery = !terminal && d.Input.Kind != "wrap-up"
	m.Dispatch.PendingWrapUp = !terminal && d.Input.Kind == "wrap-up"
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
		ref := w.Artifact
		m.Dispatch.Integration = &ref
		if d.WrapUpAuthority != nil {
			ref := ArtifactReference{id, dispatchPath(d.Input.ID, "wrap-up-authority-"+strconv.FormatUint(a.Epoch, 10))}
			m.Dispatch.WrapUpAuthority = &ref
		}
		if d.State == DispatchCompleted && (s.Entry.State != "done" && s.Entry.State != "archived" || MetadataFrom(s.Text, "RESULT") != "completed") {
			return m, coordinatorError("wrap-up completion state: " + id)
		}
	}
	return m, ctx.Err()
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

func coordinatorReleasedDispatch(tx *Transaction, s Snapshot, old CoordinatorDispatch) error {
	releases, err := cardReleases(s)
	if err != nil {
		return err
	}
	for _, r := range releases {
		if r.Termination.Authorization.DispatchID != old.ID {
			continue
		}
		if err := verifyCardRelease(tx, s, r); err != nil {
			return err
		}
		d, err := readDispatch(tx, s.Entry.TaskID, old.ID)
		if err != nil {
			return err
		}
		if d.Input.Base != old.Base || d.Input.Kind != old.Kind {
			return coordinatorError("released dispatch facts changed")
		}
		return coordinatorDispatchAdvance(tx, s.Entry.TaskID, old, d)
	}
	return coordinatorError("unbound member lacks dispatch termination and release originals")
}
