package board

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/dualface/kander/internal/config"
)

// completionReportTemplate mirrors the template in rules/KANDER-REPORTING-RULES.md;
// completion_report_test.go pins the skeleton against the released rules file.
const completionReportTemplate = `# Kanban Task Completion Report

- Task: [%s - %s](%s)
- Delivery: <user-observable outcome and key changes>
- Acceptance: <completed>/<total>; <per-item self-check conclusion or user-accepted exceptions>
- Verification: <actual commands and results; for failed or unexecuted items, the reason, impact, and substitute evidence>
- Review: <reviewer, status, and summary for PMQA (stage one) and Security (stage two); include N/A with the skip/exemption basis and fixes made during review>
- Wrap-up: <full SHA | N/A>; <integration result | N/A>; <main worktree sync, worktree, branch, temporary review files, ` + "`kander check`" + ` all completed or exceptions item by item>
- Unresolved issues (<N>): <None; or item by item ` + "`[source or category][tier or status] issue; impact: ...; reason: ...`" + `; attach send time and timeout time for timed-out items>
- Summary: <one-sentence summary>; Code branch: <branch where the code finally lives | N/A>; Final card state: %s
`

// writeCompletionReportReminder prints the completion report template after a CLI move
// into a terminal state while rules.reporting is enabled. It never changes the move result:
// a failure to read the configuration or the moved card only produces a notice.
func writeCompletionReportReminder(w io.Writer, moved Entry) {
	if moved.State != "done" && moved.State != "archived" && moved.State != "trash" {
		return
	}
	cfg, err := config.Load(false)
	if err != nil {
		fmt.Fprintln(w, t("board.report_template_unavailable", err.Error()))
		return
	}
	if !cfg.Rules[config.RuleReporting] {
		return
	}
	text, err := ReadDocument(moved)
	if err != nil {
		fmt.Fprintln(w, t("board.report_template_unavailable", err.Error()))
		return
	}
	language := strings.TrimSpace(MetadataFrom(text, FieldLanguage))
	if language == "" {
		language = cfg.AgentLanguage
	}
	path, err := filepath.Abs(moved.Path)
	if err != nil {
		path = moved.Path
	}
	fmt.Fprintln(w, t("board.report_template_reminder"))
	fmt.Fprintf(w, completionReportTemplate, moved.TaskID, singleLine(TitleFrom(text)), path, finalCardState(moved.State, MetadataFrom(text, FieldResult)))
	fmt.Fprintf(w, "Write the report in %q.\n", language)
}

func finalCardState(state, result string) string {
	if state == "archived" {
		return "archived (" + result + ")"
	}
	return state
}

// singleLine keeps a card title from adding lines or terminal control sequences to the report.
func singleLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	}), " ")
}
