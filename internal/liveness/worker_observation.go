package liveness

import (
	"context"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/worker"
)

func init() { worker.ObserveAgent = observeWorkerAgent }

// observeWorkerAgent observes the agent of an execution for the worker, with
// the agent definition read from the project overlay of the address's
// configuration directory.
func observeWorkerAgent(ctx context.Context, address worker.AgentAddress) api.AgentObservation {
	input := AgentStatusInput{Window: address.Window, Session: address.Session, Lookup: config.AgentLookupIn(address.ConfigDir)}
	reports := ObserveAgentStatus(ctx, []AgentStatusInput{input}, BatchOptions{Budget: 2 * time.Second, Concurrency: 1})
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
