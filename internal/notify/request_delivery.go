package notify

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/liveness"
	"github.com/dualface/kander/v2/internal/terminal"
	"github.com/dualface/kander/v2/internal/worker"
)

// The background worker delivers collaboration requests through the same
// target resolution and agent prompt primitive as kander notify.
func init() {
	worker.Resolve = resolveRequest
	worker.CreateRequestFile = writeRequestFile
}

// requestTargetTimeout bounds the target probe of one request delivery.
const requestTargetTimeout = 10

// resolveRequest finds the agent pane of a card's WINDOW and SESSION and checks
// that it takes a line. It types nothing: an error means no text reached any
// terminal. The prompt it returns types into the pane the resolution found,
// including a pane found by the session lookup.
func resolveRequest(window, session string) (*worker.Target, error) {
	if window == "" {
		return nil, errors.New("the card records no WINDOW to deliver to")
	}
	agent, reference, _ := strings.Cut(session, " ")
	if _, _, parsed := terminal.ParseWindow(window); parsed && reference == "" {
		// Without a session ID no pane can prove it runs this card's
		// session, so probing the terminal could never succeed.
		return nil, bareSessionError(agent)
	}
	target, err := ResolveTarget(window, "", liveness.TaskSession{Agent: agent, Reference: reference}, requestTargetTimeout)
	var marker *SessionMarkerError
	if errors.As(err, &marker) {
		return nil, errors.New(config.Text("notify.request_session_marker_hint", err.Error()))
	}
	if err != nil {
		return nil, err
	}
	return &worker.Target{
		Window: target.Window,
		Prompt: func(text string) error {
			return AgentPrompt(target.Backend, target.Program, target.PaneID, text)
		},
		Seen: func(ctx context.Context, marker string) (bool, error) {
			return seenAcknowledgement(ctx, target.Backend, probeConn(target.Program), target.PaneID, marker)
		},
	}, nil
}

// bareSessionError explains a card SESSION that names only the agent. An
// agent whose effective definition records no session ID gets no takeover
// hint: a new session of it would record none either. A definition that
// cannot be loaded keeps the general text, which holds for any agent.
func bareSessionError(agent string) error {
	definition, err := config.LoadAgent(agent)
	if err == nil && definition.Session.Mode == "none" {
		return errors.New(config.Text("notify.request_session_mode_none", agent))
	}
	return errors.New(config.Text("notify.request_session_without_id", agent))
}

func seenAcknowledgement(ctx context.Context, backend terminal.Backend, conn terminal.Conn, pane, marker string) (bool, error) {
	if backend.Capabilities().WaitOutput && backend.WaitOutput(ctx, conn, pane, marker, 1000) == nil {
		return true, nil
	}
	// A literal wait cannot see a marker that wraps in a narrow pane.
	output, err := backend.ReadOutput(ctx, conn, pane)
	if err != nil {
		return false, err
	}
	withoutSpace := func(text string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, text)
	}
	return strings.Contains(withoutSpace(output), withoutSpace(marker)), nil
}
