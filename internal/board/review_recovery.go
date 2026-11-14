package board

import (
	"maps"
	"slices"

	"github.com/dualface/kander/internal/statemachine/reviewobligation"
)

// RecoveryReview is what a reset leaves on the member's review: the roles its plan
// required, also those of its dropped batches, which only a closed batch from
// FromBatch on can discharge. N/A never counts, and a later reclaim changes nothing,
// because the record binds the member, not an execution cycle.
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

// stageRecoveryReview runs a reset through the member's kernel and writes the
// record it leaves: one with no role and no plan for a member with no plan, and
// one for the whole plan as it stands for a member with a plan, with the roles of
// the old record and the roles the member's dropped batches required. Neither a
// drop decision nor a release ends a recovery, in either order. When the plan
// requires no role, the old record stays as it is.
func stageRecoveryReview(tx *Transaction, id, recovery string) error {
	p, exists, err := readTaskPlan(tx, id)
	if err != nil {
		return err
	}
	k, err := loadMemberKernel(tx, p, exists, id, kernelBound(p))
	if err != nil {
		return err
	}
	keeps, err := queryKernel(k, reviewobligation.ResetKeepsRecord)
	if err != nil {
		return err
	}
	if !k.step(reviewobligation.Reset{}) {
		return reviewError("recovery of a member that left its review: " + id)
	}
	switch {
	case keeps:
		return nil
	case k.state.Rec == reviewobligation.RecoveryNoPlan:
		return tx.PutGroup(reviewControlGroup, recoveryReviewName(id), reviewJSON(RecoveryReview{Schema: 1, TaskID: id, RecoveryID: recovery, Roles: []string{}}))
	default:
		return tx.PutGroup(reviewControlGroup, recoveryReviewName(id), reviewJSON(RecoveryReview{Schema: 1, TaskID: id, PlanID: p.PlanID, RecoveryID: recovery, FromBatch: int(k.state.RecFloor), Roles: roleNames(k.state.RecRoles)}))
	}
}

// recoveryReviewOwed reports a member whose recovery still needs an actual review,
// and the roles that review misses (`recoveryOwed` and `recoveryMissing`).
func recoveryReviewOwed(tx *Transaction, p ReviewPlan, id string) (missing []string, owed bool, err error) {
	k, err := loadMemberKernel(tx, p, true, id, kernelBound(p))
	if err != nil {
		return nil, false, err
	}
	roles, err := queryKernel(k, reviewobligation.RecoveryMissing)
	if err != nil {
		return nil, false, err
	}
	if owed, err = queryKernel(k, reviewobligation.RecoveryOwed); err != nil {
		return nil, false, err
	}
	if roles != 0 {
		missing = roleNames(roles)
	}
	return missing, owed, nil
}

// closedInCycle reports a closure whose conclusion run of role reviewed cycle. A
// recovery changed the cycle, so a run with no binding never counts here.
func closedInCycle(c ReviewClosure, role, id, cycle string) bool {
	run := c.Request.Roles[role].RunID
	at := slices.IndexFunc(c.View.Runs, func(rv ReviewRunView) bool { return rv.Run.RunID == run })
	return at >= 0 && c.View.Runs[at].Run.MemberCycles[id] == cycle
}
