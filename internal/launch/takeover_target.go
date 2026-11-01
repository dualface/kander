package launch

import (
	"context"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/liveness"
	"github.com/dualface/kander/internal/terminal"
)

// takeoverTarget is the old executor the cleanup of a takeover acts on.
type takeoverTarget struct {
	window  string
	session AgentSession
	// failure is why the search found no answer, so nothing counts as cleaned.
	failure string
}

// locateOldExecutor searches the session of a card before the new executor starts.
// After that start a search could find the new executor: the Codex rollout scan
// takes the newest session of the task, and the new pane matches a generated
// session of the same task. A card with no container, or with no session to search,
// keeps the recorded address.
func locateOldExecutor(entry board.Entry, text, oldWindow string, oldSession AgentSession) takeoverTarget {
	target := takeoverTarget{window: oldWindow, session: oldSession}
	if _, _, ok := terminal.ParseWindow(oldWindow); !ok || CleanupTakeover == nil || parseTaskSession(text) == nil {
		return target
	}
	report := recoverLivenessFn(context.Background(), board.Snapshot{Entry: entry, Text: text})
	switch report.Status {
	case liveness.Drifted:
		target.window = report.NewWindow
		if report.Reference != "" {
			target.session.Reference = report.Reference
		}
	case liveness.Unknown:
		target.failure = strings.Join(strings.Fields(report.Detail), " ")
		// The search saw an executor it cannot prove, so that pane is the one kept.
		if report.Evidence != nil && report.Evidence.Window != "" {
			target.window = report.Evidence.Window
		}
	}
	return target
}
