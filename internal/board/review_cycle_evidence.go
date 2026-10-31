package board

// reviewLedgerScope is the review scope of an operation that reads the execution
// ledger of a member.
func reviewLedgerScope(ids []string, readOnly bool) LockScope {
	scope := reviewScope(ids, readOnly)
	scope.Groups = append(scope.Groups, taskStartGroup)
	return scope
}

// cycleUnchanged reports a card that only ever had one execution cycle. The card and
// the from side of a link count too: a binary with no ledger left no link for a
// change of its time.
func cycleUnchanged(tx *Transaction, s Snapshot) (bool, error) {
	l, err := readExecutionLedger(tx, s)
	if err != nil {
		return false, err
	}
	empty := emptyCycle(s.Entry.TaskID)
	cycles := map[string]bool{planCycle(s): true}
	for _, t := range l.chain {
		cycles[t.FromCycle], cycles[t.ToCycle] = true, true
	}
	delete(cycles, empty)
	return len(cycles) <= 1, nil
}

// runCoversCycle reports a run that reviewed the current cycle of a member. A run
// from before the binding names no cycle, so it counts only while no other existed.
func runCoversCycle(tx *Transaction, run ReviewRun, id string) (bool, error) {
	s, err := tx.Snapshot(id)
	if err != nil {
		return false, err
	}
	if run.MemberCycles != nil {
		return run.MemberCycles[id] == planCycle(s), nil
	}
	return cycleUnchanged(tx, s)
}

// requireCurrentConclusions refuses a new closure on the evidence of another
// execution cycle. It runs at the close only: a closed batch stays valid after its
// member moved on. A chain may start in the old cycle, as long as its head reviewed
// the current one.
func requireCurrentConclusions(tx *Transaction, b ReviewBatch, r ReviewCloseRequest) error {
	for _, role := range reviewRequirementRoles(b.Requirements) {
		if b.Requirements[role] != "required" {
			continue
		}
		var run ReviewRun
		ok, err := readReviewJSON(tx, reviewRunName(r.Roles[role].RunID), &run)
		if err != nil {
			return err
		}
		if !ok {
			return reviewError("missing run")
		}
		for _, id := range b.TaskIDs {
			excluded, err := memberExcluded(tx, id)
			if err != nil {
				return err
			}
			if excluded {
				continue
			}
			current, err := runCoversCycle(tx, run, id)
			if err != nil {
				return err
			}
			if !current {
				return reviewError(t("board.review_stale_conclusion", run.RunID, role, id, b.BatchID))
			}
		}
	}
	return nil
}

// staleRunActions names each role and member of an open batch that has successful
// runs, none of them of the current cycle of the member.
func staleRunActions(tx *Transaction, b ReviewBatch, runs []ReviewRun) ([]ReviewNextAction, error) {
	var actions []ReviewNextAction
	for _, role := range reviewRequirementRoles(b.Requirements) {
		if b.Requirements[role] != "required" {
			continue
		}
		heads := reviewChainHeads(runs, role)
		if len(heads) == 0 {
			continue
		}
		for _, id := range b.TaskIDs {
			excluded, err := memberExcluded(tx, id)
			if err != nil {
				return nil, err
			}
			if excluded {
				continue
			}
			current := false
			for _, run := range runs {
				if run.Role != role || run.ExecutionStatus != "ok" {
					continue
				}
				if current, err = runCoversCycle(tx, run, id); current || err != nil {
					break
				}
			}
			if err != nil {
				return nil, err
			}
			if !current {
				actions = append(actions, ReviewNextAction{Action: "rerun-current-cycle", BatchID: b.BatchID, Role: role, RunIDs: heads, TaskID: id})
			}
		}
	}
	return actions, nil
}
