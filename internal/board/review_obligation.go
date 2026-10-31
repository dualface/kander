package board

import (
	"fmt"
	"slices"
)

const (
	releaseDropped  = "dropped"
	releaseRecovery = "recovery"
)

// ReviewDropObligation is the review a drop of a required batch leaves on the plan.
// A later batch that requires the role, names the member and closes discharges it:
// a dropped requirement never reads as a review that is no longer needed.
type ReviewDropObligation struct {
	BatchID   string   `json:"batch_id"`
	FromBatch int      `json:"from_batch"`
	Roles     []string `json:"roles"`
	TaskIDs   []string `json:"task_ids"`
}

// ReviewObligationRelease names what one user decision releases. Each kind releases
// only itself, because a recovery voids review evidence and a drop does not.
type ReviewObligationRelease struct {
	Kind    string   `json:"kind"`
	TaskIDs []string `json:"task_ids"`
}

// keepDropObligations moves the obligations of p to the new end of the plan and adds
// those of the dropped batches. A user decision leaves none for this drop.
func keepDropObligations(tx *Transaction, p *ReviewPlan, dropped []ReviewPlanBatch, from int, decision string) error {
	kept := slices.Clone(p.DropObligations)
	for i := range kept {
		kept[i].FromBatch = min(kept[i].FromBatch, from)
	}
	p.DropObligations = kept
	if decision != "" {
		return nil
	}
	for _, b := range dropped {
		roles := planRequiredRoles(ReviewPlan{Batches: []ReviewPlanBatch{b}})
		members := make([]string, 0, len(b.TaskIDs))
		for _, id := range b.TaskIDs {
			excluded, err := memberExcluded(tx, id)
			if err != nil {
				return err
			}
			if excluded {
				continue
			}
			s, err := tx.Snapshot(id)
			if err != nil {
				return err
			}
			if s.Entry.State == "working" || s.Entry.State == "review" {
				members = append(members, id)
			}
		}
		if len(roles) > 0 && len(members) > 0 {
			p.DropObligations = append(p.DropObligations, ReviewDropObligation{BatchID: b.BatchID, FromBatch: from, Roles: roles, TaskIDs: members})
		}
	}
	return nil
}

// dropObligationsOwed lists the undischarged obligations as `<member>/<role>`. With
// mustClose a batch discharges one only after it closed; the seal also accepts a
// batch that is only planned, because done still waits for it.
func dropObligationsOwed(tx *Transaction, p ReviewPlan, mustClose bool) ([]string, error) {
	var owed []string
	for _, o := range p.DropObligations {
		for _, id := range o.TaskIDs {
			excluded, err := memberExcluded(tx, id)
			if err != nil {
				return nil, err
			}
			if excluded {
				continue
			}
			for _, role := range o.Roles {
				covered, err := batchCoversRole(tx, p, o.FromBatch, id, role, mustClose)
				if err != nil {
					return nil, err
				}
				if item := id + "/" + role; !covered && !slices.Contains(owed, item) {
					owed = append(owed, item)
				}
			}
		}
	}
	return owed, nil
}

func batchCoversRole(tx *Transaction, p ReviewPlan, from int, id, role string, mustClose bool) (bool, error) {
	for i, b := range p.Batches {
		if i < from || !containsID(b.TaskIDs, id) || !slices.Contains(planRequiredRoles(ReviewPlan{Batches: []ReviewPlanBatch{b}}), role) {
			continue
		}
		if !mustClose {
			return true, nil
		}
		var c ReviewClosure
		closed, err := readReviewJSON(tx, closureName(b.BatchID), &c)
		if closed || err != nil {
			return closed, err
		}
	}
	return false, nil
}

// releaseObligation applies a user decision. It refuses a member with nothing to
// release, so a decision never stands in the history with no effect.
func releaseObligation(tx *Transaction, p *ReviewPlan, r ReviewObligationRelease, decision string) error {
	if decision == "" {
		return reviewError(t("board.review_release_decision"))
	}
	ids, err := normalizedReviewTasks(r.TaskIDs)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if !containsID(p.TaskIDs, id) {
			return reviewError("foreign plan member")
		}
		switch r.Kind {
		case releaseDropped:
			if !releaseDropObligation(p, id) {
				return reviewError(t("board.review_release_nothing", r.Kind, id))
			}
		case releaseRecovery:
			if err = releaseRecoveryReview(tx, *p, id, decision); err != nil {
				return err
			}
		default:
			return reviewError(fmt.Sprintf("release_obligation kind: %q", r.Kind))
		}
	}
	return nil
}

func releaseDropObligation(p *ReviewPlan, id string) bool {
	released := false
	kept := make([]ReviewDropObligation, 0, len(p.DropObligations))
	for _, o := range p.DropObligations {
		members := slices.DeleteFunc(slices.Clone(o.TaskIDs), func(member string) bool { return member == id })
		released = released || len(members) < len(o.TaskIDs)
		if o.TaskIDs = members; len(members) > 0 {
			kept = append(kept, o)
		}
	}
	p.DropObligations = kept
	return released
}

// releaseRecoveryReview accepts only the record of a member that had no plan when it
// was reset, under a plan that requires nothing. A plan that requires a role takes
// the review: after a recovery N/A never stands in for one, and neither does this.
func releaseRecoveryReview(tx *Transaction, p ReviewPlan, id, decision string) error {
	var o RecoveryReview
	ok, err := readReviewJSON(tx, recoveryReviewName(id), &o)
	if err != nil {
		return err
	}
	if !ok || o.PlanID != "" || o.ReleaseDecision != "" {
		return reviewError(t("board.review_release_nothing", releaseRecovery, id))
	}
	if roles := recoveryRoles(p, o); len(roles) > 0 {
		return reviewError(t("board.review_release_recovery_roles", id, fmt.Sprint(roles)))
	}
	o.ReleaseDecision = decision
	return tx.PutGroup(reviewControlGroup, recoveryReviewName(id), reviewJSON(o))
}
