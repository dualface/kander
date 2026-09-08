package flow

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/i18n"
)

func keys(lines []Line) map[string]bool {
	out := map[string]bool{}
	for _, line := range lines {
		out[line.Key] = true
	}
	return out
}

func TestModulePruning(t *testing.T) {
	for _, disabled := range append([]string{"", "all"}, config.RuleModules...) {
		t.Run(disabled, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Rules = config.DefaultRules(disabled != "all")
			if disabled != "" && disabled != "all" {
				cfg.Rules[disabled] = false
			}
			before := config.Clone(cfg)
			lines := Build(cfg)
			got := keys(lines)
			if !reflect.DeepEqual(cfg, before) {
				t.Fatal("workflow mutated its input")
			}
			for module, key := range map[string]string{
				config.RuleCode: "self_check", config.RuleCollaboration: "collaboration",
				config.RuleTaskIntake: "intake", config.RuleReview: "review_trigger",
				config.RuleGit: "integrate", config.RuleReporting: "report",
			} {
				if got["flow."+key] != cfg.Rules[module] {
					t.Errorf("%s presence does not match %s", key, module)
				}
			}
			group := cfg.Rules[config.RuleTaskGroups] && cfg.Rules[config.RuleGit]
			if got["flow.group_branch"] != group || got["flow.group_unavailable"] == group {
				t.Fatal("group dependency pruning failed")
			}
			for _, key := range []string{"card", "todo", "start", "read", "implement", "plan", "close_plan", "records", "single_done"} {
				if !got["flow."+key] {
					t.Errorf("missing mandatory command protocol: %s", key)
				}
			}
			if !cfg.Rules[config.RuleReview] {
				for _, key := range []string{"stage_one", "stage_two", "role_auto", "role_required", "dispatch_fix", "single_fix", "review_finish", "batch"} {
					if got["flow."+key] {
						t.Errorf("disabled review node: %s", key)
					}
				}
			}
			if !cfg.Rules[config.RuleGit] && (got["flow.task_branch"] || got["flow.task_cleanup"] || got["flow.commit"]) {
				t.Fatal("disabled Git nodes remain")
			}
			modules := 0
			for _, line := range lines {
				if line.Key == "flow.module_on" || line.Key == "flow.module_off" {
					modules++
				}
			}
			if modules != 7 {
				t.Fatalf("summary has %d modules", modules)
			}
		})
	}
}

func TestReviewRolesAndStageOrder(t *testing.T) {
	for shift := 0; shift < 3; shift++ {
		cfg := config.DefaultConfig()
		for i, role := range config.ReviewRoles {
			cfg.ReviewStages[role] = []string{"required", "auto", "skip"}[(i+shift)%3]
			cfg.Reviewers[role] = "reviewer-" + role
		}
		seen := map[string]int{}
		stage := ""
		for _, line := range Build(cfg) {
			if line.Key == "flow.stage_one" || line.Key == "flow.stage_two" {
				stage = line.Key
			}
			if line.Key != "flow.role_auto" && line.Key != "flow.role_required" {
				continue
			}
			role := line.Args[0].(string)
			seen[role]++
			if line.Key != "flow.role_"+cfg.ReviewStages[role] || line.Args[1] != cfg.Reviewers[role] {
				t.Fatalf("wrong policy/reviewer: %+v", line)
			}
			want := "flow.stage_one"
			if role == "CSA" || role == "Hacker" {
				want = "flow.stage_two"
			}
			if stage != want {
				t.Fatalf("%s in %s", role, stage)
			}
		}
		for _, role := range config.ReviewRoles {
			want := 2
			if cfg.ReviewStages[role] == "skip" {
				want = 0
			}
			if seen[role] != want {
				t.Errorf("%s/%s count %d, want %d", role, cfg.ReviewStages[role], seen[role], want)
			}
		}
	}
}

func TestWorkflowOrderAndTranslations(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.ReviewStages["PM"] = "required"
	lines := Build(cfg)
	positions := map[string]int{}
	steps := 0
	for index, line := range lines {
		positions[line.Key] = index
		if line.Kind == Heading {
			if steps > 40 {
				t.Fatalf("workflow has %d steps", steps)
			}
			steps = 0
		}
		if line.Kind == Step {
			steps++
		}
	}
	for _, pair := range [][2]string{{"member_delivery", "batch"}, {"batch", "receive"}, {"receive", "dispatch_fix"}, {"dispatch_fix", "member_fix"}, {"member_fix", "close_plan"}, {"close_plan", "group_ready"}, {"group_ready", "integrate"}, {"integrate", "wrap_dispatch"}, {"wrap_dispatch", "task_cleanup"}, {"task_cleanup", "member_done"}, {"member_done", "group_cleanup"}} {
		if positions["flow."+pair[0]] >= positions["flow."+pair[1]] {
			t.Errorf("wrong order: %v", pair)
		}
	}
	if steps > 40 {
		t.Fatalf("group has %d steps", steps)
	}
	for _, launcher := range []string{"auto", "tmux", "tmux-session", "herdr", "foreground", "console"} {
		cfg.Launcher = launcher
		for _, line := range Build(cfg) {
			for _, lang := range []string{"en", "cn", "ja"} {
				text := i18n.Text(lang, line.Key, line.Args...)
				if text == line.Key || strings.Contains(text, "{{") {
					t.Errorf("untranslated %s/%s", lang, line.Key)
				}
			}
		}
	}
}

func TestSkippedStagesAndConfiguredSummary(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.KanbanAgents["large"], cfg.KanbanAgents["small"] = "large-agent", "small-agent"
	cfg.Launcher = "tmux-session"
	for _, role := range config.ReviewRoles {
		cfg.ReviewStages[role] = "skip"
	}
	lines := Build(cfg)
	got := keys(lines)
	for _, key := range []string{"stage_one", "stage_two", "single_fix", "dispatch_fix", "review_finish"} {
		if got["flow."+key] {
			t.Errorf("all-skip config contains %s", key)
		}
	}
	agents, reviewers := 0, 0
	for _, line := range lines {
		switch line.Key {
		case "flow.agent":
			agents++
			scale := line.Args[0].(string)
			if line.Args[1] != cfg.KanbanAgents[scale] {
				t.Fatal("summary lost scale agent")
			}
		case "flow.reviewer":
			reviewers++
			if line.Args[2] != "skip" {
				t.Fatal("summary must retain skipped roles")
			}
		}
	}
	if agents != 2 || reviewers != 4 || !got["flow.launch_tmux-session"] {
		t.Fatal("incomplete configured summary or launcher")
	}
}
