package notify

import (
	"errors"
	"strings"

	"github.com/dualface/kander/internal/liveness"
	"github.com/dualface/kander/internal/worker"
)

// The background worker delivers collaboration requests through the same
// target resolution and agent prompt primitive as kander notify.
func init() { worker.Resolve = resolveRequest }

// requestTargetTimeout bounds the target probe of one request delivery.
const requestTargetTimeout = 10

// resolveRequest finds the agent pane of a card's WINDOW and SESSION and checks
// that it takes a line. It types nothing: an error means no text reached any
// terminal. The prompt it returns types into the pane the resolution found,
// including a pane found by the session lookup.
func resolveRequest(window, session string) (worker.Prompt, error) {
	if window == "" {
		return nil, errors.New("the card records no WINDOW to deliver to")
	}
	agent, reference, _ := strings.Cut(session, " ")
	target, err := ResolveTarget(window, "", liveness.TaskSession{Agent: agent, Reference: reference}, requestTargetTimeout)
	if err != nil {
		return nil, err
	}
	return func(text string) error {
		return AgentPrompt(target.Backend, target.Program, target.PaneID, text)
	}, nil
}
