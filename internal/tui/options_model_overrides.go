package tui

import (
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/menu"
)

// Model edits may pin the agent and the paired field, even when the edited
// field was already overridden. Rebuild all affected inheritance labels.
func (p *optionsPanel) modelSelectionOverrides(field menu.ModelField) [3]bool {
	path := modelOverlayPath(field)
	if field.Kind() == "chat" {
		return [3]bool{
			p.overridePresence("chat_agent"),
			p.overridePresence("models", "chat", field.Agent, "model"),
			p.overridePresence("models", "chat", field.Agent, "effort"),
		}
	}
	if len(path) != 4 {
		return [3]bool{}
	}
	for _, scale := range config.TaskScales {
		if field.FieldName() != scale+"_model" && field.FieldName() != scale+"_effort" {
			continue
		}
		agent := p.overridePresence("kanban_agents", scale)
		if path[1] == "review_roles" {
			agent = p.overridePresence("reviewers", scale, field.Agent)
		}
		return [3]bool{
			agent,
			p.overridePresence("models", path[1], field.Agent, scale+"_model"),
			p.overridePresence("models", path[1], field.Agent, scale+"_effort"),
		}
	}
	return [3]bool{}
}
