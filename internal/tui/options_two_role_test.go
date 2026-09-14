package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/flow"
)

func TestOptionsReviewFieldSetOnlyQAAndSecurity(t *testing.T) {
	_, panel := openPanel(t)
	for _, section := range []string{sectionReview, sectionReviewStages} {
		pumpPanel(panel, panel.dispatch(section))
		plain := ansi.Strip(panel.form.View())
		for _, role := range config.ReviewRoles {
			if !strings.Contains(plain, role) {
				t.Fatalf("%s missing %s:\n%s", section, role, plain)
			}
		}
		for _, legacy := range []string{"PM", "CSA", "Hacker"} {
			if strings.Contains(plain, legacy) {
				t.Fatalf("%s still shows %s:\n%s", section, legacy, plain)
			}
		}
		if got := len(panel.bind.reviewers); section == sectionReview && got != len(config.TaskScales)*len(config.ReviewRoles) {
			t.Fatalf("reviewer fields=%d want %d", got, len(config.TaskScales)*len(config.ReviewRoles))
		}
		if got := len(panel.bind.stages); section == sectionReviewStages && got != len(config.TaskScales)*len(config.ReviewRoles) {
			t.Fatalf("stage fields=%d want %d", got, len(config.TaskScales)*len(config.ReviewRoles))
		}
		drivePanel(panel, keyMsg("esc"))
	}
}

func TestFlowChartTwoRoleSingleNodes(t *testing.T) {
	for _, mode := range []string{"required", "auto", "skip"} {
		cfg := config.DefaultConfig()
		cfg.Rules[config.RuleReview] = true
		cfg.ReviewStages["large"] = map[string]string{"QA": "required", "Security": mode}
		chart := flow.BuildChart(cfg, "large")
		if len(chart.Stages) != 2 {
			t.Fatalf("mode %s stages=%d", mode, len(chart.Stages))
		}
		if chart.Stages[0].Name != flow.StagePrimary || len(chart.Stages[0].Nodes) != 1 || chart.Stages[0].Nodes[0].Role != "QA" {
			t.Fatalf("mode %s primary=%+v", mode, chart.Stages[0])
		}
		security := chart.Stages[1]
		if security.Name != flow.StageSecurity {
			t.Fatalf("mode %s security name=%s", mode, security.Name)
		}
		if mode == "skip" {
			if len(security.Nodes) != 0 {
				t.Fatalf("skip must be N/A: %+v", security)
			}
		} else if len(security.Nodes) != 1 || security.Nodes[0].Role != "Security" || security.Nodes[0].Mode != mode {
			t.Fatalf("mode %s security=%+v", mode, security)
		}
		text := newFlowText()
		joined := strings.Join(renderFlowChart(chart, 120, text), "\n")
		if !strings.Contains(joined, "QA") {
			t.Fatalf("mode %s missing QA:\n%s", mode, joined)
		}
		if mode == "skip" {
			if !strings.Contains(joined, text.stageNA) || strings.Contains(joined, text.decision) {
				t.Fatalf("skip should show N/A without user decision:\n%s", joined)
			}
		} else if !strings.Contains(joined, "Security") || !strings.Contains(joined, text.decision) {
			t.Fatalf("mode %s missing Security/user decision:\n%s", mode, joined)
		}
	}
}

func TestOptionsLegacyReviewMigrationHintGlobalAndProject(t *testing.T) {
	_, panel := openPanel(t)
	scope, err := config.DocumentFromConfig(panel.session.Config)
	if err != nil {
		t.Fatal(err)
	}
	scope["reviewers"] = map[string]any{
		"large": map[string]any{"CSA": "cursor", "Hacker": "codex", "QA": "claude", "Security": "claude"},
		"small": map[string]any{"QA": "claude", "Security": "claude"},
	}
	scope["review_stages"] = map[string]any{
		"large": map[string]any{"QA": "required", "Security": "auto"},
		"small": map[string]any{"PM": "auto", "QA": "auto", "Security": "skip"},
	}
	panel.session.SeedScopeRawForTest(scope)

	pumpPanel(panel, panel.dispatch(sectionReview))
	hint := panel.session.LegacyReviewHint()
	for _, want := range []string{"reviewers.large.CSA", "review_stages.small.PM", "doctor"} {
		if !strings.Contains(strings.ToLower(hint), strings.ToLower(want)) && !strings.Contains(hint, want) {
			t.Fatalf("session hint missing %s: %q", want, hint)
		}
	}
	plain := ansi.Strip(panelView(panel))
	// Chrome clips long hints; assert the visible migration banner is present.
	if !strings.Contains(plain, "Legacy review keys") && !strings.Contains(plain, "旧审核键") {
		t.Fatalf("global chrome missing migration banner:\n%s", plain)
	}
	if strings.Contains(plain, "\n  PM:") || strings.Contains(plain, "\n┃ PM:") {
		t.Fatalf("global view shows a PM role field:\n%s", plain)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, config.OverlayFilename)
	overlay := map[string]any{
		"reviewers": map[string]any{
			"large": map[string]any{"CSA": "grok"},
		},
	}
	if err := panel.session.AttachOverlay(config.ModeGlobal, config.OverlayLocation{ProjectRoot: dir, Path: path}, overlay); err != nil {
		t.Fatal(err)
	}
	drivePanel(panel, keyMsg("tab"))
	if panel.session.Target != config.TargetOverlay {
		t.Fatalf("target=%s", panel.session.Target)
	}
	if panel.session.Config.Reviewers["large"]["Security"] != "grok" {
		t.Fatalf("project mapped=%v", panel.session.Config.Reviewers["large"])
	}
	plain = ansi.Strip(panelView(panel))
	if !strings.Contains(plain, "Legacy review keys") && !strings.Contains(plain, "旧审核键") {
		t.Fatalf("project chrome missing migration banner:\n%s", plain)
	}
	if !strings.Contains(panel.session.LegacyReviewHint(), "reviewers.large.CSA") {
		t.Fatalf("project hint=%q", panel.session.LegacyReviewHint())
	}
	drivePanel(panel, keyMsg("shift-tab"))
	if panel.session.Target != config.TargetScope {
		t.Fatalf("target=%s", panel.session.Target)
	}
	if !strings.Contains(panel.session.LegacyReviewHint(), "review_stages.small.PM") {
		t.Fatalf("global hint lost after tab switch: %q", panel.session.LegacyReviewHint())
	}
	plain = ansi.Strip(panelView(panel))
	if !strings.Contains(plain, "Legacy review keys") && !strings.Contains(plain, "旧审核键") {
		t.Fatalf("global chrome lost after tab switch:\n%s", plain)
	}
}

func TestThemeLiveWalkthroughHasNoLegacyReviewRoles(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "theme-live-walkthrough.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, legacy := range []string{"PM", "CSA", "Hacker"} {
		if strings.Contains(text, legacy) {
			t.Fatalf("walkthrough still mentions %s", legacy)
		}
	}
}
