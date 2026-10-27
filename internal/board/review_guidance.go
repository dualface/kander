package board

import (
	"slices"
	"strings"
)

// ReviewNextAction is a machine-readable recovery step for an unclosed batch.
type ReviewNextAction struct {
	Action  string   `json:"action"`
	BatchID string   `json:"batch_id"`
	Role    string   `json:"role"`
	RunIDs  []string `json:"run_ids"`
}

// reviewChainHeads returns the successful runs of role that no other successful
// run names as previous_run_id. More than one head means parallel duplicates.
func reviewChainHeads(runs []ReviewRun, role string) []string {
	referenced := map[string]bool{}
	for _, r := range runs {
		if r.Role == role && r.ExecutionStatus == "ok" && r.PreviousRunID != "" {
			referenced[r.PreviousRunID] = true
		}
	}
	var heads []string
	for _, r := range runs {
		if r.Role == role && r.ExecutionStatus == "ok" && !referenced[r.RunID] {
			heads = append(heads, r.RunID)
		}
	}
	slices.Sort(heads)
	return heads
}

func duplicateRunActions(b ReviewBatch, runs []ReviewRun) []ReviewNextAction {
	var actions []ReviewNextAction
	for _, role := range reviewRequirementRoles(b.Requirements) {
		if heads := reviewChainHeads(runs, role); len(heads) > 1 {
			actions = append(actions, ReviewNextAction{Action: "supersede-duplicate-runs", BatchID: b.BatchID, Role: role, RunIDs: heads})
		}
	}
	return actions
}

// UnlinkedReviewRuns lists the runs of run's batch and role that its
// predecessor chain does not reach and that closure will still have to place:
// successful chain heads and runs not yet finalized. Finalized failures are
// left out because a failed run is retried as a fresh run and bound through
// resolved_failures.
func UnlinkedReviewRuns(root string, run ReviewRun) ([]ReviewRun, error) {
	var unlinked []ReviewRun
	err := WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
		var b ReviewBatch
		ok, err := readReviewJSON(tx, reviewBatchName(run.BatchID), &b)
		if err != nil || !ok {
			return err
		}
		runs, err := batchRuns(tx, b)
		if err != nil {
			return err
		}
		byID := map[string]ReviewRun{}
		for _, r := range runs {
			byID[r.RunID] = r
		}
		lineage := map[string]bool{run.RunID: true}
		for id := run.PreviousRunID; id != "" && !lineage[id]; id = byID[id].PreviousRunID {
			lineage[id] = true
		}
		heads := reviewChainHeads(runs, run.Role)
		for _, r := range runs {
			if r.Role != run.Role || lineage[r.RunID] {
				continue
			}
			if r.Phase != "finalized" || slices.Contains(heads, r.RunID) {
				unlinked = append(unlinked, r)
			}
		}
		return nil
	})
	return unlinked, err
}

// UnlinkedReviewRunsWarning renders the start warning, or "" when none applies.
func UnlinkedReviewRunsWarning(run ReviewRun, unlinked []ReviewRun) string {
	if len(unlinked) == 0 {
		return ""
	}
	items := make([]string, 0, len(unlinked))
	for _, r := range unlinked {
		items = append(items, r.RunID+" ("+r.Phase+"/"+r.ExecutionStatus+")")
	}
	return t("board.review_unlinked_runs_warning", run.BatchID, run.Role, strings.Join(items, ", "), run.RunID)
}
