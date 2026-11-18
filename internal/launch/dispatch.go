package launch

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/liveness"
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
	text, err := render(paths, "dispatch.md", map[string]any{
		"Command":       commandName(paths),
		"TaskID":        d.Input.TaskID,
		"DispatchID":    d.Input.ID,
		"Epoch":         d.Authorization.Epoch,
		"Kind":          d.Input.Kind,
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

// PrintDispatchResult reports durable state, never terminal prompt echo.
func PrintDispatchResult(d board.Dispatch) error { return json.NewEncoder(os.Stdout).Encode(d) }

// ParseDispatchOptions shares the notify/resume flag spelling and rejects duplicates.
func ParseDispatchOptions(args []string) ([]string, DispatchOptions, error) {
	var o DispatchOptions
	var rest []string
	values := map[string]*string{"--dispatch-id": &o.ID, "--kind": &o.Kind, "--base": &o.Base, "--evidence-file": &o.EvidenceFile}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		name, value, inline := strings.Cut(args[i], "=")
		field, ok := values[name]
		if !ok {
			rest = append(rest, args[i])
			// Preserve the value boundary owned by the notify/resume parser.
			// A message or path may itself look like a dispatch control option.
			if !inline && i+1 < len(args) {
				switch name {
				case "--message", "--message-file", "--timeout", "--agent", "--launcher", "--pane", "--decision":
					i++
					rest = append(rest, args[i])
				}
			}
			continue
		}
		if seen[name] {
			return nil, o, launchError("board.unknown_option", name)
		}
		seen[name] = true
		if !inline {
			i++
			if i >= len(args) {
				return nil, o, launchError("board.option_requires_a_value", name)
			}
			value = args[i]
		}
		if value == "" {
			return nil, o, launchError("board.option_requires_a_value", name)
		}
		*field = value
	}
	return rest, o, nil
}
