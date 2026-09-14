package liveness

import (
	"context"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/terminal"
)

func taskGroupFrom(text string) string {
	return board.TaskGroupFrom(text)
}

// ParseTaskSession parses the session field of a card; it returns nil when the field cannot be parsed.
func ParseTaskSession(text string) *TaskSession {
	value := board.MetadataFrom(text, board.FieldSession)
	if value == "" {
		return nil
	}
	parts := strings.Fields(value)
	if len(parts) == 0 {
		return nil
	}
	agent := parts[0]
	reference := ""
	if len(parts) > 1 {
		reference = parts[1]
	}
	if !config.ValidAgentName(agent) || len(parts) > 2 || (reference != "" && !sessionReferenceRe.MatchString(reference)) {
		return nil
	}
	return &TaskSession{Agent: agent, Reference: reference}
}

func agentCommandName(agent string) (string, error) {
	definition, err := config.LoadAgent(agent)
	return definition.ProcessName, err
}

// lookupIdentity is the reverse-lookup identity of a card session. The expected
// foreground command is resolved only when a backend needs it.
func lookupIdentity(session TaskSession) terminal.Identity {
	return terminal.Identity{
		Agent:       session.Agent,
		Reference:   session.Reference,
		ProcessName: func() (string, error) { return agentCommandName(session.Agent) },
	}
}

// ReverseLookup uniquely locates the pane of a session through its backend,
// within the caller's shared budget. A completed search with zero or several
// matches returns a *terminal.MatchError.
func ReverseLookup(ctx context.Context, backend terminal.Backend, program string, session TaskSession) (terminal.Address, error) {
	return backend.ReverseLookup(ctx, terminal.Conn{Program: program, Run: terminal.ProbeRunner}, lookupIdentity(session))
}
