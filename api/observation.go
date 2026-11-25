package api

import "time"

// AgentObservation reports execution presence independently of worker liveness.
// Unknown never proves an exit. ObservedAt is assigned by the service.
type AgentObservation struct {
	RuntimeState string    `json:"runtime_state,omitempty"`
	Status       string    `json:"status"`
	Reason       string    `json:"reason,omitempty"`
	ObservedAt   time.Time `json:"observed_at,omitzero"`
}
