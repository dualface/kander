package board

// IntegrationScope locks review originals, execution facts and complete group
// topology for a service-owned integration record. It grants no task write.
func IntegrationScope(ids []string, group string) LockScope {
	scope := reviewScope(ids, false)
	scope.Groups = append(scope.Groups, taskStartGroup)
	if group != "" {
		scope.Groups = append(scope.Groups, group)
	}
	scope.ExclusiveBoard = true
	return scope
}

func IntegrationGroupMembers(tx *Transaction, group string, ids []string) error {
	return coordinatorMembership(tx, group, ids)
}

// IntegrationReview requires the same completion gate used by done. The caller
// binds its code target separately and does not infer success from card prose.
func IntegrationReview(tx *Transaction, id string) (ReviewProgress, error) {
	progress, err := validateTaskReview(tx, id, true)
	if err != nil || progress.Status == "excluded" {
		return progress, err
	}
	range_, err := dispatchReviewRange(tx, id)
	if err != nil {
		return progress, err
	}
	progress.PlanTarget = range_.Descendant
	return progress, nil
}

// IntegrationReviewProgress exposes the closed plan target with the same
// complete review scope used by the ordinary progress and completion gates.
func IntegrationReviewProgress(root, id string) (progress ReviewProgress, err error) {
	scope, err := reviewGateScope(root, id, false)
	if err != nil {
		return progress, err
	}
	scope.ReadOnly = true
	err = WithTransaction(root, scope, func(tx *Transaction) error { var e error; progress, e = IntegrationReview(tx, id); return e })
	return progress, err
}
