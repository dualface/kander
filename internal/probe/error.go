package probe

import "github.com/dualface/kander/internal/config"

// Error is a displayable failure of pane fact collection; it carries no delivery or liveness policy.
type Error struct {
	Message string
}

func (e *Error) Error() string { return e.Message }

func probeError(id string, args ...any) *Error {
	return &Error{Message: config.Text(id, args...)}
}
