package board

import "strings"

type ReviewBatchAdvance struct {
	BatchID          string        `json:"batch_id"`
	ExpectedRevision uint64        `json:"expected_revision"`
	Advance          ReviewAdvance `json:"advance"`
}

// AdvanceReviewBatch receives a verified in-batch delivery without starting a
// reviewer. Mechanical-only fixes need this same CAS to close at their new HEAD.
func AdvanceReviewBatch(root string, x ReviewBatchAdvance) error {
	if !ValidReviewID(x.BatchID) {
		return reviewError("batch_id")
	}
	return WithTransaction(root, reviewScope(nil, false), func(tx *Transaction) error {
		var b ReviewBatch
		ok, err := readReviewJSON(tx, reviewBatchName(x.BatchID), &b)
		if err != nil {
			return err
		}
		if !ok || b.Revision != x.ExpectedRevision {
			return reviewError("batch revision CAS conflict")
		}
		var closed ReviewClosure
		exists, err := readReviewJSON(tx, closureName(x.BatchID), &closed)
		if err != nil {
			return err
		}
		if exists {
			return reviewError("batch already closed")
		}
		a := x.Advance
		if a.PreviousTarget != b.TargetCommit || a.Target == a.PreviousTarget || !validCommit(a.Target) || strings.TrimSpace(a.Reason) == "" || len(a.Deliveries) == 0 {
			return reviewError("batch target CAS conflict")
		}
		for commit, id := range a.Deliveries {
			if !validCommit(commit) || !containsID(b.TaskIDs, id) {
				return reviewError("foreign delivery")
			}
		}
		if err = settledReviewBatch(tx, b.BatchID); err != nil {
			return err
		}
		b.Advances = append(b.Advances, a)
		b.TargetCommit = a.Target
		b.Revision++
		return tx.PutGroup(reviewControlGroup, reviewBatchName(b.BatchID), reviewJSON(b))
	})
}
