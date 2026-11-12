package board

// ReviewResolutionFacts freezes the historical originals and ancestry claims
// a client inspects before an audited resolution. CWD is the evidence identity,
// including a migrated legacy value; it is never a server filesystem path.
type ReviewResolutionFacts struct {
	TaskIDs       []string            `json:"task_ids"`
	PlanID        string              `json:"plan_id"`
	PlanRevision  uint64              `json:"plan_revision"`
	BatchID       string              `json:"batch_id"`
	BatchRevision uint64              `json:"batch_revision"`
	Runs          []ReviewResolvedRun `json:"runs"`
	Edges         []ReviewGitEdge     `json:"edges"`
	CWD           string              `json:"cwd"`
}

func ReviewBatchResolutionFacts(root string, x ReviewPlanExtension) (value ReviewResolutionFacts, err error) {
	ids, err := resolutionScope(root, x)
	if err != nil {
		return value, err
	}
	err = WithTransaction(root, reviewScope(ids, true), func(tx *Transaction) error {
		var err error
		value, err = ReviewBatchResolutionFactsInTransaction(tx, x)
		value.TaskIDs = ids
		return err
	})
	return value, err
}

// ReviewBatchResolutionFactsInTransaction reads only the locked plan/batch
// originals, allowing a service to recheck the client's exact inspected facts.
func ReviewBatchResolutionFactsInTransaction(tx *Transaction, x ReviewPlanExtension) (value ReviewResolutionFacts, err error) {
	plan, facts, err := resolutionEntry(tx, x)
	if err != nil {
		return value, err
	}
	return ReviewResolutionFacts{PlanID: plan.PlanID, PlanRevision: plan.Revision, BatchID: facts.batch.BatchID, BatchRevision: facts.batch.Revision, Runs: facts.bound, Edges: facts.edges, CWD: plan.CWD}, nil
}
