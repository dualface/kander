package tui

import (
	"encoding/json"
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
	if strings.Contains(text, config.Text("flow.phase_stage_primary")) {
		t.Fatalf("review off must drop the review stages: %s", text)
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

func chartWithRules(t *testing.T, scale string, overrides map[string]bool, stages map[string]string) flow.Chart {
	t.Helper()
	cfg := config.DefaultConfig()
	for name, value := range overrides {
		cfg.Rules[name] = value
	}
	if stages != nil {
		cfg.ReviewStages[scale] = stages
	}
	return flow.BuildChart(cfg, scale)
}

// TestRenderFlowChartEdges covers that every Exit.Target becomes a drawn edge
// when the width allows, and that the compact form names the target instead.
func TestRenderFlowChartEdges(t *testing.T) {
	required := map[string]string{"PMQA": "required", "Security": "required"}
	for _, tc := range []struct {
		name   string
		chart  flow.Chart
		width  int
		want   []string
		absent []string
	}{
		{"wide", chartWithRules(t, "large", nil, required), 100, []string{
			config.Text("flow.phase_implement"), config.Text("flow.phase_stage_primary"),
			config.Text("flow.phase_stage_security"), config.Text("flow.gate_must_fix"),
			config.Text("flow.gate_security_qualified"), config.Text("flow.sec_cross_primary"),
			config.Text("flow.mf_round_cap"), config.Text("flow.reviewer_unavailable"),
			config.Text("flow.mf_attribution"),
			config.Text("flow.phase_unresolved"), config.Text("flow.user_exit"),
			"┌", "└", "▶",
		}, []string{" → "}},
		{"compact", chartWithRules(t, "large", nil, required), 60, []string{
			" → " + config.Text("flow.phase_stage_primary"),
			" → " + config.Text("flow.phase_intake"),
		}, []string{"◀"}},
		{"review off", chartWithRules(t, "large", map[string]bool{
			config.RuleReview: false, config.RuleTaskGroups: false,
		}, nil), 100, []string{
			config.Text("flow.phase_implement"), config.Text("flow.phase_integrate"),
		}, []string{
			config.Text("flow.phase_stage_primary"), config.Text("flow.gate_whitelist"),
			config.Text("flow.phase_self_check"),
		}},
		{"review off keeps the self-check for group members", chartWithRules(t, "large", map[string]bool{
			config.RuleReview: false,
		}, nil), 100, []string{config.Text("flow.phase_self_check")}, nil},
		{"code off", chartWithRules(t, "large", map[string]bool{
			config.RuleCode: false,
		}, required), 100, []string{
			config.Text("flow.phase_delivery_check"),
		}, []string{config.Text("flow.phase_self_check")}},
		{"git off", chartWithRules(t, "large", map[string]bool{
			config.RuleGit: false,
		}, required), 100, []string{
			config.Text("flow.phase_finish_own"), config.Text("flow.sec_decision_stop_delivery"),
		}, []string{config.Text("flow.phase_integrate"), config.Text("flow.phase_wrap_up")}},
		{"security skipped", chartWithRules(t, "large", nil, map[string]string{
			"PMQA": "required", "Security": "skip",
		}), 100, []string{
			config.Text("flow.note_stage_na"), config.Text("flow.note_stage_na_override"),
		}, []string{config.Text("flow.gate_security_qualified"), config.Text("flow.sec_cross_primary")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines := renderFlowChart(tc.chart, tc.width)
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
		})
	}
}

// TestRenderFlowChartCompactNamesEveryTarget covers that the compact form keeps
// one named target per edge the wide form would draw.
func TestRenderFlowChartCompactNamesEveryTarget(t *testing.T) {
	chart := chartWithRules(t, "large", nil, map[string]string{"PMQA": "required", "Security": "required"})
	compact := strings.Join(renderFlowChart(chart, 60), "\n")
	back, forward := countFlowEdges(chart)
	edges := back + forward
	if back == 0 || forward == 0 {
		t.Fatalf("expected both loops and forward jumps, got %d and %d", back, forward)
	}
	if got := strings.Count(compact, " → "); got != edges {
		t.Fatalf("compact named %d targets, wide drew %d", got, edges)
	}
}

// TestRenderFlowChartWidthCeiling covers the diagram width ceiling and that a
// wide terminal never widens the chart past it.
func TestRenderFlowChartWidthCeiling(t *testing.T) {
	chart := chartWithRules(t, "large", nil, map[string]string{"PMQA": "required", "Security": "required"})
	for _, width := range []int{24, 40, 80, 100, 200} {
		limit := width
		if limit > flowMaxWidth {
			limit = flowMaxWidth
		}
		for _, line := range renderFlowChart(chart, width) {
			if displayWidth(line) > limit {
				t.Fatalf("width %d produced %q", width, line)
			}
		}
	}
}

// TestFlowLocaleKeysResolve covers that every key the chart uses exists in all
// three catalogs and that the catalogs carry no unused keys.
func TestFlowLocaleKeysResolve(t *testing.T) {
	used := map[string]bool{
		"flow.title": true, "flow.cli_default": true, "flow.user_exit": true,
	}
	for _, mode := range []string{flow.ModeRequired, flow.ModeAuto, flow.ModeSkip, flow.ModeInvalid} {
		used["flow.mode_"+mode] = true
	}
	for _, combo := range flowRuleCombos() {
		for _, scale := range config.TaskScales {
			for _, stages := range []map[string]string{
				{"PMQA": "required", "Security": "required"},
				{"PMQA": "auto", "Security": "auto"},
				{"PMQA": "skip", "Security": "skip"},
				{"PMQA": "broken", "Security": "broken"},
			} {
				chart := chartWithRules(t, scale, combo, stages)
				for _, phase := range chart.Phases {
					used["flow.phase_"+phase.Key] = true
					for _, note := range phase.Notes {
						used["flow."+note] = true
					}
					for _, gate := range phase.Gates {
						used["flow."+gate.Key] = true
						for _, exit := range gate.Exits {
							used["flow."+exit.Key] = true
							for _, step := range exit.Steps {
								used["flow."+step] = true
							}
						}
					}
				}
			}
		}
	}
	for _, lang := range []string{"en", "zh-CN", "ja"} {
		raw, err := os.ReadFile(filepath.Join("..", "i18n", "locales", "flow", lang+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var catalog map[string]string
		if err := json.Unmarshal(raw, &catalog); err != nil {
			t.Fatal(err)
		}
		for key := range used {
			value, ok := catalog[key]
			if !ok || strings.TrimSpace(value) == "" {
				t.Fatalf("%s: missing %q", lang, key)
			}
		}
		for key := range catalog {
			if !used[key] {
				t.Fatalf("%s: unused %q", lang, key)
			}
		}
	}
}

// flowRuleCombos returns every switch combination that changes the chart.
func flowRuleCombos() []map[string]bool {
	names := []string{config.RuleTaskIntake, config.RuleCode, config.RuleGit, config.RuleReview, config.RuleTaskGroups, config.RuleReporting}
	var out []map[string]bool
	for mask := 0; mask < 1<<len(names); mask++ {
		combo := map[string]bool{}
		for i, name := range names {
			combo[name] = mask&(1<<i) != 0
		}
		out = append(out, combo)
	}
	return out
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
