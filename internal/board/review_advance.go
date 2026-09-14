package board

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

type ReviewBatchAdvance struct {
	BatchID          string        `json:"batch_id"`
	ExpectedRevision uint64        `json:"expected_revision"`
	Advance          ReviewAdvance `json:"advance"`
}

// reviewPlanTargetSyncRequest records why a plan batch target was rewritten.
// Advance paths and extend-plan SyncTargets share this history shape.
type reviewPlanTargetSyncRequest struct {
	Kind            string `json:"kind"`
	BatchID         string `json:"batch_id"`
	PreviousTarget  string `json:"previous_target"`
	Target          string `json:"target"`
	Author          string `json:"author,omitempty"`
	Basis           string `json:"basis,omitempty"`
	ExpectedPlanRev uint64 `json:"expected_revision,omitempty"`
}

// AdvanceReviewBatch receives a verified in-batch delivery without starting a
// reviewer. Mechanical-only fixes need this same CAS to close at their new HEAD.
// Planned batches also rewrite the plan's recorded target in the same transaction.
func AdvanceReviewBatch(root string, x ReviewBatchAdvance) error {
	if !ValidReviewID(x.BatchID) {
		return reviewError("batch_id")
	}
	var scopeIDs []string
	err := WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
		var b ReviewBatch
		ok, e := readReviewJSON(tx, reviewBatchName(x.BatchID), &b)
		if e != nil {
			return e
		}
		if !ok {
			return reviewError("missing batch")
		}
		if b.PlanID == "" {
			return nil
		}
		var p ReviewPlan
		found, e := readReviewJSON(tx, planName(b.PlanID), &p)
		if e != nil {
			return e
		}
		if !found || p.PlanID != b.PlanID {
			return reviewError("batch review plan required")
		}
		scopeIDs = p.TaskIDs
		return nil
	})
	if err != nil {
		return err
	}
	return WithTransaction(root, reviewScope(scopeIDs, false), func(tx *Transaction) error {
		var b ReviewBatch
		ok, err := readReviewJSON(tx, reviewBatchName(x.BatchID), &b)
		if err != nil {
			return err
		}
		if !ok || b.Revision != x.ExpectedRevision {
			return reviewError("batch revision CAS conflict")
		}
		var closed ReviewClosure
		exists, err := readReviewJSON(tx, closureName(x.BatchID), &closed)
		if err != nil {
			return err
		}
		if exists {
			return reviewError("batch already closed")
		}
		a := x.Advance
		if a.PreviousTarget != b.TargetCommit || a.Target == a.PreviousTarget || !validCommit(a.Target) || strings.TrimSpace(a.Reason) == "" || len(a.Deliveries) == 0 {
			return reviewError("batch target CAS conflict")
		}
		for commit, id := range a.Deliveries {
			if !validCommit(commit) || !containsID(b.TaskIDs, id) {
				return reviewError("foreign delivery")
			}
		}
		if err = settledReviewBatch(tx, b.BatchID); err != nil {
			return err
		}
		b.Advances = append(b.Advances, a)
		b.TargetCommit = a.Target
		b.Revision++
		if err = tx.PutGroup(reviewControlGroup, reviewBatchName(b.BatchID), reviewJSON(b)); err != nil {
			return err
		}
		return syncPlannedBatchTarget(tx, b, reviewPlanTargetSyncRequest{
			Kind:           "advance",
			PreviousTarget: a.PreviousTarget,
		})
	})
}

// syncPlannedBatchTarget rewrites one plan batch's recorded target to match the
// runtime batch. Unplanned batches are left unchanged. History keeps the prior
// plan so an old recorded target remains auditable. Callers must pass the
// pre-advance batch target as PreviousTarget so drifted legacy plans are refused
// instead of silently repaired.
func syncPlannedBatchTarget(tx *Transaction, batch ReviewBatch, request reviewPlanTargetSyncRequest) error {
	if batch.PlanID == "" {
		return nil
	}
	var p ReviewPlan
	ok, err := readReviewJSON(tx, planName(batch.PlanID), &p)
	if err != nil {
		return err
	}
	if !ok || p.PlanID != batch.PlanID {
		return reviewError("batch review plan required")
	}
	index := -1
	for i, pb := range p.Batches {
		if pb.BatchID == batch.BatchID {
			index = i
			break
		}
	}
	if index < 0 {
		return reviewError("unplanned batch")
	}
	previousTarget := p.Batches[index].TargetCommit
	if previousTarget == batch.TargetCommit {
		return nil
	}
	if request.PreviousTarget != previousTarget {
		return planBatchTargetMismatch(previousTarget, request.PreviousTarget, batch.BatchID)
	}
	request.Target = batch.TargetCommit
	request.BatchID = batch.BatchID
	previous := p
	previous.Batches = slices.Clone(p.Batches)
	p.Batches[index].TargetCommit = batch.TargetCommit
	p.Revision++
	for _, id := range p.TaskIDs {
		if err = tx.Put(id, "reviews/plan.json", reviewJSON(p)); err != nil {
			return err
		}
	}
	if err = tx.PutGroup(reviewControlGroup, fmt.Sprintf("plan-history/%s/%d.json", p.PlanID, p.Revision), reviewJSON(struct {
		Previous   ReviewPlan                  `json:"previous"`
		Request    reviewPlanTargetSyncRequest `json:"request"`
		RecordedAt string                      `json:"recorded_at"`
	}{previous, request, time.Now().UTC().Format(time.RFC3339Nano)})); err != nil {
		return err
	}
	return tx.PutGroup(reviewControlGroup, planName(p.PlanID), reviewJSON(p))
}

func planBatchTargetMismatch(planTarget, batchTarget, batchID string) error {
	return reviewError(fmt.Sprintf("plan target %s != batch target %s for %s", planTarget, batchTarget, batchID))
}
