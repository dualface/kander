package config

import "strings"

// DisplayConfig returns the configuration as kander config shows it: cfg as it would load
// without the content this version ignores. An unusable agents entry and the keys of a newer
// version are left out, so the built-in agent the entry took out of service shows with its
// models again, and a role that names an agent this version does not know shows the value it
// falls back to when the key is absent. cfg itself stays as loaded, because saving keeps all of
// that; kander config reports what it hid as notices. It returns cfg when there is nothing to
// hide. The configuration is read again from the files of the current scope.
func DisplayConfig(cfg *Config) (*Config, error) {
	if cfg == nil || (len(cfg.Ignored) == 0 && len(UnknownAgentRefs(cfg)) == 0) {
		return cfg, nil
	}
	display := cfg
	if len(cfg.Ignored) > 0 {
		_, raw, err := loadEffectiveRaw("", false)
		if err != nil {
			return nil, err
		}
		// An unusable entry goes first: with it gone, a built-in agent is known again and its
		// models.* entries are settings, not ignored content. What is still ignored after that
		// is removed until nothing is left.
		for _, item := range cfg.Ignored {
			if item.Kind == IgnoredUnusableAgent {
				OverlayDelete(raw, item.Path...)
			}
		}
		// Each round removes what the last one reported; a path that cannot be removed ends the loop.
		for range 8 {
			display, err = Validate(raw)
			if err != nil {
				return nil, err
			}
			if len(display.Ignored) == 0 {
				break
			}
			for _, item := range display.Ignored {
				OverlayDelete(raw, item.Path...)
			}
		}
	} else {
		display = Clone(cfg)
	}
	for _, ref := range UnknownAgentRefs(display) {
		setRoleToFallback(display, ref.Field)
	}
	return display, nil
}

// setRoleToFallback sets the role field the dotted path names to the value the role has when
// its key is absent: kanban_agent and a reviewer take the default agent, a task scale takes
// kanban_agent, and Chat takes the large scale.
func setRoleToFallback(cfg *Config, field string) {
	parts := strings.Split(field, ".")
	switch parts[0] {
	case "kanban_agent":
		cfg.KanbanAgent = defaultAgentName()
	case "kanban_agents":
		cfg.KanbanAgents[parts[1]] = cfg.KanbanAgent
	case "chat_agent":
		cfg.ChatAgent = cfg.KanbanAgents["large"]
	case "reviewers":
		cfg.Reviewers[parts[1]][parts[2]] = defaultAgentName()
	}
}
