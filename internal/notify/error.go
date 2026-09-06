package notify

import (
	"errors"
	"time"

	"github.com/dualface/kander/internal/config"
)

const pollInterval = 100 * time.Millisecond

// Error is a displayable failure of the notify policy layer.
type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

// BusyError means the target was busy and never went idle within the timeout; it neither recovers nor touches the card.
type BusyError struct{ Message string }

func (e *BusyError) Error() string { return e.Message }

func notifyError(id string, args ...any) *Error {
	return &Error{Message: config.Text(id, args...)}
}

func t(id string, args ...any) string {
	return config.Text(id, args...)
}

func isBusy(err error) bool {
	var busy *BusyError
	return errors.As(err, &busy)
}
