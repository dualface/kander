package board

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/dualface/kander/internal/config"
)

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
	path, err := filepath.Abs(moved.Path)
	if err != nil {
		path = moved.Path
	}
	WriteCompletionReportSnapshot(w, moved.TaskID, moved.State, text, path, cfg)
}

// WriteCompletionReportSnapshot renders the same CLI reminder from an already
// committed service snapshot. resource is a logical service location, not a
// client filesystem path; this entry never reads local board storage.
func WriteCompletionReportSnapshot(w io.Writer, taskID, state, text, resource string, cfg *config.Config) {
	if state != "done" && state != "archived" && state != "trash" || cfg == nil || !cfg.Rules[config.RuleReporting] {
		return
	}
	language := strings.TrimSpace(MetadataFrom(text, FieldLanguage))
	if language == "" {
		language = cfg.AgentLanguage
	}
	paths, err := installPathsFn()
	if err != nil {
		paths = config.InstallPaths{}
	}
	report, err := config.RenderRulesTemplate(paths, "templates/report/completion.md", map[string]string{
		"TaskID":     taskID,
		"Title":      singleLine(TitleFrom(text)),
		"Path":       resource,
		"FinalState": finalCardState(state, MetadataFrom(text, FieldResult)),
	}, w)
	if err != nil {
		fmt.Fprintln(w, t("board.report_template_unavailable", err.Error()))
		return
	}
	fmt.Fprintln(w, t("board.report_template_reminder"))
	fmt.Fprint(w, report)
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
