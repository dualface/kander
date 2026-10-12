package terminal

import (
	"context"
	"errors"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
)

// AgentPane is one agent pane reported by an agent list, with the same
// meaning as the AgentIdentity fields of PaneFacts.
type AgentPane struct {
	Pane             string
	Agent            string
	AgentStatus      string
	AgentSession     string
	AgentSessionKind string
}

// Facts returns the pane facts the agent list reported for this pane.
func (p AgentPane) Facts() PaneFacts {
	return PaneFacts{Agent: p.Agent, AgentStatus: p.AgentStatus, AgentSession: p.AgentSession, AgentSessionKind: p.AgentSessionKind}
}

// AgentLister is an optional Backend extension of AgentIdentity backends: one
// call reports every agent pane, so observing many cards costs one process
// instead of one PaneFacts call per card. A pane missing from the list is not
// proof that it is gone; callers confirm it with PaneFacts.
type AgentLister interface {
	AgentList(context.Context, Conn) ([]AgentPane, error)
}

// AgentList runs the agent_list operation within the default probe budget.
// A definition that does not declare it returns ErrUnsupported.
func (b *DeclarativeBackend) AgentList(ctx context.Context, conn Conn) ([]AgentPane, error) {
	op, ok := b.op(OpAgentList)
	if !ok {
		return nil, ErrUnsupported
	}
	ctx, cancel := probe.WithDefaultTimeout(ctx)
	defer cancel()
	e := b.newExecution(ctx, conn, b.getenv, map[string]string{})
	if _, err := e.runOp(op); err != nil {
		return nil, err
	}
	rows, err := e.matchRows(op.Rows)
	if err != nil {
		return nil, err
	}
	panes := make([]AgentPane, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if seen[row["pane"]] {
			return nil, errors.New(config.Text("terminal.agent_list_duplicate_pane", row["pane"]))
		}
		seen[row["pane"]] = true
		panes = append(panes, AgentPane{
			Pane: row["pane"], Agent: row["agent"], AgentStatus: row["agent_status"],
			AgentSession: row["agent_session"], AgentSessionKind: row["agent_session_kind"],
		})
	}
	return panes, nil
}
