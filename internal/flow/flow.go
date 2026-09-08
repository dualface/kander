// Package flow describes configured execution and review assignments without performing actions.
package flow

import "github.com/dualface/kander/internal/config"

// Kind distinguishes section headings, agent/model assignments and short status notes.
type Kind uint8

const (
	Heading Kind = iota
	Assignment
	Note
)

// Line carries a catalog key and template arguments independent of terminal layout.
// An assignment's final argument is its model ID; empty means the agent CLI default.
type Line struct {
	Kind Kind
	Key  string
	Args []any
}

// Build reads the normalized options-session configuration without modifying it.
// Auto roles remain conditional because their applicability depends on the task.
func Build(cfg *config.Config) []Line {
	var lines []Line
	add := func(kind Kind, key string, args ...any) {
		lines = append(lines, Line{Kind: kind, Key: "flow." + key, Args: args})
	}
	add(Heading, "execution")
	for _, scale := range config.TaskScales {
		agent := cfg.KanbanAgents[scale]
		add(Assignment, scale, agent, config.KanbanModelFor(cfg.Models.Kanban[agent], scale))
	}
	add(Heading, "review")
	if !cfg.Rules[config.RuleReview] {
		add(Note, "review_off")
		return lines
	}
	active := false
	for _, scale := range config.TaskScales {
		var scaleLines []Line
		for index, roles := range [][]string{{"PM", "QA"}, {"CSA", "Hacker"}} {
			stageAdded := false
			for _, role := range roles {
				mode, err := config.ReviewStageFor(cfg, scale, role)
				if err != nil || mode == "skip" {
					continue
				}
				if !stageAdded {
					key := "stage_one"
					if index == 1 {
						key = "stage_two"
					}
					scaleLines = append(scaleLines, Line{Kind: Note, Key: "flow." + key})
					stageAdded = true
				}
				agent := cfg.Reviewers[role]
				model, _ := config.ReviewModelFor(cfg, agent, role)
				scaleLines = append(scaleLines, Line{Kind: Assignment, Key: "flow.role_" + mode, Args: []any{role, agent, model}})
			}
		}
		if len(scaleLines) == 0 {
			continue
		}
		add(Note, "review_"+scale)
		lines = append(lines, scaleLines...)
		active = true
	}
	if !active {
		add(Note, "review_none")
	}
	return lines
}
