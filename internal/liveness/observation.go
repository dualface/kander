package liveness

import "github.com/dualface/kander/internal/board"

// ObservationIdentity binds a result to its input, not to a later card with the
// same task ID. These fields are correlation evidence, not authentication.
type ObservationIdentity struct {
	TaskID    string `json:"task_id"`
	Session   string `json:"session"`
	Window    string `json:"window"`
	Owner     string `json:"owner"`
	StartedAt string `json:"started_at"`
}

func identityFrom(entry board.Entry, text string) ObservationIdentity {
	return ObservationIdentity{
		TaskID:    entry.TaskID,
		Session:   board.MetadataFrom(text, "SESSION"),
		Window:    board.MetadataFrom(text, "WINDOW"),
		Owner:     board.MetadataFrom(text, "OWNER"),
		StartedAt: board.MetadataFrom(text, "STARTED_AT"),
	}
}

// ValidFor rejects failed, uncollected and superseded observations. Consumers
// must additionally judge ObservedAt against their own freshness policy. It
// proves neither delivery readiness nor business progress, even for Alive.
func (r Report) ValidFor(entry board.Entry, text string) bool {
	return r.ObservationValid && !r.ObservedAt.IsZero() && r.Identity == identityFrom(entry, text)
}
