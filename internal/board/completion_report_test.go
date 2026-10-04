package board

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/rules"
)

const reportTemplateHeading = "# Kanban Task Completion Report"

func setReportingRule(t *testing.T, enabled bool) {
	t.Helper()
	cfg, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Rules[config.RuleReporting] = enabled
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
}

// workingCard creates a small card in working/ that is ready for done.
func workingCard(t *testing.T, root, slug, title string) string {
	t.Helper()
	id := todayID(slug)
	if code, _, err := capture(t, func() int { return RunNew([]string{"feature", slug, title}) }); code != 0 {
		t.Fatalf("new: %s", err)
	}
	makeReady(t, filepath.Join(root, "backlog", id, "spec.md"))
	for _, state := range []string{"todo", "working"} {
		if code, _, err := capture(t, func() int { return RunMove([]string{id, state}) }); code != 0 {
			t.Fatalf("%s: %s", state, err)
		}
	}
	complete(t, filepath.Join(root, "working", id, "spec.md"))
	return id
}

func TestMoveDonePrintsReportTemplateOnStderr(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	id := workingCard(t, root, "report-done", "报告模板")
	code, out, errText := capture(t, func() int { return RunMove([]string{id, "done", "--result", "completed"}) })
	if code != 0 {
		t.Fatalf("done: %s", errText)
	}
	donePath := filepath.Join(root, "done", id)
	if out != donePath+"\n" {
		t.Fatalf("stdout changed: %q", out)
	}
	for _, want := range []string{
		config.Text("board.report_template_reminder"),
		reportTemplateHeading,
		"- Task: [" + id + " - 报告模板](" + donePath + ")",
		"Final card state: done\n",
		`Write the report in "zh-CN".`,
	} {
		if !strings.Contains(errText, want) {
			t.Fatalf("stderr missing %q:\n%s", want, errText)
		}
	}
}

func TestMoveArchivedAndTrashReportFinalState(t *testing.T) {
	resetLang(t)
	tempBoard(t)
	archivedID := todayID("report-archived")
	capture(t, func() int { return RunNew([]string{"research", "report-archived", "归档"}) })
	code, _, errText := capture(t, func() int {
		return RunMove([]string{archivedID, "archived", "--result", "cancelled", "--reason", "user cancelled", "--decision", "test decision"})
	})
	if code != 0 || !strings.Contains(errText, "Final card state: archived (cancelled)\n") {
		t.Fatalf("archived: %d %s", code, errText)
	}
	trashID := todayID("report-trash")
	capture(t, func() int { return RunNew([]string{"research", "report-trash", "删除"}) })
	code, _, errText = capture(t, func() int {
		return RunMove([]string{trashID, "trash", "--result", "trashed", "--reason", "user deleted", "--decision", "test decision"})
	})
	if code != 0 || !strings.Contains(errText, "Final card state: trash\n") {
		t.Fatalf("trash: %d %s", code, errText)
	}
}

func TestMoveReportTemplateOnlyForTerminalStatesWithReporting(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	id := todayID("report-gate")
	capture(t, func() int { return RunNew([]string{"feature", "report-gate", "门控"}) })
	spec := filepath.Join(root, "backlog", id, "spec.md")
	makeReady(t, spec)
	setMeta(t, spec, "- TASK_BRANCH:\n", "- TASK_BRANCH: report-gate\n")
	for _, state := range []string{"todo", "backlog", "todo", "working", "review", "working"} {
		code, _, errText := capture(t, func() int { return RunMove([]string{id, state}) })
		if code != 0 || strings.Contains(errText, reportTemplateHeading) {
			t.Fatalf("%s: %d %s", state, code, errText)
		}
	}
	complete(t, filepath.Join(root, "working", id, "spec.md"))
	setReportingRule(t, false)
	code, _, errText := capture(t, func() int { return RunMove([]string{id, "done"}) })
	if code != 0 || errText != "" {
		t.Fatalf("reporting off: %d %q", code, errText)
	}
}

func TestMoveReportTemplateUsesConfigLanguageWithoutCardLanguage(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	id := workingCard(t, root, "report-lang", "语言")
	setMeta(t, filepath.Join(root, "working", id, "spec.md"), "- LANGUAGE: zh-CN\n", "")
	cfg, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AgentLanguage = "ja"
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	_, _, errText := capture(t, func() int { return RunMove([]string{id, "done"}) })
	if !strings.Contains(errText, `Write the report in "ja".`) {
		t.Fatalf("language fallback: %s", errText)
	}
}

func TestMoveReportTemplateSkipsOnConfigFailure(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	id := workingCard(t, root, "report-noconfig", "无配置")
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "missing.json"))
	code, out, errText := capture(t, func() int { return RunMove([]string{id, "done"}) })
	if code != 0 || out != filepath.Join(root, "done", id)+"\n" {
		t.Fatalf("move result changed: %d %q %s", code, out, errText)
	}
	if strings.Contains(errText, reportTemplateHeading) || !strings.Contains(errText, config.Text("board.report_template_unavailable", "")) {
		t.Fatalf("config failure: %s", errText)
	}
}

func TestMoveReportTemplateTitleIsSingleLine(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	id := workingCard(t, root, "report-title", "标题")
	setMeta(t, filepath.Join(root, "working", id, "spec.md"), "# 标题\n", "# 标题\x1b[31m\t红\r色\n")
	_, _, errText := capture(t, func() int { return RunMove([]string{id, "done"}) })
	if !strings.Contains(errText, "- Task: ["+id+" - 标题 [31m 红 色](") {
		t.Fatalf("title not single line: %q", errText)
	}
}

func TestMoveDispatchDonePrintsTemplateOnlyWhenNotReplayed(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	s := dispatchCard(t, root, "report-wrap")
	in := dispatchInput(s, "report-wrap-one")
	bindWrapUpFixture(t, root, &in)
	d := prepareTestDispatch(t, root, in)
	if _, err := dispatchMove(t, root, d, "working"); err != nil {
		t.Fatal(err)
	}
	args := []string{s.Entry.TaskID, "done", "--result", "completed", "--dispatch-id", d.Input.ID, "--execution-epoch", "1", "--delivery-commit", strings.Repeat("b", 40)}
	for _, replay := range []bool{false, true} {
		code, out, errText := capture(t, func() int { return RunMove(args) })
		var receipt struct {
			Replayed bool `json:"replayed"`
		}
		if code != 0 || json.Unmarshal([]byte(out), &receipt) != nil || receipt.Replayed != replay {
			t.Fatalf("replay %v: %d %q %s", replay, code, out, errText)
		}
		if strings.Contains(errText, reportTemplateHeading) == replay {
			t.Fatalf("replay %v template: %s", replay, errText)
		}
	}
}

// TestReportTemplateMatchesReleasedRules keeps the printed skeleton in step with the
// template in rules/KANDER-REPORTING-RULES.md: same heading and the same field labels in order.
func TestReportTemplateMatchesReleasedRules(t *testing.T) {
	data, err := rules.File("KANDER-REPORTING-RULES.md")
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile("(?s)```markdown\n(.*?)```").FindSubmatch(data)
	if block == nil {
		t.Fatal("template block missing from KANDER-REPORTING-RULES.md")
	}
	if got, want := templateLabels(completionReportTemplate), templateLabels(string(block[1])); strings.Join(got, "|") != strings.Join(want, "|") {
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
