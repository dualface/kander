package liveness

import (
	"context"
	"time"

	"github.com/dualface/kander/internal/board"
)

// RunList is the full-binary kander list: board's list with the
// --agent-status observation of `kander list --json`.
func RunList(args []string) int {
	return board.RunListWithAgentStatus(args, ObserveListAgents)
}

// ObserveListAgents observes the requested cards with ObserveAgentStatus and
// converts each Report into the snapshot agent object.
func ObserveListAgents(ctx context.Context, requests []board.AgentStatusRequest) []board.ListAgent {
	inputs := make([]AgentStatusInput, len(requests))
	for index, request := range requests {
		inputs[index] = AgentStatusInput{TaskID: request.TaskID, Session: request.Session, Window: request.Window}
	}
	reports := ObserveAgentStatus(ctx, inputs, BatchOptions{})
	agents := make([]board.ListAgent, len(reports))
	for index, report := range reports {
		agents[index] = board.ListAgent{
			Status:       report.Status,
			RuntimeState: report.RuntimeState,
			Reason:       report.Detail,
			ObservedAt:   report.ObservedAt.UTC().Format(time.RFC3339),
			Valid:        report.ObservationValid,
		}
	}
	return agents
}
