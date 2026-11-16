package board

import "errors"

// batchError ties a review evidence error to the batch it concerns, so a check
// can name the cards of that batch and not only the card it was checking.
type batchError struct {
	error
	BatchID string
}

func (e *batchError) Unwrap() error { return e.error }

// batchTaskIDs returns the cards of the batch err concerns, or nil when err
// concerns no batch or the batch cannot be read.
func batchTaskIDs(tx *Transaction, err error) []string {
	var concerned *batchError
	if tx == nil || !errors.As(err, &concerned) {
		return nil
	}
	var batch ReviewBatch
	if ok, e := readReviewJSON(tx, reviewBatchName(concerned.BatchID), &batch); e != nil || !ok {
		return nil
	}
	return batch.TaskIDs
}

func checkImportReviewGate(root string, ids []string) (problems []Problem) {
	for _, id := range ids {
		var related []string
		scope, err := reviewGateScope(root, id, false)
		if err == nil {
			scope.ReadOnly = true
			err = WithTransaction(root, scope, func(tx *Transaction) error {
				_, err := validateTaskReviewMode(tx, id, false, true)
				if err != nil {
					related = batchTaskIDs(tx, err)
				}
				return err
			})
		}
		if err != nil {
			problems = append(problems, Problem{Path: id, Message: err.Error(), RelatedPaths: related})
		}
	}
	return problems
}
