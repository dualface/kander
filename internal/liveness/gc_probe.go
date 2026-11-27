package liveness

import (
	"context"

	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/terminal"
)

// ObserveExecutors classifies the retained executors of a worktree for
// `kander gc` the way a card is classified: when the recorded address is
// stale, the session is searched by reverse lookup, so an executor that moved
// to another pane is not taken for stopped. Only independent evidence counts
// as stopped: a stale address of a session without a reference was never
// searched for, and an executor without a terminal container has only process
// evidence that lives on a local board; both read unknown, so the worktree
// stays. An execution without a valid session or window reads unknown too.
func ObserveExecutors(ctx context.Context, executions []remote.RetainedExecution) []string {
	statuses := make([]string, len(executions))
	var inputs []TaskInput
	var indexes []int
	for i, execution := range executions {
		statuses[i] = Unknown
		if terminal.HasCapability(execution.Window, func(c terminal.Capabilities) bool { return !c.Container }) {
			continue
		}
		inputs = append(inputs, TaskInput{Entry: board.Entry{TaskID: execution.TaskID}, Text: executorText(execution)})
		indexes = append(indexes, i)
	}
	for n, report := range ClassifyTasksContext(ctx, inputs, BatchOptions{}) {
		i := indexes[n]
		session := ParseSessionValue(executions[i].Session)
		if report.Status == Stopped && (session == nil || session.Reference == "") {
			continue
		}
		statuses[i] = report.Status
	}
	return statuses
}

// executorText is the card metadata the classification reads: the session
// and the window of the retained execution.
func executorText(execution remote.RetainedExecution) string {
	return "- " + board.FieldSession + ": " + execution.Session + "\n- " + board.FieldWindow + ": " + execution.Window + "\n"
}
