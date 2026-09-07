package probe

import (
	"context"
	"errors"
	"github.com/dualface/kander/internal/config"
)

// Error is a displayable failure of pane fact collection; it carries no delivery or liveness policy.
type Error struct {
	Message string
}

func (e *Error) Error() string { return e.Message }

func probeError(id string, args ...any) *Error {
	return &Error{Message: config.Text(id, args...)}
}

// FailureDetail localizes cancellation without discarding the underlying cause.
func FailureDetail(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return config.Text("probe.deadline_exceeded", err.Error())
	}
	if errors.Is(err, context.Canceled) {
		return config.Text("probe.canceled", err.Error())
	}
	return err.Error()
}
