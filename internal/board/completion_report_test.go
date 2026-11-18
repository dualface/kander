package board

import (
	"regexp"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/rules"
)

const reportTemplateHeading = "# Kanban Task Completion Report"

// reportText renders the completion report for a card in state with the given
// body and the configured rules.
func reportText(t *testing.T, state, body string) string {
	t.Helper()
	cfg, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	_, _ = out.WriteString(CompletionReportSnapshot("20260101-report-task", state, body, "kanban/"+state+"/20260101-report-task", cfg, &out))
	return out.String()
}

const reportBody = "# 报告模板\n\n- TYPE: Feature\n- LANGUAGE: zh-CN\n- RESULT:\n"

func TestCompletionReportCarriesTemplateTaskAndLanguage(t *testing.T) {
	resetLang(t)
	tempBoard(t)
	got := reportText(t, "done", reportBody)
	for _, want := range []string{
		config.Text("board.report_template_reminder"),
		reportTemplateHeading,
		"- Task: [20260101-report-task - 报告模板](kanban/done/20260101-report-task)",
		"Final card state: done\n",
		`Write the report in "zh-CN".`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("report missing %q:\n%s", want, got)
		}
	}
}

func TestCompletionReportFinalState(t *testing.T) {
	resetLang(t)
	tempBoard(t)
	archived := strings.Replace(reportBody, "- RESULT:\n", "- RESULT: cancelled\n", 1)
	if got := reportText(t, "archived", archived); !strings.Contains(got, "Final card state: archived (cancelled)\n") {
		t.Fatalf("archived: %s", got)
	}
	if got := reportText(t, "trash", reportBody); !strings.Contains(got, "Final card state: trash\n") {
		t.Fatalf("trash: %s", got)
	}
}

func TestCompletionReportOnlyForTerminalStatesWithReporting(t *testing.T) {
	resetLang(t)
	tempBoard(t)
	for _, state := range []string{"backlog", "todo", "working", "review"} {
		if got := reportText(t, state, reportBody); got != "" {
			t.Fatalf("%s printed a report: %s", state, got)
		}
	}
	cfg, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Rules[config.RuleReporting] = false
	var out strings.Builder
	_, _ = out.WriteString(CompletionReportSnapshot("20260101-report-task", "done", reportBody, "kanban/done/20260101-report-task", cfg, &out))
	if out.Len() != 0 {
		t.Fatalf("reporting off printed a report: %s", out.String())
	}
}

func TestCompletionReportUsesConfigLanguageWithoutCardLanguage(t *testing.T) {
	resetLang(t)
	tempBoard(t)
	cfg, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AgentLanguage = "ja"
	var out strings.Builder
	_, _ = out.WriteString(CompletionReportSnapshot("20260101-report-task", "done", strings.Replace(reportBody, "- LANGUAGE: zh-CN\n", "", 1), "kanban/done/20260101-report-task", cfg, &out))
	if !strings.Contains(out.String(), `Write the report in "ja".`) {
		t.Fatalf("language fallback: %s", out.String())
	}
}

func TestCompletionReportTitleIsSingleLine(t *testing.T) {
	resetLang(t)
	tempBoard(t)
	body := strings.Replace(reportBody, "# 报告模板\n", "# 标题\x1b[31m\t红\r色\n", 1)
	if got := reportText(t, "done", body); !strings.Contains(got, "- Task: [20260101-report-task - 标题 [31m 红 色](") {
		t.Fatalf("title not single line: %q", got)
	}
}

func TestReportTemplateMatchesReleasedRules(t *testing.T) {
	data, err := rules.File("KANDER-REPORTING-RULES.md")
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile("(?s)```markdown\n(.*?)```").FindSubmatch(data)
	if block == nil {
		t.Fatal("template block missing from KANDER-REPORTING-RULES.md")
	}
	rendered, err := rules.File("templates/report/completion.md")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := templateLabels(string(rendered)), templateLabels(string(block[1])); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("template drift:\n got %v\nwant %v", got, want)
	}
}

func templateLabels(text string) []string {
	var labels []string
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "# "):
			labels = append(labels, line)
		case strings.HasPrefix(line, "- "):
			label, _, _ := strings.Cut(line, ": ")
			labels = append(labels, label)
		}
	}
	return labels
}
