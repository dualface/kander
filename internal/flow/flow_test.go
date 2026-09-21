package flow

import (
	"reflect"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestBuildChartPerScale(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.KanbanAgents["large"], cfg.KanbanAgents["small"] = "claude", "codex"
	cfg.Models.Kanban["claude"] = map[string]string{
		"large_model": "large-model", "large_effort": "high", "model": "legacy-large",
	}
	cfg.Models.Kanban["codex"] = map[string]string{
		"small_model": "small-model", "small_effort": "medium", "model": "legacy-small",
	}
	cfg.Reviewers["large"]["PMQA"], cfg.Reviewers["large"]["Security"] = "claude", "codex"
	cfg.Reviewers["small"]["PMQA"], cfg.Reviewers["small"]["Security"] = "claude", "codex"
	cfg.Models.ReviewRoles["PMQA"] = map[string]string{"large_model": "pm-large", "large_effort": "xhigh", "model": "pm-model"}
	cfg.Models.ReviewRoles["Security"] = map[string]string{}
	cfg.Models.Review["codex"] = map[string]string{"model": "qa-fallback", "effort": "low"}
	cfg.ReviewStages = map[string]map[string]string{
		"large": {"PMQA": "required", "Security": "skip"},
		"small": {"PMQA": "required", "Security": "skip"},
	}
	before := config.Clone(cfg)

	large := BuildChart(cfg, "large")
	if large.Scale != "large" || large.ReviewDisabled || !large.DeliveryFull || !large.Integrate || !large.GroupBatches {
		t.Fatalf("%+v", large)
	}
	if large.Execution != (Node{Model: "large-model", Effort: "high"}) {
		t.Fatalf("large execution=%+v", large.Execution)
	}
	if len(large.Stages) != 2 || large.Stages[0].Name != StagePrimary || len(large.Stages[0].Nodes) != 1 ||
		large.Stages[1].Name != StageSecurity || len(large.Stages[1].Nodes) != 0 {
		t.Fatalf("large stages=%+v", large.Stages)
	}
	if large.Stages[0].Nodes[0] != (Node{Role: "PMQA", Mode: "required", Model: "pm-large", Effort: "xhigh"}) {
		t.Fatalf("PMQA=%+v", large.Stages[0].Nodes[0])
	}

	small := BuildChart(cfg, "small")
	if small.Execution != (Node{Model: "small-model", Effort: "medium"}) {
		t.Fatalf("small execution=%+v", small.Execution)
	}
	if small.Stages[0].Nodes[0].Model != "pm-model" {
		t.Fatalf("small PMQA falls back to shared model: %+v", small.Stages[0].Nodes[0])
	}

	if !reflect.DeepEqual(cfg, before) {
		t.Fatal("BuildChart mutated configuration")
	}
	charts := BuildCharts(cfg)
	if len(charts) != 2 || charts[0].Scale != "large" || charts[1].Scale != "small" {
		t.Fatalf("%+v", charts)
	}
}

func TestBuildChartReviewModes(t *testing.T) {
	same := func(modes map[string]string) map[string]map[string]string {
		return map[string]map[string]string{"large": modes, "small": modes}
	}
	for _, tc := range []struct {
		name     string
		enabled  bool
		modes    map[string]map[string]string
		disabled bool
		roles    [2][]string
	}{
		{"disabled", false, config.DefaultReviewStages(), true, [2][]string{}},
		{"all skipped", true, same(map[string]string{"PMQA": "skip", "Security": "skip"}), false, [2][]string{nil, nil}},
		{"both stages", true, same(map[string]string{"PMQA": "auto", "Security": "required"}), false, [2][]string{{"PMQA"}, {"Security"}}},
		{"second only", true, same(map[string]string{"PMQA": "skip", "Security": "required"}), false, [2][]string{nil, {"Security"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Rules[config.RuleReview], cfg.ReviewStages = tc.enabled, tc.modes
			got := BuildChart(cfg, "large")
			if got.ReviewDisabled != tc.disabled {
				t.Fatalf("disabled=%v", got.ReviewDisabled)
			}
			if !got.DeliveryFull || !got.Integrate || !got.GroupBatches {
				t.Fatalf("module flags=%+v", got)
			}
			if tc.disabled {
				if len(got.Stages) != 0 {
					t.Fatalf("disabled review must have no stages: %+v", got.Stages)
				}
				return
			}
			// Both stages stay in place so a skipped stage one never shifts stage two.
			if len(got.Stages) != 2 || got.Stages[0].Name != StagePrimary || got.Stages[1].Name != StageSecurity {
				t.Fatalf("stages=%+v", got.Stages)
			}
			for i, stage := range got.Stages {
				var roles []string
				for _, node := range stage.Nodes {
					roles = append(roles, node.Role)
				}
				if !reflect.DeepEqual(roles, tc.roles[i]) {
					t.Fatalf("stage %d roles=%v want %v", i, roles, tc.roles[i])
				}
			}
		})
	}
}

func TestModuleSwitchesChangeOnlyTheirChartFlags(t *testing.T) {
	cfg := config.DefaultConfig()
	want := BuildChart(cfg, "large")
	if !want.DeliveryFull || !want.Integrate || !want.GroupBatches {
		t.Fatalf("%+v", want)
	}
	for _, module := range []string{config.RuleCollaboration, config.RuleReporting, config.RuleTaskIntake} {
		cfg.Rules[module] = false
	}
	if got := BuildChart(cfg, "large"); !reflect.DeepEqual(got, want) {
		t.Fatal("unrelated module switches changed chart")
	}
	cfg.Rules[config.RuleCode] = false
	got := BuildChart(cfg, "large")
	if got.DeliveryFull || !got.Integrate || !got.GroupBatches {
		t.Fatalf("code off: %+v", got)
	}
	cfg.Rules[config.RuleGit] = false
	cfg.Rules[config.RuleTaskGroups] = true
	got = BuildChart(cfg, "large")
	if got.Integrate || got.GroupBatches {
		t.Fatalf("git off: %+v", got)
	}
}

func TestReviewChartHidesEffortTheReviewLaunchOmits(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Reviewers["large"]["PMQA"] = "cursor"
	cfg.ReviewStages["large"]["PMQA"] = "required"
	cfg.ReviewStages["large"]["Security"] = "skip"
	cfg.Models.ReviewRoles["PMQA"]["large_model"] = "cursor-model"
	cfg.Models.ReviewRoles["PMQA"]["large_effort"] = "high"
	cfg.Models.ReviewRoles["PMQA"]["large_agent"] = "cursor"
	cfg.Agents = map[string]config.AgentDefinition{
		"cursor": {Args: &config.AgentArgs{Start: []string{"{model}", "{effort}"}}},
	}
	if !config.AgentSupportsEffort(cfg, "cursor") || config.ReviewModelSupportsEffort(cfg, "cursor") {
		t.Fatal("fixture does not separate execution effort from review effort")
	}
	got := BuildChart(cfg, "large")
	node := got.Stages[0].Nodes[0]
	if node.Model != "cursor-model" || node.Effort != "" {
		t.Fatalf("PMQA=%+v", node)
	}
}

func TestEmptyModelsRetainCLIDefaultSemantics(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Models.Kanban = map[string]map[string]string{}
	cfg.Models.Review = map[string]map[string]string{}
	cfg.Models.ReviewRoles = map[string]map[string]string{}
	cfg.ReviewStages["large"]["PMQA"] = "required"
	cfg.ReviewStages["large"]["Security"] = "skip"
	got := BuildChart(cfg, "large")
	if got.Execution.Model != "" || got.Execution.Effort != "" {
		t.Fatalf("execution=%+v", got.Execution)
	}
	if len(got.Stages) != 2 || len(got.Stages[0].Nodes) != 1 || got.Stages[0].Nodes[0].Model != "" {
		t.Fatalf("review=%+v", got.Stages)
	}
}
