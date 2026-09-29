package board

import (
	"os"
	"path/filepath"
	"slices"
	"time"
)

// frozenTaskContextError explains a later run of an existing batch whose task context differs from the
// one the batch froze. Publication appends the review index to each member's spec.md, so passing the
// live spec path again always drifts; the error names the frozen snapshot to pass instead, or says that
// none is available and a new batch is needed.
func frozenTaskContextError(tx *Transaction, batch ReviewBatch, previousRunID string) error {
	snapshot, err := frozenTaskContextSnapshot(tx, batch, previousRunID)
	if err != nil {
		return err
	}
	if snapshot == "" {
		return reviewError(t("board.review_task_context_frozen_no_snapshot", batch.BatchID))
	}
	return reviewError(t("board.review_task_context_frozen", batch.BatchID, snapshot))
}

// frozenTaskContextSnapshot picks a finalized run of the batch whose task context hash equals the frozen
// one: the named previous run when it qualifies, otherwise the earliest by creation time. It returns the
// absolute path of that run's published task-context.md in the first member, by task ID, that has it,
// or "" when no run qualifies.
func frozenTaskContextSnapshot(tx *Transaction, batch ReviewBatch, previousRunID string) (string, error) {
	runs, err := batchRuns(tx, batch)
	if err != nil {
		return "", err
	}
	var candidates []ReviewRun
	for _, run := range runs {
		if run.Phase == "finalized" && run.InputHashes["task-context.md"] == batch.TaskContextHash {
			candidates = append(candidates, run)
		}
	}
	slices.SortStableFunc(candidates, func(a, b ReviewRun) int {
		if a.RunID == previousRunID {
			return -1
		}
		if b.RunID == previousRunID {
			return 1
		}
		return reviewCreatedAt(a).Compare(reviewCreatedAt(b))
	})
	members := slices.Clone(batch.TaskIDs)
	slices.Sort(members)
	for _, run := range candidates {
		for _, id := range members {
			if !run.Published[id] {
				continue
			}
			card, err := tx.Snapshot(id)
			if err != nil {
				return "", err
			}
			path := filepath.Join(card.Entry.Path, "reviews", run.RunID, "task-context.md")
			if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
				return path, nil
			}
		}
	}
	return "", nil
}

// reviewCreatedAt parses a run's creation time; an unparsable value sorts first as the zero time.
func reviewCreatedAt(run ReviewRun) time.Time {
	created, _ := time.Parse(time.RFC3339Nano, run.CreatedAt)
	return created
}
