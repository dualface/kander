package liveness

import (
	"context"

	"github.com/dualface/kander/v2/internal/remote"
)

// ObserveExecutors classifies the retained executors of a worktree for
// `kander gc` with the batch observation of `kander list --agent-status`. An
// execution without a valid session or window reads unknown, so its worktree
// stays.
func ObserveExecutors(ctx context.Context, executions []remote.RetainedExecution) []string {
	inputs := make([]AgentStatusInput, len(executions))
	for i, execution := range executions {
		inputs[i] = AgentStatusInput{TaskID: execution.TaskID, Session: execution.Session, Window: execution.Window}
	}
	reports := ObserveAgentStatus(ctx, inputs, BatchOptions{})
	statuses := make([]string, len(reports))
	for i, report := range reports {
		statuses[i] = report.Status
	}
	return statuses
}
