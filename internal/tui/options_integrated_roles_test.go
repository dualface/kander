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
		primary, ok := chart.Phase(flow.PhaseStagePrimary)
		if !ok || primary.Node.Role != "PMQA" || primary.Node.Mode != flow.ModeRequired {
			t.Fatalf("incorrect primary stage: %+v", primary)
		}
		security, ok := chart.Phase(flow.PhaseStageSecurity)
		if !ok || security.Node.Role != "Security" || security.Node.Mode != mode {
			t.Fatalf("incorrect security stage: %+v", security)
		}
		joined := strings.Join(renderFlowChart(chart, 100), "\n")
		if !strings.Contains(joined, "PMQA") || !strings.Contains(joined, "Security") {
			t.Fatal("missing an integrated role", joined)
		}
		hasDecision := strings.Contains(joined, config.Text("flow.gate_security_qualified"))
		if mode == "skip" {
			if len(security.Gates) != 0 || !strings.Contains(joined, config.Text("flow.note_stage_na")) || hasDecision {
				t.Fatal("skipped security stage must remain N/A", joined)
			}
			continue
		}
		if !hasDecision {
			t.Fatal("missing the security decision gate", joined)
		}
	}
}
