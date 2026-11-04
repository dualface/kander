package notify

import (
	"errors"
	"strings"

	"github.com/dualface/kander/internal/liveness"
	"github.com/dualface/kander/internal/worker"
)

// The background worker delivers collaboration requests through the same
// target resolution and agent prompt primitive as kander notify.
func init() { worker.Deliver = deliverRequest }

// requestTargetTimeout bounds the target probe of one request delivery.
const requestTargetTimeout = 10

func deliverRequest(window, session, text string) error {
	if window == "" {
		return errors.New("the card records no WINDOW to deliver to")
	}
	agent, reference, _ := strings.Cut(session, " ")
	target, err := ResolveTarget(window, "", liveness.TaskSession{Agent: agent, Reference: reference}, requestTargetTimeout)
	if err != nil {
		return err
	}
	return AgentPrompt(target.Backend, target.Program, target.PaneID, text)
}
