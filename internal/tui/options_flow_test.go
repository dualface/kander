package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/flow"
	"github.com/dualface/kander/internal/i18n"
)

func TestFlowRootAndReadOnlySession(t *testing.T) {
	app, panel := openPanel(t)
	root := ansi.Strip(panel.form.View())
	if a, b, c := strings.Index(root, config.Text("rules.modules")), strings.Index(root, config.Text("flow.title")), strings.Index(root, config.Text("tui.environment_check")); a < 0 || b <= a || c <= b {
		t.Fatalf("wrong root order: %s", root)
	}
	before := config.Clone(panel.session.Config)
	path := os.Getenv(config.EnvConfig)
	disk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	moveRootTo(t, panel, sectionFlow)
	drivePanel(panel, keyMsg("enter"))
	if panel.report == nil || panel.report.title != config.Text("flow.title") || panel.dirty || app.pendingWork != nil || app.pendingShell != nil {
		t.Fatal("flow must open a synchronous, clean, read-only report")
	}
	if panel.flowScale != "large" {
		t.Fatalf("default scale=%q", panel.flowScale)
	}
	drivePanel(panel, keyMsg("esc"))
	if panel.report != nil || panel.form == nil || panel.section != sectionFlow || panel.current != "" || panel.flowScale != "" {
		t.Fatal("Escape must restore the root and selected flow entry")
	}
	if !reflect.DeepEqual(before, panel.session.Config) {
		t.Fatal("viewing changed configuration")
	}
	pumpPanel(panel, panel.dispatch(sectionRules))
	*panel.bind.rules[config.RuleReview] = false
	panel.bind.apply(panel)
	drivePanel(panel, keyMsg("esc"))
	panel.dispatch(sectionFlow)
	if !panel.dirty || !strings.Contains(panel.report.title, config.Text("tui.unsaved")) {
		t.Fatal("unsaved state must be retained and visible")
	}
	text := flowReportText(panel)
	if !strings.Contains(text, config.Text("flow.review_off")) {
		t.Fatalf("expected review-off note: %s", text)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(disk) != string(after) {
		t.Fatal("read-only flow wrote disk configuration")
	}
}

func TestFlowScaleTabsAndChart(t *testing.T) {
	_, panel := openPanel(t)
	panel.session.Config.KanbanAgents["large"] = "codex"
	panel.session.Config.KanbanAgents["small"] = "claude"
	panel.session.Config.Models.Kanban["codex"]["large_model"] = "large-only"
	panel.session.Config.Models.Kanban["codex"]["large_effort"] = "high"
	panel.session.Config.Models.Kanban["claude"]["small_model"] = "small-only"
	panel.session.Config.Models.Kanban["claude"]["small_effort"] = "low"
	panel.dispatch(sectionFlow)

	text := flowReportText(panel)
	if !strings.Contains(text, "▸ "+config.Text("tui.review_group_large")) {
		t.Fatalf("missing large tab: %s", text)
	}
	if !strings.Contains(text, "large-only (high)") || strings.Contains(text, "small-only") {
		t.Fatalf("large chart should show large model only: %s", text)
	}
	if !strings.Contains(text, "┌") || !strings.Contains(text, "└") {
		t.Fatalf("expected flowchart boxes: %s", text)
	}

	drivePanel(panel, keyMsg("right"))
	if panel.flowScale != "small" {
		t.Fatalf("right scale=%q", panel.flowScale)
	}
	text = flowReportText(panel)
	if !strings.Contains(text, "▸ "+config.Text("tui.review_group_small")) {
		t.Fatalf("missing small tab: %s", text)
	}
	if !strings.Contains(text, "small-only (low)") || strings.Contains(text, "large-only") {
		t.Fatalf("small chart should show small model only: %s", text)
	}
	drivePanel(panel, keyMsg("left"))
	if panel.flowScale != "large" {
		t.Fatalf("left scale=%q", panel.flowScale)
	}
}

func TestFlowNarrowReportScrolling(t *testing.T) {
	for _, lang := range []string{"en", "cn", "ja"} {
		t.Run(lang, func(t *testing.T) {
			config.ApplyLanguageArgument([]string{"--lang", lang})
			defer config.ApplyLanguageArgument(nil)
			app, panel := openPanel(t)
			app.Width, app.Height = 72, 12
			panel.session.Config.Models.Kanban["codex"]["large_model"] = strings.Repeat("model-", 8)
			panel.dispatch(sectionFlow)
			if panel.innerWidth() != 60 {
				t.Fatalf("inner width %d", panel.innerWidth())
			}
			panel.view()
			for _, line := range strings.Split(panel.report.view.View(), "\n") {
				if !utf8.ValidString(line) || ansi.StringWidth(line) > 60 || strings.Contains(line, "�") {
					t.Fatalf("broken narrow output: %q", line)
				}
			}
			for _, msg := range []tea.Msg{tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyPgDown}} {
				old := panel.report.view.YOffset
				panel.Update(msg)
				if panel.report.view.YOffset <= old {
					t.Fatal("keyboard must scroll report")
				}
			}
			old := panel.report.view.YOffset
			panel.Update(tea.KeyMsg{Type: tea.KeyPgUp})
			if panel.report.view.YOffset >= old {
				t.Fatal("PgUp must scroll upward")
			}
			old = panel.report.view.YOffset
			panel.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
			if panel.report.view.YOffset <= old {
				t.Fatal("wheel must scroll report")
			}
			panel.report.view.Height = panel.report.view.TotalLineCount()
			panel.report.view.GotoTop()
			text := ansi.Strip(panel.report.view.View())
			for _, line := range strings.Split(text, "\n") {
				if !utf8.ValidString(line) || ansi.StringWidth(line) > 60 {
					t.Fatalf("broken wrapped line: %q", line)
				}
			}
			if !strings.Contains(text, i18n.Text(lang, "tui.review_group_large")) {
				t.Fatal("missing localized large-task tab")
			}
		})
	}
}

func TestFlowShowsUnsavedModelsAndCLIDefault(t *testing.T) {
	_, panel := openPanel(t)
	panel.session.Config.Models.Kanban["codex"]["large_model"] = "unsaved-large"
	panel.session.Config.Models.ReviewRoles["PMQA"]["large_model"] = "unsaved-pm"
	panel.session.Config.Models.Kanban["codex"]["small_model"] = ""
	panel.session.Config.Models.Kanban["codex"]["model"] = ""
	panel.session.Config.ReviewStages["large"]["PMQA"] = "required"
	panel.session.Config.ReviewStages["large"]["Security"] = "skip"
	panel.dispatch(sectionFlow)
	text := flowReportText(panel)
	for _, want := range []string{"unsaved-large", "unsaved-pm"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	drivePanel(panel, keyMsg("right"))
	text = flowReportText(panel)
	if !strings.Contains(text, config.Text("flow.cli_default")) {
		t.Fatalf("small empty model should show CLI default: %s", text)
	}
}

func TestRenderFlowChartReviewLoops(t *testing.T) {
	text := newFlowText()
	chartFor := func(modes map[string]string, review bool) flow.Chart {
		cfg := config.DefaultConfig()
		cfg.Rules[config.RuleReview] = review
		cfg.ReviewStages["large"] = modes
		return flow.BuildChart(cfg, "large")
	}
	all := map[string]string{"PMQA": "required", "Security": "required"}
	noSecurity := map[string]string{"PMQA": "required", "Security": "skip"}
	for _, tc := range []struct {
		name    string
		chart   flow.Chart
		width   int
		want    []string
		absent  []string
		returns int
	}{
		{"rail", chartFor(all, true), 120, []string{
			text.execute, text.selfCheck, text.stageNames[flow.StagePrimary], text.stageNames[flow.StageSecurity],
			"PMQA · " + text.required, "Security · " + text.required,
			text.gates[flow.StagePrimary], text.gates[flow.StageSecurity], text.decision, text.fix, text.rereview, text.done,
		}, []string{"↺"}, 2},
		{"compact", chartFor(all, true), 40, []string{
			text.gates[flow.StagePrimary], "▶ " + text.fix, "↺ " + text.stageNames[flow.StagePrimary], text.done,
		}, []string{"◀"}, 0},
		{"security skipped", chartFor(noSecurity, true), 120, []string{text.stageNA, text.gates[flow.StagePrimary]},
			[]string{text.gates[flow.StageSecurity], text.decision}, 1},
		{"review off", chartFor(all, false), 60, []string{text.execute, text.reviewOff, text.done},
			[]string{text.selfCheck, text.stageNames[flow.StagePrimary]}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines := renderFlowChart(tc.chart, tc.width, text)
			joined := strings.Join(lines, "\n")
			for _, line := range lines {
				if displayWidth(line) > tc.width {
					t.Fatalf("line wider than %d: %q", tc.width, line)
				}
			}
			for _, want := range tc.want {
				if !strings.Contains(joined, want) {
					t.Fatalf("missing %q:\n%s", want, joined)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(joined, absent) {
					t.Fatalf("unexpected %q:\n%s", absent, joined)
				}
			}
			if got := strings.Count(joined, "◀"); got != tc.returns {
				t.Fatalf("loop returns=%d want %d:\n%s", got, tc.returns, joined)
			}
		})
	}
}

func TestFlowScopeTabsAndScaleKeys(t *testing.T) {
	_, panel := openPanel(t)
	agent := panel.session.Config.KanbanAgents["large"]
	dir := t.TempDir()
	path := filepath.Join(dir, config.OverlayFilename)
	raw := map[string]any{
		"models": map[string]any{
			"kanban": map[string]any{
				agent: map[string]any{
					"large_model": "project-flow-model",
				},
			},
		},
	}
	if err := panel.session.AttachOverlay(config.ModeGlobal, config.OverlayLocation{ProjectRoot: dir, Path: path}, raw); err != nil {
		t.Fatal(err)
	}
	panel.dispatch(sectionFlow)

	text := flowReportText(panel)
	if strings.Contains(text, "project-flow-model") {
		t.Fatalf("global chart leaked project model: %s", text)
	}
	plain := ansi.Strip(panelView(panel))
	if !strings.Contains(plain, uiText("tui.tab_global")) || !strings.Contains(plain, uiText("tui.tab_project")) {
		t.Fatalf("flow missing Global/Project tabs:\n%s", plain)
	}
	if !strings.Contains(plain, uiText("tui.flow_scale_scroll_esc")) || !strings.Contains(plain, uiText("tui.switch_scope_tabs")) {
		t.Fatalf("flow status bar missing keys:\n%s", plain)
	}
	if !flowHintHasBlankLine(panel, uiText("tui.flow_scale_scroll_esc")) {
		t.Fatal("need a blank line between the chart and the status bar")
	}

	drivePanel(panel, keyMsg("tab"))
	if panel.session.Target != config.TargetOverlay {
		t.Fatalf("tab target=%s", panel.session.Target)
	}
	if panel.flowScale != "large" || panel.report == nil {
		t.Fatalf("tab must keep the flowchart open, scale=%q report=%v", panel.flowScale, panel.report != nil)
	}
	text = flowReportText(panel)
	if !strings.Contains(text, "project-flow-model") {
		t.Fatalf("project chart missing overlay model: %s", text)
	}

	drivePanel(panel, keyMsg("right"))
	if panel.flowScale != "small" {
		t.Fatalf("right scale=%q", panel.flowScale)
	}
	drivePanel(panel, keyMsg("left"))
	if panel.flowScale != "large" {
		t.Fatalf("left scale=%q", panel.flowScale)
	}

	drivePanel(panel, keyMsg("shift-tab"))
	if panel.session.Target != config.TargetScope {
		t.Fatalf("shift-tab target=%s", panel.session.Target)
	}
	if panel.flowScale != "large" {
		t.Fatalf("shift-tab changed scale to %s", panel.flowScale)
	}
	text = flowReportText(panel)
	if strings.Contains(text, "project-flow-model") {
		t.Fatalf("global chart kept project model: %s", text)
	}

	panel.view()
	if len(panel.tabHits) < 2 {
		t.Fatalf("tabHits=%d", len(panel.tabHits))
	}
	hit := panel.tabHits[1]
	x := panel.headerX + (hit.x0+hit.x1)/2
	y := panel.headerY + panel.tabLabelRow
	pumpPanel(panel, panel.HandleMouse(x, y, mouseBtn1Clicked))
	if panel.session.Target != config.TargetOverlay {
		t.Fatalf("click target=%s", panel.session.Target)
	}
	if panel.report == nil || panel.flowScale != "large" {
		t.Fatal("clicking a scope tab closed the flowchart")
	}
}

func TestFlowHintOmitsScopeWhenSingleTab(t *testing.T) {
	_, panel := openPanel(t)
	attachTempOverlay(t, panel.session, config.ModeProject)
	panel.dispatch(sectionFlow)
	plain := ansi.Strip(panelView(panel))
	if strings.Contains(plain, " · "+uiText("tui.switch_scope_tabs")) || strings.Contains(plain, uiText("tui.tab_global")) {
		t.Fatalf("single-tab flow listed Global/Project keys:\n%s", plain)
	}
	if !strings.Contains(plain, uiText("tui.flow_scale_scroll_esc")) {
		t.Fatalf("missing scale keys:\n%s", plain)
	}
	if !flowHintHasBlankLine(panel, uiText("tui.flow_scale_scroll_esc")) {
		t.Fatal("need a blank line between the chart and the status bar")
	}
	drivePanel(panel, keyMsg("tab"))
	if panel.flowScale != "large" {
		t.Fatalf("tab changed scale to %s", panel.flowScale)
	}
	drivePanel(panel, keyMsg("right"))
	if panel.flowScale != "small" {
		t.Fatalf("right scale=%s", panel.flowScale)
	}
}

func flowHintHasBlankLine(panel *optionsPanel, hint string) bool {
	inner := panel.innerWidth()
	_, body := panel.content(themePalette(panel.app.Theme), inner, 24)
	plain := ansi.Strip(body)
	idx := strings.LastIndex(plain, hint)
	if idx < 0 {
		return false
	}
	before := strings.TrimRight(plain[:idx], " ")
	return strings.HasSuffix(before, "\n\n")
}

func flowReportText(panel *optionsPanel) string {
	var text string
	for _, line := range panel.report.lines {
		text += line.Text + "\n"
	}
	return text
}
