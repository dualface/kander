// Package window writes the card's window field back and restores the original text on failure.
package window

import (
	"regexp"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

const (
	SessionField = "会话"
	WindowField  = "窗口"
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

func replaceKeep1(pattern *regexp.Regexp, text, suffix string) string {
	return pattern.ReplaceAllString(text, "${1}"+quoteReplacement(suffix))
}

// RenderWindowMetadata writes or inserts the single window field; when the field is missing it is inserted after the session field.
func RenderWindowMetadata(text, value string) (string, error) {
	lines := regexp.MustCompile(`(?m)^- `+WindowField+`:.*$`).FindAllString(text, -1)
	if len(lines) > 1 {
		return "", windowError(
			"launch.task_document_must_contain_exactly_one_metadata_field", WindowField,
		)
	}
	if len(lines) == 0 {
		sessionLines := regexp.MustCompile(`(?m)^- `+SessionField+`:.*$`).FindAllString(text, -1)
		if len(sessionLines) != 1 {
			return "", windowError(
				"launch.task_document_must_contain_exactly_one_metadata_field", SessionField,
			)
		}
		return replaceKeep1(regexp.MustCompile(`(?m)^(- `+SessionField+`:.*)$`), text, "\n- "+WindowField+": "+value), nil
	}
	return replaceLiteral(regexp.MustCompile(`(?m)^- `+WindowField+`:.*$`), text, "- "+WindowField+": "+value), nil
}

// WriteDocument atomically writes the task document back.
func WriteDocument(root string, entry board.Entry, text string) error {
	err := fs.WriteTextAtomic(root, entry.Document, text, true)
	if err != nil {
		return windowError(
			"board.task_path_must_not_contain_a_symlink_reparse_point", err.Error(),
		)
	}
	return nil
}

// RestoreWindowText restores the card to the text it had before the call. It returns an error on failure and nil on success.
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
