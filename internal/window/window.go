// Package window restores window metadata through board transactions. Rollbacks
// reject stale revisions and never recreate moved cards. Writes of WINDOW go
// through the transition entrances of the board package.
package window

import (
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// WindowField names the card field this package restores.
const WindowField = board.FieldWindow

// RestoreWindowText restores the pre-call text only while this operation owns
// the current revision. Newer records and moved paths produce a conflict.
func RestoreWindowText(root string, entry board.Entry, text string) error {
	return board.RollbackDocument(root, entry, text, "")
}

// ResumeFailureMessage merges a failed resume, a failed cleanup and a failed window rollback.
func ResumeFailureMessage(primary error, cleanup error, rollback error) string {
	if primary == nil {
		return ""
	}
	var details []string
	if cleanup != nil {
		details = append(details, config.Text("window.cleanup", cleanup.Error()))
	}
	if rollback != nil {
		details = append(details, config.Text("window.window_rollback", rollback.Error()))
	}
	if len(details) == 0 {
		return ""
	}
	return primary.Error() + "; " + strings.Join(details, "; ")
}
