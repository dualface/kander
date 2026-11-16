package board

import (
	"errors"
	"slices"
)

// batchError ties a review evidence error to the batch it concerns, so a check
// can name the cards of that batch and not only the card it was checking.
type batchError struct {
	error
	BatchID string
}

func (e *batchError) Unwrap() error { return e.error }

// reviewProblem is the check problem of card id for err. An error that
// concerns a batch belongs to the cards of that batch alone, however many cards
// of the plan the check reached it from, and however a check worded it around
// the error (the run evidence check prefixes the run ID): the problem is the
// batch error itself. A batch whose cards cannot be read stays with the card
// checked.
func reviewProblem(tx *Transaction, id, message string, err error) Problem {
	problem := Problem{Path: id, Message: message}
	var concerned *batchError
	if tx == nil || !errors.As(err, &concerned) {
		return problem
	}
	var batch ReviewBatch
	if ok, e := readReviewJSON(tx, reviewBatchName(concerned.BatchID), &batch); e != nil || !ok || len(batch.TaskIDs) == 0 {
		return problem
	}
	members := slices.Sorted(slices.Values(batch.TaskIDs))
	problem.Path, problem.RelatedPaths, problem.Message, problem.Key = members[0], members[1:], concerned.Error(), concerned.Error()
	return problem
}

func checkImportReviewGate(root string, ids []string) (problems []Problem) {
	for _, id := range ids {
		var problem *Problem
		scope, err := reviewGateScope(root, id, false)
		if err == nil {
			scope.ReadOnly = true
			err = WithTransaction(root, scope, func(tx *Transaction) error {
				_, err := validateTaskReviewMode(tx, id, false, true)
				if err != nil {
					found := reviewProblem(tx, id, err.Error(), err)
					problem = &found
				}
				return err
			})
		}
		if err != nil {
			if problem == nil {
				problem = &Problem{Path: id, Message: err.Error()}
			}
			problems = append(problems, *problem)
		}
	}
	return problems
}
