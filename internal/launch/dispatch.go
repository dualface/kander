package launch

import (
	"context"

	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/liveness"
)

// DispatchOptions opt working/generic messages into the durable protocol. A
// review card of a group or with a binding takes dispatch messages only.
type DispatchOptions struct {
	ID, Kind, Base, EvidenceFile string
	// Decision is the user decision of a takeover. It never opts into the
	// durable protocol.
	Decision string
	// Takeover marks a resume --agent. Like Decision, it opts into nothing.
	Takeover bool
}

// DispatchInstruction names the only business acknowledgement and completion
// entrances. A repeated receipt must never start the external work twice.
func DispatchInstruction(paths config.InstallPaths, d board.Dispatch) (string, error) {
	// A wrap-up completion binds the integration source of its evidence, so the
	// prompt names that commit instead of a placeholder the agent must guess.
	source := ""
	if w := d.Input.Evidence.WrapUp; w != nil {
		source = w.Git.SourceCommit
	}
	text, err := render(paths, "dispatch.md", map[string]any{
		"Command":       commandName,
		"TaskID":        d.Input.TaskID,
		"DispatchID":    d.Input.ID,
		"Epoch":         d.Authorization.Epoch,
		"Kind":          d.Input.Kind,
		"SourceCommit":  source,
		"PhaseSnippets": phaseSnippets(paths, dispatchSnippets(d.Input.Kind)...),
	})
	if err != nil {
		return "", err
	}
	return text + "\n" + board.DispatchEvidenceInstruction(d), nil
}

// DispatchObservation uses P3's shared deadline and identity-valid report. A
// transport recovery is permitted only from a valid stopped observation.
func DispatchObservation(ctx context.Context, s board.Snapshot) liveness.Report {
	reports := liveness.ClassifyTasksContext(ctx, []liveness.TaskInput{{Entry: s.Entry, Text: s.Text}}, liveness.BatchOptions{})
	return reports[0]
}
