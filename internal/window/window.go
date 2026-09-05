// Package window 负责卡片「窗口」字段的回写与失败恢复原文.
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

// Error 是窗口回写失败.
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

// RenderWindowMetadata 写入或插入唯一的窗口字段; 缺字段时插在会话之后.
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

// WriteDocument 原子回写任务文档.
func WriteDocument(root string, entry board.Entry, text string) error {
	err := fs.WriteTextAtomic(root, entry.Document, text, true)
	if err != nil {
		return windowError(
			"board.task_path_must_not_contain_a_symlink_reparse_point", err.Error(),
		)
	}
	return nil
}

// RestoreWindowText 把卡片恢复为调用前原文. 失败返回错误, 成功返回 nil.
func RestoreWindowText(root string, entry board.Entry, text string) error {
	return WriteDocument(root, entry, text)
}

// ResumeFailureMessage 合并恢复失败、清理失败和窗口回滚失败.
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
