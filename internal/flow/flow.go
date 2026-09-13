// Package flow describes configured execution and review assignments without performing actions.
package flow

import "github.com/dualface/kander/internal/config"

// Review stage names, in the order the review rules run them.
const (
	StagePrimary  = "primary"  // PM and QA in parallel
	StageSecurity = "security" // CSA and Hacker after stage one passes
)

// Node is one process block. Role and Mode are empty for the execution node;
// Mode is the review stage policy ("required" or "auto") for review roles.
type Node struct {
	Role   string
	Mode   string
	Model  string
	Effort string
}

// Stage is one review stage and its non-skipped roles, which run in parallel.
// An empty Nodes slice means every role of the stage is skipped (N/A).
type Stage struct {
	Name  string
	Nodes []Node
}

// Chart is the flowchart for one task scale (large or small).
type Chart struct {
	Scale          string
	Execution      Node
	ReviewDisabled bool
	// Stages holds both review stages in order (primary, then security) when
	// review is enabled, and is empty when review is off.
	Stages []Stage
}

// BuildChart reads the options-session configuration for one task scale.
func BuildChart(cfg *config.Config, scale string) Chart {
	chart := Chart{
		Scale:     scale,
		Execution: executionNode(cfg, scale),
	}
	if !cfg.Rules[config.RuleReview] {
		chart.ReviewDisabled = true
		return chart
	}
	for _, stage := range []struct {
		name  string
		roles []string
	}{{StagePrimary, []string{"PM", "QA"}}, {StageSecurity, []string{"CSA", "Hacker"}}} {
		built := Stage{Name: stage.name}
		for _, role := range stage.roles {
			mode, err := config.ReviewStageFor(cfg, scale, role)
			if err != nil || mode == "skip" {
				continue
			}
			node := reviewNode(cfg, scale, role)
			node.Mode = mode
			built.Nodes = append(built.Nodes, node)
		}
		chart.Stages = append(chart.Stages, built)
	}
	return chart
}

// BuildCharts returns large then small charts.
func BuildCharts(cfg *config.Config) []Chart {
	out := make([]Chart, 0, len(config.TaskScales))
	for _, scale := range config.TaskScales {
		out = append(out, BuildChart(cfg, scale))
	}
	return out
}

func executionNode(cfg *config.Config, scale string) Node {
	agent := cfg.KanbanAgents[scale]
	entry := cfg.Models.Kanban[agent]
	model := config.KanbanModelFor(entry, scale)
	effort := ""
	if config.AgentSupportsEffort(cfg, agent) {
		effort = entry[scale+"_effort"]
	}
	return Node{Model: model, Effort: effort}
}

func reviewNode(cfg *config.Config, scale, role string) Node {
	agent := config.ReviewerFor(cfg, scale, role)
	model, effort := config.ReviewModelFor(cfg, agent, role, scale)
	if !config.AgentSupportsEffort(cfg, agent) {
		effort = ""
	}
	return Node{Role: role, Model: model, Effort: effort}
}
