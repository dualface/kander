package notify

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

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

// requestReasonLimit is the worker's limit on a blocked reason, in bytes
// (blockReasonLimit in internal/worker): a longer reason is cut at the end,
// which is where the next steps of a session marker hint stand.
const requestReasonLimit = 512

// resolveRequest finds the agent pane of a card's WINDOW and SESSION and checks
// that it takes a line. It types nothing: an error means no text reached any
// terminal. The prompt it returns types into the pane the resolution found,
// including a pane found by the session lookup. Every agent definition it
// reads comes from the project overlay of the address's configuration
// directory.
func resolveRequest(address worker.AgentAddress) (*worker.Target, error) {
	window := address.Window
	if window == "" {
		return nil, errors.New("the card records no WINDOW to deliver to")
	}
	lookup := config.AgentLookupIn(address.ConfigDir)
	agent, reference, _ := strings.Cut(address.Session, " ")
	if _, _, parsed := terminal.ParseWindow(window); parsed && reference == "" {
		// Without a session ID no pane can prove it runs this card's
		// session, so probing the terminal could never succeed.
		return nil, bareSessionError(lookup, agent)
	}
	target, err := ResolveTarget(window, "", liveness.TaskSession{Agent: agent, Reference: reference, Lookup: lookup}, requestTargetTimeout)
	var marker *SessionMarkerError
	if errors.As(err, &marker) {
		return nil, errors.New(sessionMarkerReason(err.Error()))
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
func bareSessionError(lookup config.AgentLookup, agent string) error {
	definition, err := lookup.Agent(agent)
	if err == nil && definition.Session.Mode == "none" {
		return errors.New(config.Text("notify.request_session_mode_none", agent))
	}
	return errors.New(config.Text("notify.request_session_without_id", agent))
}

// sessionMarkerReason appends the session marker hint to the failure text of
// the target. The failure text, which a stale address lookup composes from two
// diagnostics, is shortened so that the whole hint stays within the worker's
// reason limit.
func sessionMarkerReason(detail string) string {
	const ellipsis = "…"
	budget := requestReasonLimit - len(config.Text("notify.request_session_marker_hint", ""))
	if len(detail) > budget {
		cut := max(budget-len(ellipsis), 0)
		for cut > 0 && !utf8.RuneStart(detail[cut]) {
			cut--
		}
		detail = detail[:cut] + ellipsis
	}
	return config.Text("notify.request_session_marker_hint", detail)
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
