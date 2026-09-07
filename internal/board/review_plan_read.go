package board

// ReadReviewPlan exposes the authoritative identity for worktree binding before
// Git verification. Mutations still re-read and CAS it inside their transaction.
func ReadReviewPlan(root, id string) (p ReviewPlan, err error) {
	if !ValidReviewID(id) {
		return p, reviewError("plan_id")
	}
	err = WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
		ok, e := readReviewJSON(tx, planName(id), &p)
		if e != nil {
			return e
		}
		if !ok || p.PlanID != id {
			return reviewError("missing plan")
		}
		return nil
	})
	return
}
