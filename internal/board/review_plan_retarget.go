package board

import (
	"slices"
	"strings"
)

// ReviewBatchRetarget corrects the base and target of a planned batch that never
// ran. The previous values are the compare-and-swap of the request: they must
// equal what the batch record and the plan hold now.
type ReviewBatchRetarget struct {
	PreviousBase   string `json:"previous_base"`
	PreviousTarget string `json:"previous_target"`
	Base           string `json:"base"`
	Target         string `json:"target"`
}

// retargetPlanBatches applies the retarget_batches operation of extend-plan. A
// batch with no run, advance, or closure has no evidence that names its base or
// target, so only the plan, the batch record, and a published aggregate view
// carry them. The batch record gets a new revision, which voids the view hash of
// an earlier aggregate: the author aggregates again before closing.
func retargetPlanBatches(tx *Transaction, p *ReviewPlan, retargets map[string]ReviewBatchRetarget) error {
	// The plan the caller keeps as the history snapshot must not see the new values.
	p.Batches = slices.Clone(p.Batches)
	for _, id := range sortedKeys(retargets) {
		r := retargets[id]
		at := slices.IndexFunc(p.Batches, func(pb ReviewPlanBatch) bool { return pb.BatchID == id })
		if at < 0 {
			return reviewError(t("board.review_retarget_unplanned", id))
		}
		var b ReviewBatch
		ok, err := readReviewJSON(tx, reviewBatchName(id), &b)
		if err != nil {
			return err
		}
		if !ok || b.PlanID != p.PlanID {
			return reviewError("planned batch missing")
		}
		for _, sha := range []string{r.PreviousBase, r.PreviousTarget, r.Base, r.Target} {
			if !validCommit(sha) {
				return reviewError(t("board.review_retarget_sha", id))
			}
		}
		planned := p.Batches[at]
		if r.PreviousBase != b.Base || r.PreviousTarget != b.TargetCommit || planned.Base != b.Base || planned.TargetCommit != b.TargetCommit {
			return reviewError(t("board.review_retarget_cas", id))
		}
		if r.Base == b.Base && r.Target == b.TargetCommit {
			return reviewError(t("board.review_retarget_same", id))
		}
		runs, err := batchRuns(tx, b)
		if err != nil {
			return err
		}
		var closure ReviewClosure
		closed, err := readReviewJSON(tx, closureName(id), &closure)
		if err != nil {
			return err
		}
		if closed || len(runs) > 0 || len(b.Advances) > 0 {
			return reviewError(t("board.review_retarget_ran", id))
		}
		if at > 0 && r.Base != p.Batches[at-1].TargetCommit {
			return reviewError(t("board.review_plan_chain_gap", id, r.Base, p.Batches[at-1].BatchID, p.Batches[at-1].TargetCommit))
		}
		if at < len(p.Batches)-1 && r.Target != b.TargetCommit {
			successors := make([]string, 0, len(p.Batches)-at-1)
			for _, next := range p.Batches[at+1:] {
				successors = append(successors, next.BatchID)
			}
			return reviewError(t("board.review_retarget_successor", id, strings.Join(successors, ", ")))
		}
		p.Batches[at].Base, p.Batches[at].TargetCommit = r.Base, r.Target
		b.Base, b.TargetCommit = r.Base, r.Target
		b.Revision++
		if err = tx.PutGroup(reviewControlGroup, reviewBatchName(id), reviewJSON(b)); err != nil {
			return err
		}
		if err = republishReviewDisposition(tx, b); err != nil {
			return err
		}
	}
	return nil
}

// republishReviewDisposition rewrites the aggregate view of a batch that already
// has one, so no copy keeps the base or target the batch had before. A batch
// nobody aggregated has no view to rewrite.
func republishReviewDisposition(tx *Transaction, b ReviewBatch) error {
	_, published, err := tx.ReadGroup(reviewControlGroup, "dispositions/"+b.BatchID+".json")
	if err != nil || !published {
		return err
	}
	v, err := aggregateReviewBatch(tx, b)
	if err != nil {
		return err
	}
	targets, err := reviewCopyTargets(tx, b.TaskIDs)
	if err != nil {
		return err
	}
	for _, task := range targets {
		if err = tx.Put(task, "reviews/batches/"+b.BatchID+"/disposition.json", reviewJSON(v)); err != nil {
			return err
		}
	}
	return tx.PutGroup(reviewControlGroup, "dispositions/"+b.BatchID+".json", reviewJSON(v))
}
