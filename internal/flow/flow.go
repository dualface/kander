// Package flow describes configured workflows without performing workflow actions.
package flow

import "github.com/dualface/kander/internal/config"

// Kind distinguishes headings, summary facts, steps and conditional step details.
type Kind uint8

const (
	Heading Kind = iota
	Summary
	Step
	Detail
)

// Line carries a catalog key and template arguments, independent of terminal layout.
type Line struct {
	Kind Kind
	Key  string
	Args []any
}

// Build reads a normalized options-session configuration without modifying it.
// Project overrides and task-specific triggers are resolved by agents at execution time.
func Build(cfg *config.Config) []Line {
	var lines []Line
	add := func(kind Kind, key string, args ...any) {
		lines = append(lines, Line{Kind: kind, Key: "flow." + key, Args: args})
	}
	add(Heading, "configuration")
	add(Summary, "scope")
	for _, scale := range config.TaskScales {
		add(Summary, "agent", scale, cfg.KanbanAgents[scale])
	}
	add(Summary, "launcher", cfg.Launcher)
	for _, role := range config.ReviewRoles {
		add(Summary, "reviewer", role, cfg.Reviewers[role], cfg.ReviewStages[role])
	}
	for _, module := range config.RuleModules {
		key := "module_off"
		if cfg.Rules[module] {
			key = "module_on"
		}
		add(Summary, key, module)
	}
	start := func() {
		add(Step, "start")
		add(Detail, "launch_"+cfg.Launcher)
		add(Detail, "launch_failure")
		add(Step, "read")
		if cfg.Rules[config.RuleCollaboration] {
			add(Step, "collaboration")
		}
	}
	implement := func() {
		add(Step, "implement")
		if cfg.Rules[config.RuleCode] {
			add(Step, "self_check")
		}
	}
	review := func(group bool) {
		add(Step, "plan")
		if !cfg.Rules[config.RuleReview] {
			return
		}
		add(Step, "review_trigger")
		active := false
		for _, role := range config.ReviewRoles {
			active = active || cfg.ReviewStages[role] != "skip"
		}
		if !active {
			return
		}
		if group {
			add(Step, "batch")
		}
		for _, stage := range [][]string{{"PM", "QA"}, {"CSA", "Hacker"}} {
			var roles []string
			for _, role := range stage {
				if cfg.ReviewStages[role] != "skip" {
					roles = append(roles, role)
				}
			}
			if len(roles) == 0 {
				continue
			}
			key := "stage_one"
			if stage[0] == "CSA" {
				key = "stage_two"
			}
			add(Step, key)
			for _, role := range roles {
				add(Detail, "role_"+cfg.ReviewStages[role], role, cfg.Reviewers[role])
			}
		}
		if group {
			add(Step, "dispatch_fix")
			add(Step, "member_fix")
		} else {
			add(Step, "single_fix")
		}
		add(Step, "review_finish")
	}
	report := func() {
		if cfg.Rules[config.RuleReporting] {
			add(Step, "report")
		}
	}
	add(Heading, "single")
	if cfg.Rules[config.RuleTaskIntake] {
		add(Step, "intake")
	}
	add(Step, "card")
	add(Step, "todo")
	start()
	if cfg.Rules[config.RuleGit] {
		add(Step, "task_branch")
	}
	implement()
	if cfg.Rules[config.RuleGit] {
		add(Step, "commit")
	}
	review(false)
	add(Step, "close_plan")
	if cfg.Rules[config.RuleGit] {
		add(Step, "single_authority")
		add(Step, "integrate")
		add(Step, "task_cleanup")
	}
	add(Step, "records")
	report()
	add(Step, "single_done")
	add(Heading, "group")
	if !cfg.Rules[config.RuleTaskGroups] || !cfg.Rules[config.RuleGit] {
		add(Summary, "group_unavailable")
		return lines
	}
	add(Step, "group_plan")
	add(Step, "group_cards")
	add(Step, "dependencies")
	add(Step, "group_branch")
	add(Step, "schedule")
	start()
	add(Step, "subscribe")
	add(Step, "member_branch")
	implement()
	add(Step, "member_delivery")
	add(Step, "receive")
	add(Detail, "sync_dispatch")
	review(true)
	add(Step, "close_plan")
	add(Step, "group_ready")
	add(Step, "integrate")
	add(Step, "wrap_dispatch")
	add(Step, "task_cleanup")
	add(Step, "records")
	report()
	add(Step, "member_done")
	add(Step, "group_cleanup")
	add(Detail, "dismiss")
	return lines
}
