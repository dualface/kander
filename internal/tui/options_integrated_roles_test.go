package tui

import (
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/flow"
)

func TestExistingOptionsFormsIncludeIntegratedRoles(t *testing.T) {
	_, panel := openPanel(t)
	roles := []string{"PMQA", "Security"}
	pumpPanel(panel, panel.dispatch(sectionReview))
	for _, scale := range []string{"large", "small"} {
		for _, role := range roles {
			if panel.bind.reviewers[reviewerFocusKey(role, scale)] == nil {
				t.Fatalf("missing reviewer %s.%s", scale, role)
			}
		}
	}
	drivePanel(panel, keyMsg("esc"))
	pumpPanel(panel, panel.dispatch(sectionReviewStages))
	for _, scale := range []string{"large", "small"} {
		for _, role := range roles {
			value := panel.bind.stages[stageFocusKey(role, scale)]
			if value == nil || *value != "auto" {
				t.Fatalf("missing stage or unexpected default %s.%s: %v", scale, role, value)
			}
		}
	}
}

func TestExistingFlowRendererSupportsIntegratedRoles(t *testing.T) {
	for _, mode := range []string{"required", "auto", "skip"} {
		cfg := config.DefaultConfig()
		cfg.Rules[config.RuleReview] = true
		cfg.ReviewStages["large"] = map[string]string{"PMQA": "required", "Security": mode}
		chart := flow.BuildChart(cfg, "large")
		if len(chart.Stages) != 2 || chart.Stages[0].Name != flow.StagePrimary || len(chart.Stages[0].Nodes) != 1 || chart.Stages[0].Nodes[0].Role != "PMQA" || chart.Stages[1].Name != flow.StageSecurity {
			t.Fatalf("incorrect integrated stage assignment: %+v", chart.Stages)
		}
		text := newFlowText()
		joined := strings.Join(renderFlowChart(chart, 120, text), "\n")
		if !strings.Contains(joined, "PMQA") {
			t.Fatal("missing integrated primary node", joined)
		}
		if mode == "skip" {
			if len(chart.Stages[1].Nodes) != 0 || !strings.Contains(joined, text.stageNA) || strings.Contains(joined, text.decision) {
				t.Fatal("skipped security stage must remain N/A", joined)
			}
		} else if len(chart.Stages[1].Nodes) != 1 || chart.Stages[1].Nodes[0].Role != "Security" || !strings.Contains(joined, "Security") || !strings.Contains(joined, text.decision) {
			t.Fatal("missing integrated security node or decision", joined)
		}
	}
}
