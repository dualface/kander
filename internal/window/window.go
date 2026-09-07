// Package window renders window metadata and commits it through board transactions.
// Rollbacks reject stale revisions and never recreate moved cards.
package window

import (
	"regexp"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

const (
	SessionField = board.FieldSession
	WindowField  = board.FieldWindow
)

// Error is a failed window write-back.
type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

func windowError(id string, args ...any) *Error {
	return &Error{Message: config.Text(id, args...)}
}

func quoteReplacement(s string) string {
	return strings.ReplaceAll(s, "$", "$$")
}

func replaceLiteral(pattern *regexp.Regexp, text, repl string) string {
	return pattern.ReplaceAllString(text, quoteReplacement(repl))
}

// RenderWindowMetadata writes or inserts the single window field; when the field is missing it is inserted after the session field.
// A legacy Chinese field is updated in place, so the card keeps exactly one window field instead of gaining an English duplicate.
func RenderWindowMetadata(text, value string) (string, error) {
	windowRe := board.FieldLineRe(WindowField)
	lines := windowRe.FindAllString(text, -1)
	if len(lines) > 1 {
		return "", windowError(
			"launch.task_document_must_contain_exactly_one_metadata_field", WindowField,
		)
	}
	if len(lines) == 0 {
		sessionRe := board.FieldLineRe(SessionField)
		sessionLines := sessionRe.FindAllString(text, -1)
		if len(sessionLines) != 1 {
			return "", windowError(
				"launch.task_document_must_contain_exactly_one_metadata_field", SessionField,
			)
		}
		return replaceLiteral(sessionRe, text, sessionLines[0]+"\n"+board.RenderField(WindowField, value)), nil
	}
	return replaceLiteral(windowRe, text, board.RenderField(WindowField, value)), nil
}

// WriteDocument commits through the operation-local version cursor.
func WriteDocument(root string, entry board.Entry, text string) error {
	return board.WriteManagedDocument(root, entry, text)
}

// RestoreWindowText restores the pre-call text only while this operation owns
// the current revision. Newer records and moved paths produce a conflict.
func RestoreWindowText(root string, entry board.Entry, text string) error {
	return WriteDocument(root, entry, text)
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
