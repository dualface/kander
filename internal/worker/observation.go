package worker

import (
	"context"
	"errors"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/remote"
)

// ObserveAgent is provided by the terminal observation layer. It observes only
// immutable host-local routing facts, never another client's card address.
var ObserveAgent func(context.Context, remote.Credential) api.AgentObservation

func (w *loop) observe(ctx context.Context, c remote.Credential) api.AgentObservation {
	if w.o.Observe != nil {
		return w.o.Observe(ctx, c)
	}
	if c.AgentPID > 0 {
		if c.AgentStart == "" || c.BootID == "" {
			return api.AgentObservation{Status: "unknown", Reason: "process incarnation has not been recorded"}
		}
		boot, err := process.BootIdentity()
		if err != nil {
			return api.AgentObservation{Status: "unknown", Reason: err.Error()}
		}
		if !process.SameBoot(c.BootID, boot) {
			return api.AgentObservation{Status: "stopped", Reason: "the recorded execution predates this system boot"}
		}
		facts, err := process.ObserveProcess(ctx, c.AgentPID)
		if errors.Is(err, process.ErrProcessGone) {
			return api.AgentObservation{Status: "stopped", Reason: "the recorded agent process exited"}
		}
		if err != nil {
			return api.AgentObservation{Status: "unknown", Reason: err.Error()}
		}
		if facts.Start != c.AgentStart {
			return api.AgentObservation{Status: "stopped", Reason: "the recorded process ID was reused"}
		}
		return api.AgentObservation{Status: "alive"}
	}
	if ObserveAgent != nil && c.Window != "" && c.Session != "" {
		return ObserveAgent(ctx, c)
	}
	return api.AgentObservation{Status: "unknown", Reason: "agent launch facts are incomplete"}
}
