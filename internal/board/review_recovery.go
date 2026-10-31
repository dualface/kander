package board

import (
	"maps"
	"slices"
)

// RecoveryReview is what a reset leaves on the member's review: the roles its plan
// required, which only a closed batch from FromBatch on can discharge. N/A never
// counts, and a later reclaim changes nothing, because the record binds the
// member, not an execution cycle.
//
// A reset of a member with no plan writes the record with no PlanID. The roles then
// come from the plan created later, and Roles keeps those a drop took out of it. Only
// a conclusion run of the current cycle discharges them. When no role is left, the
// plan is all N/A, and only ReleaseDecision discharges the record.
type RecoveryReview struct {
	Schema          int      `json:"schema"`
	TaskID          string   `json:"task_id"`
	PlanID          string   `json:"plan_id"`
	RecoveryID      string   `json:"recovery_id"`
	FromBatch       int      `json:"from_batch"`
	Roles           []string `json:"roles"`
	ReleaseDecision string   `json:"release_decision,omitempty"`
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
	if err != nil {
		return err
	}
	if !exists {
		return tx.PutGroup(reviewControlGroup, recoveryReviewName(id), reviewJSON(RecoveryReview{Schema: 1, TaskID: id, RecoveryID: recovery, Roles: []string{}}))
	}
	roles := planRequiredRoles(p)
	if len(roles) == 0 {
		return nil
	}
	return tx.PutGroup(reviewControlGroup, recoveryReviewName(id), reviewJSON(RecoveryReview{Schema: 1, TaskID: id, PlanID: p.PlanID, RecoveryID: recovery, FromBatch: len(p.Batches), Roles: roles}))
}

// moveRecoveryReviews follows a drop of trailing batches. The next batch lands at
// the new end, so a recovery owed past it must accept it. Every batch left stays
// below that position, so none of them counts. A record with no PlanID reads its
// roles from the plan, so it keeps the roles the drop took out.
func moveRecoveryReviews(tx *Transaction, p ReviewPlan, droppedRoles []string) error {
	for _, id := range p.TaskIDs {
		var o RecoveryReview
		ok, err := readReviewJSON(tx, recoveryReviewName(id), &o)
		if err != nil {
			return err
		}
		switch {
		case !ok:
			continue
		case o.PlanID == "":
			o.Roles = slices.Compact(slices.Sorted(slices.Values(slices.Concat(o.Roles, droppedRoles))))
		case o.PlanID != p.PlanID || o.FromBatch <= len(p.Batches):
			continue
		default:
			o.FromBatch = len(p.Batches)
		}
		if err = tx.PutGroup(reviewControlGroup, recoveryReviewName(id), reviewJSON(o)); err != nil {
			return err
		}
	}
	return nil
}

// recoveryRoles lists the roles a record owes under p.
func recoveryRoles(p ReviewPlan, o RecoveryReview) []string {
	if o.PlanID != "" {
		return o.Roles
	}
	return slices.Compact(slices.Sorted(slices.Values(slices.Concat(o.Roles, planRequiredRoles(p)))))
}

// recoveryReviewOwed reports a member whose recovery still needs an actual review.
func recoveryReviewOwed(tx *Transaction, p ReviewPlan, id string) (bool, error) {
	var o RecoveryReview
	ok, err := readReviewJSON(tx, recoveryReviewName(id), &o)
	if err != nil || !ok {
		return false, err
	}
	if o.Schema != 1 || o.TaskID != id || o.PlanID != "" && len(o.Roles) == 0 {
		return false, reviewError("invalid recovery review: " + id)
	}
	roles := recoveryRoles(p, o)
	if len(roles) == 0 {
		return o.ReleaseDecision == "", nil
	}
	from := o.FromBatch
	if o.PlanID != p.PlanID {
		from = 0
	}
	cycle := ""
	if o.PlanID == "" {
		s, err := tx.Snapshot(id)
		if err != nil {
			return false, err
		}
		cycle = planCycle(s)
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
			if requirement == "required" && (cycle == "" || closedInCycle(c, role, id, cycle)) {
				reviewed[PinRole(role)] = true
			}
		}
	}
	return slices.ContainsFunc(roles, func(role string) bool { return !reviewed[role] }), nil
}

// closedInCycle reports a closure whose conclusion run of role reviewed cycle. A
// recovery changed the cycle, so a run with no binding never counts here.
func closedInCycle(c ReviewClosure, role, id, cycle string) bool {
	run := c.Request.Roles[role].RunID
	at := slices.IndexFunc(c.View.Runs, func(rv ReviewRunView) bool { return rv.Run.RunID == run })
	return at >= 0 && c.View.Runs[at].Run.MemberCycles[id] == cycle
}
