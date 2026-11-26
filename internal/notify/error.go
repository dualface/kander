package notify

import (
	"errors"
	"time"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/terminal"
)

const pollInterval = 100 * time.Millisecond

// Error is a displayable failure of the notify policy layer.
type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

// BusyError means the target was busy and never went idle within the timeout; it neither recovers nor touches the card.
type BusyError struct{ Message string }

func (e *BusyError) Error() string { return e.Message }

// UncertainError means the target's session identity could not be decided:
// it is neither a match nor proof of a different session, so it is never
// evidence for recovering another executor or closing the container.
type UncertainError struct{ Message string }

func (e *UncertainError) Error() string { return e.Message }

// SessionMarkerError marks a target resolution that failed on the session
// identity of the pane: the card's session reference was not proven by the
// pane, and no other pane was found to deliver to. Its text and its chain are
// those of the wrapped failure.
type SessionMarkerError struct{ Err error }

func (e *SessionMarkerError) Error() string { return e.Err.Error() }

func (e *SessionMarkerError) Unwrap() error { return e.Err }

func markSessionFailure(err error, session bool) error {
	if !session {
		return err
	}
	return &SessionMarkerError{Err: err}
}

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

func isUncertain(err error) bool {
	var uncertain *UncertainError
	if errors.As(err, &uncertain) {
		return true
	}
	// A reverse lookup that could not decide is equally uncertain evidence.
	var incomplete *terminal.IncompleteLookupError
	return errors.As(err, &incomplete)
}
