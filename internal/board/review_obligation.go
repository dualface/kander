package board

import (
	"fmt"
	"slices"

	"github.com/dualface/kander/internal/statemachine/reviewobligation"
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

// dropObligationsOwed lists the undischarged obligations as `<member>/<role>`, in
// the order of the persisted obligations. With mustClose a batch discharges one
// only after it closed; the seal also accepts a batch that is only planned, because
// done still waits for it. The kernel decides each obligation on the member's state
// with that obligation alone.
func dropObligationsOwed(tx *Transaction, p ReviewPlan, mustClose bool) ([]string, error) {
	kernels := map[string]*memberKernel{}
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
			k := kernels[id]
			if k == nil {
				if k, err = loadMemberKernel(tx, p, true, id, kernelBound(p)); err != nil {
					return nil, err
				}
				kernels[id] = k
			}
			roles, err := k.obligationOwed(o, mustClose)
			if err != nil {
				return nil, err
			}
			for _, role := range o.Roles {
				r, err := kernelRole(role)
				if err != nil {
					return nil, err
				}
				if item := id + "/" + role; roles.Has(r) && !slices.Contains(owed, item) {
					owed = append(owed, item)
				}
			}
		}
	}
	return owed, nil
}

// obligationOwed is the kernel's answer for one persisted obligation: the roles
// of o no batch discharges, on the member's state with o as its only obligation.
func (k *memberKernel) obligationOwed(o ReviewDropObligation, mustClose bool) (reviewobligation.RoleSet, error) {
	alone := *k
	roles, err := kernelRoleSet(o.Roles)
	if err != nil {
		return 0, err
	}
	alone.state.DropRec = roles
	for r := range alone.state.DropFloor {
		alone.state.DropFloor[r] = uint64(o.FromBatch)
	}
	if mustClose {
		return queryKernel(&alone, reviewobligation.DropOwedClosed)
	}
	return queryKernel(&alone, reviewobligation.DropOwedPlanned)
}

// releaseFacts is what one release_obligation request establishes: the member
// kernels after the release, and the first refusal in the order the request is
// checked.
type releaseFacts struct {
	decision, members, accepted bool
	kernels                     []*memberKernel
	err                         error
}

// releaseObligation runs a user decision through the kernels of the named members.
// A member with nothing to release refuses it, so a decision never stands in the
// history with no effect.
func releaseObligation(tx *Transaction, p ReviewPlan, r ReviewObligationRelease, decision string) (f releaseFacts, err error) {
	f.decision, f.members, f.accepted = decision != "", true, true
	if !f.decision {
		f.err = reviewError(t("board.review_release_decision"))
	}
	ids, err := normalizedReviewTasks(r.TaskIDs)
	if err != nil {
		f.accepted, f.err = false, firstError(f.err, err)
		return f, nil
	}
	for _, id := range ids {
		if !containsID(p.TaskIDs, id) {
			f.members, f.err = false, firstError(f.err, reviewError("foreign plan member"))
			continue
		}
		k, err := loadMemberKernel(tx, p, true, id, kernelBound(p))
		if err != nil {
			return f, err
		}
		var refusal error
		switch r.Kind {
		case releaseDropped:
			if !k.step(reviewobligation.ReleaseDrop{}) {
				refusal = reviewError(t("board.review_release_nothing", r.Kind, id))
			}
		case releaseRecovery:
			roles, ok := reviewobligation.RecoveryRoles(k.state, k.params)
			releasable := reviewobligation.RecoveryReleasable(k.state, k.params)
			switch {
			case k.step(reviewobligation.ReleaseRecovery{}):
			case !ok:
				refusal = errKernelState(id)
			case releasable:
				refusal = reviewError(t("board.review_release_recovery_roles", id, fmt.Sprint(roleNames(roles))))
			default:
				refusal = reviewError(t("board.review_release_nothing", releaseRecovery, id))
			}
		default:
			refusal = reviewError(fmt.Sprintf("release_obligation kind: %q", r.Kind))
		}
		if refusal != nil {
			f.accepted, f.err = false, firstError(f.err, refusal)
			continue
		}
		f.kernels = append(f.kernels, k)
	}
	return f, nil
}

// persistRelease writes what the accepted release changed: the member leaves
// every drop obligation, or its recovery record takes the decision.
func persistRelease(tx *Transaction, p *ReviewPlan, kind, decision string, kernels []*memberKernel) error {
	for _, k := range kernels {
		if kind == releaseDropped {
			kept := make([]ReviewDropObligation, 0, len(p.DropObligations))
			for _, o := range p.DropObligations {
				if o.TaskIDs = slices.DeleteFunc(slices.Clone(o.TaskIDs), func(member string) bool { return member == k.id }); len(o.TaskIDs) > 0 {
					kept = append(kept, o)
				}
			}
			p.DropObligations = kept
			continue
		}
		var o RecoveryReview
		if _, err := readReviewJSON(tx, recoveryReviewName(k.id), &o); err != nil {
			return err
		}
		o.ReleaseDecision = decision
		if err := tx.PutGroup(reviewControlGroup, recoveryReviewName(k.id), reviewJSON(o)); err != nil {
			return err
		}
	}
	return nil
}
