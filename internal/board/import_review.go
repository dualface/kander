package board

func checkImportReviewGate(root string, ids []string) (problems []Problem) {
	for _, id := range ids {
		scope, err := reviewGateScope(root, id, false)
		if err == nil {
			scope.ReadOnly = true
			err = WithTransaction(root, scope, func(tx *Transaction) error { _, err := validateTaskReviewMode(tx, id, false, true); return err })
		}
		if err != nil {
			problems = append(problems, Problem{Path: id, Message: err.Error()})
		}
	}
	return problems
}
