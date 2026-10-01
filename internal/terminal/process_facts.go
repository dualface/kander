package terminal

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/dualface/kander/internal/config"
)

// ForegroundProcess identifies a process the backend observes in a pane's job.
type ForegroundProcess struct {
	PID  int    `json:"pid"`
	Name string `json:"name"`
}

// ProcessInspector is an optional Backend extension. Older definitions and
// backends remain valid; without evidence they cannot repair missing identity.
type ProcessInspector interface {
	ProcessFacts(context.Context, Conn, string) ([]ForegroundProcess, error)
}

func (b *DeclarativeBackend) ProcessFacts(ctx context.Context, conn Conn, pane string) ([]ForegroundProcess, error) {
	if _, ok := b.op(OpProcessFacts); !ok {
		return nil, ErrUnsupported
	}
	result, err := b.probeOp(ctx, OpProcessFacts, conn, map[string]string{"pane": pane}, false)
	if err != nil {
		return nil, err
	}
	if result["pane"] != pane {
		return nil, errors.New(config.Text("terminal.process_facts_wrong_pane"))
	}
	var processes []ForegroundProcess
	if err := json.Unmarshal([]byte(result["processes"]), &processes); err != nil {
		return nil, errors.New(config.Text("terminal.process_facts_invalid"))
	}
	if len(processes) > 64 {
		return nil, errors.New(config.Text("terminal.process_facts_limit"))
	}
	seen := make(map[int]bool)
	for _, process := range processes {
		if process.PID <= 0 || process.Name == "" || seen[process.PID] {
			return nil, errors.New(config.Text("terminal.process_facts_identity_invalid"))
		}
		seen[process.PID] = true
	}
	return processes, nil
}
