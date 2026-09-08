package menu

import (
	"github.com/dualface/kander/internal/config"
	"strings"
)

// AgentExecutableFields shares one definition between both task scales.
// Blank edits remove the override instead of persisting an invalid empty value.
func (s *Session) AgentExecutableFields(scale string) []ModelField {
	agent := s.Config.KanbanAgents[scale]
	if agent == "" {
		return nil
	}
	var out []ModelField
	for _, key := range []string{"path", "process_name"} {
		field := key
		label := config.Text("menu.agent_" + field)
		out = append(out, ModelField{Agent: agent, field: field, Label: agent + " " + label, Short: label, Prompt: label,
			Placeholder: config.Text("menu.agent_default"),
			get: func() string {
				d := s.Config.Agents[agent]
				if field == "path" {
					return d.Path
				}
				return d.ProcessName
			},
			set: func(value string) {
				if s.Config.Agents == nil {
					s.Config.Agents = map[string]config.AgentDefinition{}
				}
				d := s.Config.Agents[agent]
				value = strings.TrimSpace(value)
				if field == "path" {
					d.Path = value
				} else {
					d.ProcessName = value
				}
				if d.Path == "" && d.ProcessName == "" && d.Dialect == "" && d.Args == nil && d.Session == nil {
					delete(s.Config.Agents, agent)
				} else {
					s.Config.Agents[agent] = d
				}
			},
		})
	}
	return out
}
