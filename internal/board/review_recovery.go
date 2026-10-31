package board

import (
	"maps"
	"slices"
)

// RecoveryReview is what a reset leaves on the member's review: the roles its plan
// required, which only a closed batch from FromBatch on can discharge. N/A never
// counts, and a later reclaim changes nothing, because the record binds the
// member, not an execution cycle.
type RecoveryReview struct {
	Schema     int      `json:"schema"`
	TaskID     string   `json:"task_id"`
	PlanID     string   `json:"plan_id"`
	RecoveryID string   `json:"recovery_id"`
	FromBatch  int      `json:"from_batch"`
	Roles      []string `json:"roles"`
}

func recoveryReviewName(id string) string { return "recovery-reviews/" + id + ".json" }

// planRequiredRoles is the union of the roles the batches of a plan required.
func planRequiredRoles(p ReviewPlan) []string {
	roles := map[string]bool{}
	for _, b := range p.Batches {
		for role, requirement := range b.Requirements {
			if requirement == "required" {
				roles[PinRole(role)] = true
			}
		}
	}
	return slices.Sorted(maps.Keys(roles))
}

// stageRecoveryReview replaces the record of a member with one for the whole plan
// as it stands. A later recovery covers an earlier one: the roles only grow and the
// position only moves on.
func stageRecoveryReview(tx *Transaction, id, recovery string) error {
	p, exists, err := readTaskPlan(tx, id)
	if err != nil || !exists {
		return err
	}
	roles := planRequiredRoles(p)
	if len(roles) == 0 {
		return nil
	}
	return tx.PutGroup(reviewControlGroup, recoveryReviewName(id), reviewJSON(RecoveryReview{Schema: 1, TaskID: id, PlanID: p.PlanID, RecoveryID: recovery, FromBatch: len(p.Batches), Roles: roles}))
}

// moveRecoveryReviews follows a drop of trailing batches. The next batch lands at
// the new end, so a recovery owed past it must accept it. Every batch left stays
// below that position, so none of them counts.
func moveRecoveryReviews(tx *Transaction, p ReviewPlan) error {
	for _, id := range p.TaskIDs {
		var o RecoveryReview
		ok, err := readReviewJSON(tx, recoveryReviewName(id), &o)
		if err != nil {
			return err
		}
		if !ok || o.PlanID != p.PlanID || o.FromBatch <= len(p.Batches) {
			continue
		}
		o.FromBatch = len(p.Batches)
		if err = tx.PutGroup(reviewControlGroup, recoveryReviewName(id), reviewJSON(o)); err != nil {
			return err
		}
	}
	return nil
}

// recoveryReviewOwed reports a member whose recovery still needs an actual review.
func recoveryReviewOwed(tx *Transaction, p ReviewPlan, id string) (bool, error) {
	var o RecoveryReview
	ok, err := readReviewJSON(tx, recoveryReviewName(id), &o)
	if err != nil || !ok {
		return false, err
	}
	if o.Schema != 1 || o.TaskID != id || len(o.Roles) == 0 {
		return false, reviewError("invalid recovery review: " + id)
	}
	from := o.FromBatch
	if o.PlanID != p.PlanID {
		from = 0
	}
	reviewed := map[string]bool{}
	for i, b := range p.Batches {
		if i < from || !containsID(b.TaskIDs, id) {
			continue
		}
		var c ReviewClosure
		closed, err := readReviewJSON(tx, closureName(b.BatchID), &c)
		if err != nil {
			return false, err
		}
		if !closed {
			continue
		}
		for role, requirement := range b.Requirements {
			if requirement == "required" {
				reviewed[PinRole(role)] = true
			}
		}
	}
	return slices.ContainsFunc(o.Roles, func(role string) bool { return !reviewed[role] }), nil
}
