package liveness

import (
	"context"

	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/remote"
)

// ObserveExecutors classifies the retained executors of a worktree for
// `kander gc` the way a card is classified: when the recorded address is
// stale, the session is searched by reverse lookup, so an executor that moved
// to another pane is not taken for stopped. An execution without a valid
// session or window reads unknown, so its worktree stays.
func ObserveExecutors(ctx context.Context, executions []remote.RetainedExecution) []string {
	inputs := make([]TaskInput, len(executions))
	for i, execution := range executions {
		inputs[i] = TaskInput{Entry: board.Entry{TaskID: execution.TaskID}, Text: executorText(execution)}
	}
	reports := ClassifyTasksContext(ctx, inputs, BatchOptions{})
	statuses := make([]string, len(reports))
	for i, report := range reports {
		statuses[i] = report.Status
	}
	return statuses
}

// executorText is the card metadata the classification reads: the session
// and the window of the retained execution.
func executorText(execution remote.RetainedExecution) string {
	return "- " + board.FieldSession + ": " + execution.Session + "\n- " + board.FieldWindow + ": " + execution.Window + "\n"
}
