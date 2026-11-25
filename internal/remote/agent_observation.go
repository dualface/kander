package remote

import (
	"time"

	"github.com/dualface/kander/api"
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
	switch claim.Status {
	case api.ClaimLost:
		result.Reason = config.Text("remote.lease_lost_agent_unknown")
	case api.ClaimLapsed:
		result.Reason = config.Text("remote.lease_lapsed_agent_unknown", serverNow.Sub(claim.ExpiresAt).Round(time.Second).String())
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
	if !api.ClaimStands(claim.Status) || age < 0 || age > ttl {
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
