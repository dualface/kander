package board

import (
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/dualface/kander/v2/internal/config"
)

// CompletionReportSnapshot renders the CLI reminder with the completion report template from an
// already committed service snapshot, or "" when the state or the configuration asks for none.
// resource is a logical service location, not a client filesystem path; this entry never reads
// local board storage. Remarks about the template go to warn.
func CompletionReportSnapshot(taskID, state, text, resource string, cfg *config.Config, warn io.Writer) string {
	if state != "done" && state != "archived" && state != "trash" || cfg == nil || !cfg.Rules[config.RuleReporting] {
		return ""
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
	}, warn)
	if err != nil {
		fmt.Fprintln(warn, t("board.report_template_unavailable", err.Error()))
		return ""
	}
	return t("board.report_template_reminder") + "\n" + report + fmt.Sprintf("Write the report in %q.\n", language)
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
