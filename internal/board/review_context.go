package board

import "fmt"

// ReadReviewInput reads the frozen input even after a crash before finalization.
func ReadReviewInput(root, id, name string) (data []byte, err error) {
	if !ValidReviewID(id) || name != "task-context.md" && name != "review-context.md" {
		return nil, reviewError("review input name")
	}
	err = WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
		var run ReviewRun
		ok, e := readReviewJSON(tx, reviewRunName(id), &run)
		if e != nil {
			return e
		}
		if !ok {
			return reviewError("missing run")
		}
		data, ok, e = tx.ReadGroup(reviewControlGroup, "runs/"+id+"/inputs/"+name)
		if e != nil {
			return e
		}
		if !ok || ReviewDigest(data) != run.InputHashes[name] {
			return reviewError("frozen input mismatch")
		}
		return nil
	})
	return
}

// ReviewIncrementalContext accepts unresolved semantic findings. Execution and
// archive integrity are required; semantic PASS is deliberately not a precondition.
func ReviewIncrementalContext(root, id string) (data []byte, err error) {
	run, err := ReadReviewRun(root, id)
	if err != nil {
		return nil, err
	}
	err = WithTransaction(root, reviewScope(run.TaskIDs, true), func(tx *Transaction) error { var e error; data, e = incrementalReviewContext(tx, run); return e })
	return
}
func incrementalReviewContext(tx *Transaction, run ReviewRun) (data []byte, err error) {
	err = func() error {
		if e := verifyPublishedReview(tx, run); e != nil {
			return e
		}
		var b ReviewBatch
		ok, e := readReviewJSON(tx, reviewBatchName(run.BatchID), &b)
		if e != nil {
			return e
		}
		if !ok {
			return reviewError("missing batch")
		}
		view, e := aggregateReviewBatch(tx, b)
		if e != nil {
			return e
		}
		if run.ExecutionStatus != "ok" {
			return reviewError("incremental predecessor needs an actual report")
		}
		report, ok, e := tx.ReadGroup(reviewControlGroup, "runs/"+run.RunID+"/originals/report.md")
		if e != nil {
			return e
		}
		if !ok {
			return reviewError("missing prior report")
		}
		ledger, e := readDispositionLedger(tx, run, true)
		if e != nil {
			return e
		}
		// A published disposition is optional; when present, its copies must agree.
		var published ReviewBatchView
		exists, e := readReviewJSON(tx, "dispositions/"+b.BatchID+".json", &published)
		if e != nil {
			return e
		}
		if exists {
			for _, task := range b.TaskIDs {
				text, e := tx.Read(task, "reviews/batches/"+b.BatchID+"/disposition.json")
				if e != nil {
					return e
				}
				if text != reviewJSON(published) {
					return reviewError("incremental disposition copy mismatch")
				}
			}
		}
		data = []byte(fmt.Sprintf("PREVIOUS_RUN_ID: %s\nPrior report (verbatim):\n%s\nAuthor records (verbatim JSON):\n%s\nCurrent batch disposition (tool generated):\n%s", run.RunID, report, reviewJSON(ledger), reviewJSON(view)))
		return nil
	}()
	return
}
