package config

import "strings"

// ChatSettings is the execution agent and model pair a TUI chat session uses.
type ChatSettings struct {
	Agent  string
	Model  string
	Effort string
}

func chatAllowedFields(cfg *Config, agent string) map[string]struct{} {
	allowed := map[string]struct{}{"model": {}}
	if AgentSupportsEffort(cfg, agent) {
		allowed["effort"] = struct{}{}
	}
	return allowed
}

func chatFieldsFromKanban(cfg *Config, agent string, kanban map[string]string) map[string]string {
	out := map[string]string{"model": KanbanModelFor(kanban, "large")}
	if AgentSupportsEffort(cfg, agent) {
		out["effort"] = kanban["large_effort"]
	}
	return out
}

func chatModelDefaults() map[string]map[string]string {
	kanban := kanbanModelDefaults()
	out := make(map[string]map[string]string, len(ExecutionAgents))
	for _, name := range ExecutionAgents {
		out[name] = chatFieldsFromKanban(nil, name, kanban[name])
	}
	return out
}

func validateChatModels(provided map[string]any, names []string, definitions map[string]AgentDefinition) (map[string]map[string]string, error) {
	probe := &Config{Agents: definitions}
	allowedAgents := map[string]struct{}{}
	for _, name := range names {
		allowedAgents[name] = struct{}{}
	}
	var unknownAgents []string
	for agent := range provided {
		if _, ok := allowedAgents[agent]; !ok {
			unknownAgents = append(unknownAgents, agent)
		}
	}
	if len(unknownAgents) > 0 {
		return nil, configErrorf(
			"config.models_has_unknown_agents", "chat", strings.Join(sorted(unknownAgents), ", "),
		)
	}
	out := make(map[string]map[string]string, len(provided))
	for agent, entryRaw := range provided {
		entry, ok := entryRaw.(map[string]any)
		if !ok {
			return nil, configErrorf("config.models_must_be_a_json_object_3", "chat", agent)
		}
		allowedFields := chatAllowedFields(probe, agent)
		var unknownFields []string
		for field := range entry {
			if _, ok := allowedFields[field]; !ok {
				unknownFields = append(unknownFields, field)
			}
		}
		if len(unknownFields) > 0 {
			return nil, configErrorf(
				"config.models_has_unknown_fields", "chat", agent, strings.Join(sorted(unknownFields), ", "),
			)
		}
		fields := map[string]string{}
		for field, value := range entry {
			text, ok := value.(string)
			_, modelID := modelIDFields[field]
			if !ok || (!modelID && strings.TrimSpace(text) == "") {
				kind := Text("config.non_empty_string")
				if modelID {
					kind = Text("config.string")
				}
				return nil, configErrorf("config.models_must_be_a", "chat", agent, field, kind)
			}
			if strings.ContainsAny(text, "\n\r\x00") {
				return nil, configErrorf(
					"config.models_must_not_contain_line_breaks_or_nul", "chat", agent, field,
				)
			}
			fields[field] = text
		}
		out[agent] = fields
	}
	return out, nil
}

func fillChatAgentFromKanban(cfg *Config, agent string) {
	if cfg == nil || agent == "" {
		return
	}
	if cfg.Models.Chat == nil {
		cfg.Models.Chat = map[string]map[string]string{}
	}
	entry := cfg.Models.Chat[agent]
	if entry == nil {
		entry = map[string]string{}
		cfg.Models.Chat[agent] = entry
	}
	if _, ok := entry["model"]; !ok {
		entry["model"] = KanbanModelFor(cfg.Models.Kanban[agent], "large")
	}
	if AgentSupportsEffort(cfg, agent) {
		if _, ok := entry["effort"]; !ok {
			entry["effort"] = cfg.Models.Kanban[agent]["large_effort"]
		}
	}
}

// EnsureChatEntry fills missing Chat model/effort keys for agent from that
// agent's effective kanban large values, so Options can show a concrete field.
func EnsureChatEntry(cfg *Config, agent string) {
	fillChatAgentFromKanban(cfg, agent)
}

func applyChatFallbacks(cfg *Config, raw map[string]any) {
	if cfg == nil {
		return
	}
	if cfg.ChatAgent == "" {
		cfg.ChatAgent = cfg.KanbanAgents["large"]
	}
	if cfg.Models.Chat == nil {
		cfg.Models.Chat = map[string]map[string]string{}
	}
	modelsRaw, _ := asObject(raw["models"])
	_, hasChat := modelsRaw["chat"]
	if !hasChat {
		for _, agent := range AgentNames(cfg) {
			fillChatAgentFromKanban(cfg, agent)
		}
		return
	}
	fillChatAgentFromKanban(cfg, cfg.ChatAgent)
}

func derivedChatObject(cfgProbe *Config, kanban map[string]any) map[string]any {
	out := map[string]any{}
	for agent, entryRaw := range kanban {
		entry, _ := asObject(entryRaw)
		fields := map[string]any{"model": KanbanModelFor(stringMapFromRaw(entry), "large")}
		if AgentSupportsEffort(cfgProbe, agent) {
			fields["effort"] = entry["large_effort"]
			if fields["effort"] == nil {
				fields["effort"] = ""
			}
		}
		out[agent] = fields
	}
	return out
}

func stringMapFromRaw(entry map[string]any) map[string]string {
	out := map[string]string{}
	for key, value := range entry {
		text, _ := value.(string)
		out[key] = text
	}
	return out
}

func providedHasChatModels(provided map[string]any) bool {
	models, ok := asObject(provided["models"])
	if !ok {
		return false
	}
	_, has := models["chat"]
	return has
}

// fillMissingChatRaw replaces default Chat values with this document's kanban
// large settings when the source omitted chat_agent or models.chat.
func fillMissingChatRaw(root, provided map[string]any) {
	if root == nil {
		return
	}
	if provided == nil {
		provided = map[string]any{}
	}
	if _, exists := provided["chat_agent"]; !exists {
		root["chat_agent"] = chatAgentFromRaw(root)
	}
	if providedHasChatModels(provided) {
		return
	}
	modelsRoot, ok := asObject(root["models"])
	if !ok {
		return
	}
	kanban, _ := asObject(modelsRoot["kanban"])
	probe := &Config{}
	if agents, err := validateAgentDefinitions(root["agents"]); err == nil {
		probe.Agents = agents
	}
	modelsRoot["chat"] = derivedChatObject(probe, kanban)
}

func chatAgentFromRaw(root map[string]any) string {
	if agents, ok := asObject(root["kanban_agents"]); ok {
		if text, ok := agents["large"].(string); ok && text != "" {
			return text
		}
	}
	if text, ok := root["kanban_agent"].(string); ok && text != "" {
		return text
	}
	return defaultAgentName()
}

// ChatAgentFor returns the execution agent a chat session uses. A missing
// chat_agent falls back to the effective large execution agent.
func ChatAgentFor(cfg *Config) string {
	if cfg == nil {
		return ""
	}
	if cfg.ChatAgent != "" {
		return cfg.ChatAgent
	}
	return cfg.KanbanAgents["large"]
}

// ResolveChat returns the effective chat agent, model, and effort. Missing
// chat fields fall back to the agent's effective kanban large values.
func ResolveChat(cfg *Config) (agent, model, effort string) {
	if cfg == nil {
		return "", "", ""
	}
	agent = ChatAgentFor(cfg)
	entry := cfg.Models.Chat[agent]
	if _, ok := entry["model"]; ok {
		model = entry["model"]
	} else {
		model = KanbanModelFor(cfg.Models.Kanban[agent], "large")
	}
	if AgentSupportsEffort(cfg, agent) {
		if _, ok := entry["effort"]; ok {
			effort = entry["effort"]
		} else {
			effort = cfg.Models.Kanban[agent]["large_effort"]
		}
	}
	return agent, model, effort
}

// ChatSettingsFor is the shared resolver PreviewChat and StartChat use for
// the effective Chat agent, model, and effort.
func ChatSettingsFor(cfg *Config) ChatSettings {
	agent, model, effort := ResolveChat(cfg)
	return ChatSettings{Agent: agent, Model: model, Effort: effort}
}

func FormatChatModelSummary(cfg *Config, agent string, entry map[string]string) string {
	model := ""
	if entry != nil {
		model = entry["model"]
	}
	if model == "" {
		model = Text("config.cli_default")
	}
	if !AgentSupportsEffort(cfg, agent) {
		return model
	}
	effort := ""
	if entry != nil {
		effort = entry["effort"]
	}
	return formatModelEffort(model, effort)
}
