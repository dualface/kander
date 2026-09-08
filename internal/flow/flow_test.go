package flow

import (
	"reflect"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/i18n"
)

func TestEffectiveAssignments(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.KanbanAgents["large"], cfg.KanbanAgents["small"] = "claude", "codex"
	cfg.Models.Kanban["claude"] = map[string]string{"large_model": "large-model", "model": "legacy-large"}
	cfg.Models.Kanban["codex"] = map[string]string{"model": "legacy-small"}
	cfg.Reviewers["PM"], cfg.Reviewers["QA"] = "claude", "codex"
	cfg.Models.ReviewRoles["PM"] = map[string]string{"model": "pm-model"}
	cfg.Models.ReviewRoles["QA"] = map[string]string{}
	cfg.Models.Review["codex"] = map[string]string{"model": "qa-fallback"}
	cfg.ReviewStages = map[string]map[string]string{
		"large": {"PM": "required", "QA": "auto", "CSA": "skip", "Hacker": "skip"},
		"small": {"PM": "required", "QA": "auto", "CSA": "skip", "Hacker": "skip"},
	}
	before := config.Clone(cfg)
	want := []Line{
		{Kind: Heading, Key: "flow.execution"},
		{Kind: Assignment, Key: "flow.large", Args: []any{"claude", "large-model"}},
		{Kind: Assignment, Key: "flow.small", Args: []any{"codex", "legacy-small"}},
		{Kind: Heading, Key: "flow.review"},
		{Kind: Note, Key: "flow.review_large"},
		{Kind: Note, Key: "flow.stage_one"},
		{Kind: Assignment, Key: "flow.role_required", Args: []any{"PM", "claude", "pm-model"}},
		{Kind: Assignment, Key: "flow.role_auto", Args: []any{"QA", "codex", "qa-fallback"}},
		{Kind: Note, Key: "flow.review_small"},
		{Kind: Note, Key: "flow.stage_one"},
		{Kind: Assignment, Key: "flow.role_required", Args: []any{"PM", "claude", "pm-model"}},
		{Kind: Assignment, Key: "flow.role_auto", Args: []any{"QA", "codex", "qa-fallback"}},
	}
	if got := Build(cfg); !reflect.DeepEqual(got, want) {
		t.Fatalf("assignments = %+v, want %+v", got, want)
	}
	if !reflect.DeepEqual(cfg, before) {
		t.Fatal("summary mutated configuration")
	}
	cfg.KanbanAgents["small"] = "claude"
	cfg.Models.Kanban["claude"]["small_model"] = "edited-model"
	if got := Build(cfg)[2].Args; !reflect.DeepEqual(got, []any{"claude", "edited-model"}) {
		t.Fatalf("summary missed current configuration: %v", got)
	}
}

func TestReviewSelection(t *testing.T) {
	same := func(modes map[string]string) map[string]map[string]string {
		return map[string]map[string]string{"large": modes, "small": modes}
	}
	bothStages := []string{
		"flow.review_large", "flow.stage_one", "flow.role_auto", "flow.stage_two", "flow.role_required",
		"flow.review_small", "flow.stage_one", "flow.role_auto", "flow.stage_two", "flow.role_required",
	}
	secondOnly := []string{
		"flow.review_large", "flow.stage_two", "flow.role_auto", "flow.role_required",
		"flow.review_small", "flow.stage_two", "flow.role_auto", "flow.role_required",
	}
	for _, tc := range []struct {
		name    string
		enabled bool
		modes   map[string]map[string]string
		keys    []string
	}{
		{"disabled", false, config.DefaultReviewStages(), []string{"flow.review_off"}},
		{"all skipped", true, same(map[string]string{"PM": "skip", "QA": "skip", "CSA": "skip", "Hacker": "skip"}), []string{"flow.review_none"}},
		{"both stages", true, same(map[string]string{"PM": "skip", "QA": "auto", "CSA": "required", "Hacker": "skip"}), bothStages},
		{"second only", true, same(map[string]string{"PM": "skip", "QA": "skip", "CSA": "auto", "Hacker": "required"}), secondOnly},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Rules[config.RuleReview], cfg.ReviewStages = tc.enabled, tc.modes
			lines := Build(cfg)
			var got []string
			for _, line := range lines[4:] {
				got = append(got, line.Key)
			}
			if !reflect.DeepEqual(got, tc.keys) {
				t.Fatalf("review rows=%v, want %v", got, tc.keys)
			}
		})
	}
}

func TestUnrelatedModulesDoNotExpandSummary(t *testing.T) {
	cfg := config.DefaultConfig()
	want := Build(cfg)
	for _, module := range config.RuleModules {
		if module != config.RuleReview {
			cfg.Rules[module] = false
		}
	}
	if got := Build(cfg); !reflect.DeepEqual(got, want) {
		t.Fatal("unrelated module switches changed assignment list")
	}
}

func TestAssignmentCatalogsAndEmptyModels(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Models.Kanban = map[string]map[string]string{}
	cfg.Models.Review = map[string]map[string]string{}
	cfg.Models.ReviewRoles = map[string]map[string]string{}
	cfg.ReviewStages["large"]["PM"] = "required"
	cfg.ReviewStages["small"]["PM"] = "required"
	for _, line := range Build(cfg) {
		for _, lang := range []string{"cn", "en", "ja"} {
			if got := i18n.Text(lang, line.Key, line.Args...); got == line.Key {
				t.Errorf("missing %s/%s", lang, line.Key)
			}
		}
		if line.Kind == Assignment && line.Args[len(line.Args)-1] != "" {
			t.Fatal("empty model must retain CLI default semantics")
		}
	}
}
