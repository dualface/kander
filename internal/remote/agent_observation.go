package remote

import (
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// ClaimAgent translates service-clock observation age for local display. Lease
// loss never becomes invented evidence that the actual agent process stopped.
func ClaimAgent(claim *api.ClaimView, serverNow, localNow time.Time) board.ListAgent {
	result := board.ListAgent{Status: "unknown", RuntimeState: "unknown", Reason: config.Text("remote.agent_observation_pending"), ObservedAt: localNow.Format(time.RFC3339Nano)}
	if claim == nil {
		return result
	}
	if claim.Status == api.ClaimLost {
		result.Reason = config.Text("remote.lease_lost_agent_unknown")
	}
	observation := claim.Observation
	if observation == nil {
		return result
	}
	if serverNow.IsZero() {
		serverNow = localNow
	}
	result.ObservedAt = localNow.Add(observation.ObservedAt.Sub(serverNow)).Format(time.RFC3339Nano)
	result.Reason = observation.Reason
	if observation.Status == "stopped" {
		result.Status = "stopped"
		result.RuntimeState = "stopped"
		result.Valid = true
		return result
	}
	ttl := claim.ExpiresAt.Sub(claim.RenewedAt)
	age := serverNow.Sub(observation.ObservedAt)
	if claim.Status != api.ClaimActive || !serverNow.Before(claim.ExpiresAt) || age < 0 || age > ttl {
		result.Reason = config.Text("remote.agent_observation_stale")
		return result
	}
	if observation.Status == "alive" {
		result.Status = "alive"
		result.RuntimeState = observation.RuntimeState
		result.Valid = true
	}
	return result
}
