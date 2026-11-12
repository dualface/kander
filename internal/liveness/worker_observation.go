package liveness

import (
	"context"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/worker"
)

func init() { worker.ObserveAgent = observeWorkerAgent }
func observeWorkerAgent(ctx context.Context, c remote.Credential) api.AgentObservation {
	reports := ObserveAgentStatus(ctx, []AgentStatusInput{{TaskID: c.TaskID, Window: c.Window, Session: c.Session}}, BatchOptions{Budget: 2 * time.Second, Concurrency: 1})
	report := reports[0]
	status := "unknown"
	switch report.Status {
	case Alive, AliveOrphaned:
		status = "alive"
	case Stopped, IdentityMismatch:
		status = "stopped"
	}
	return api.AgentObservation{Status: status, Reason: report.Detail, RuntimeState: report.RuntimeState}
}
