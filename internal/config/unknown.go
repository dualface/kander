package config

import (
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Forward compatibility: a configuration file written by a newer Kander may carry
// keys and values this version does not know. Loading ignores them and reports
// them; saving writes them back unchanged, so a temporary rollback to this version
// neither fails nor loses the newer settings.

// Kinds of Ignored items.
const (
	// IgnoredField is a key the schema of this version does not define.
	IgnoredField = "field"
	// IgnoredAgent is a models.* entry for an agent this version does not know.
	IgnoredAgent = "agent"
	// IgnoredUnusableAgent is an agents.<name> entry this version cannot turn into a
	// usable agent. Reason says why; the entry is kept whole.
	IgnoredUnusableAgent = "unusable_agent"
)

// Ignored is one piece of configuration the current version does not use.
type Ignored struct {
	// Path is the JSON path as key segments; an array element is a decimal segment.
	Path []string
	Kind string
	// Reason is set for IgnoredUnusableAgent.
	Reason string
	// Value is the decoded JSON value, written back unchanged on save.
	Value any
	// Source is the project overlay path when the item came from there, else "".
	Source string
}

// PathString renders the path with dots, the form doctor and config print.
func (i Ignored) PathString() string { return strings.Join(i.Path, ".") }

// AgentRef is a role field naming an agent this version cannot use.
type AgentRef struct {
	Field string
	Agent string
}

// UnknownAgentRefs lists role fields (kanban_agent, kanban_agents.*, chat_agent,
// reviewers.*.*) whose agent name is not available in this version. Their values stay
// in the configuration; only using the role fails.
func UnknownAgentRefs(cfg *Config) []AgentRef {
	if cfg == nil {
		return nil
	}
	var out []AgentRef
	add := func(field, agent string) {
		if agent != "" && !HasAgent(cfg, agent) {
			out = append(out, AgentRef{Field: field, Agent: agent})
		}
	}
	add("kanban_agent", cfg.KanbanAgent)
	for _, scale := range TaskScales {
		add("kanban_agents."+scale, cfg.KanbanAgents[scale])
	}
	add("chat_agent", cfg.ChatAgent)
	for _, scale := range TaskScales {
		for _, role := range ReviewRoles {
			add("reviewers."+scale+"."+role, cfg.Reviewers[scale][role])
		}
	}
	return out
}

// UnavailableAgentError explains why the named agent cannot be used: its entry is
// unusable in this version, or the name is not known here at all.
func UnavailableAgentError(cfg *Config, name string) error {
	if cfg != nil {
		if reason, ok := cfg.UnusableAgents[name]; ok {
			return configErrorf("config.agent_unusable", name, reason)
		}
	}
	return configErrorf("config.agent_unknown_reference", name)
}

// unusableAgent is an agents.<name> entry that cannot become an agent here.
type unusableAgent struct {
	Reason string
	Value  any
	// Policy carries the install switches (install_rules, config_dir) of an
	// unusable built-in entry; both keep applying.
	Policy AgentDefinition
}

// scanIgnored walks a raw configuration object and lists everything this version
// ignores. It tolerates wrongly typed sections; Validate reports those separately.
// defs and unusable come from agentEntries on the same document.
func scanIgnored(root map[string]any, defs map[string]AgentDefinition, unusable map[string]unusableAgent) []Ignored {
	var out []Ignored
	add := func(kind string, value any, path ...string) {
		out = append(out, Ignored{Path: append([]string(nil), path...), Kind: kind, Value: value})
	}
	probe := &Config{Agents: defs, UnusableAgents: unusableReasons(unusable)}
	names := AgentNames(probe)
	for key, value := range root {
		if !containsKey(jsonKeys(reflect.TypeOf(Config{})), key) {
			add(IgnoredField, value, key)
		}
	}
	scanKeys(root["kanban_agents"], reviewScaleSet(), add, "kanban_agents")
	scanKeys(root["rules"], moduleSet(), add, "rules")
	scanStructSection(root["tui"], reflect.TypeOf(TUI{}), add, "tui")
	scanStructSection(root["issue"], reflect.TypeOf(Issue{}), add, "issue")
	scanStructSection(root["git"], reflect.TypeOf(Git{}), add, "git")
	scanStructSection(root["server"], reflect.TypeOf(Server{}), add, "server")
	scanStructSection(root["project"], reflect.TypeOf(Project{}), add, "project")
	scanReviewSection(root["reviewers"], add, "reviewers")
	scanReviewSection(root["review_stages"], add, "review_stages")
	scanModels(root["models"], probe, names, add)
	scanAgents(root["agents"], defs, unusable, add)
	for name, entry := range unusable {
		out = append(out, Ignored{Path: []string{"agents", name}, Kind: IgnoredUnusableAgent, Reason: entry.Reason, Value: entry.Value})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].PathString() < out[j].PathString() })
	return out
}

type addFunc func(kind string, value any, path ...string)

func moduleSet() map[string]struct{} {
	set := make(map[string]struct{}, len(RuleModules))
	for _, module := range RuleModules {
		set[module] = struct{}{}
	}
	return set
}

func unusablePolicies(unusable map[string]unusableAgent) map[string]AgentDefinition {
	var out map[string]AgentDefinition
	for name, entry := range unusable {
		if entry.Policy == (AgentDefinition{}) {
			continue
		}
		if out == nil {
			out = map[string]AgentDefinition{}
		}
		out[name] = entry.Policy
	}
	return out
}

func unusableReasons(unusable map[string]unusableAgent) map[string]string {
	if len(unusable) == 0 {
		return nil
	}
	out := make(map[string]string, len(unusable))
	for name, entry := range unusable {
		out[name] = entry.Reason
	}
	return out
}

func scanKeys(raw any, known map[string]struct{}, add addFunc, path ...string) {
	obj, ok := asObject(raw)
	if !ok {
		return
	}
	for key, value := range obj {
		if !containsKey(known, key) {
			add(IgnoredField, value, append(path, key)...)
		}
	}
}

func scanStructSection(raw any, t reflect.Type, add addFunc, path ...string) {
	scanKeys(raw, jsonKeys(t), add, path...)
}

// jsonKeys lists the JSON names of the exported fields of a struct type.
func jsonKeys(t reflect.Type) map[string]struct{} {
	out := map[string]struct{}{}
	for i := 0; i < t.NumField(); i++ {
		if name, ok := jsonName(t.Field(i)); ok {
			out[name] = struct{}{}
		}
	}
	return out
}

func jsonName(field reflect.StructField) (string, bool) {
	if !field.IsExported() {
		return "", false
	}
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" {
		name = field.Name
	}
	return name, true
}

// scanReviewSection handles reviewers and review_stages: either the two-scale form
// (scale -> role -> value) or the legacy flat role form. Historical role names are
// known keys, since they fold into the current roles.
func scanReviewSection(raw any, add addFunc, path ...string) {
	obj, ok := asObject(raw)
	if !ok {
		return
	}
	roles := historicalReviewRoleSet()
	for key, value := range obj {
		switch {
		case containsKey(reviewScaleSet(), key):
			scaleObj, ok := asObject(value)
			if !ok {
				continue
			}
			for role, roleValue := range scaleObj {
				if !containsKey(roles, role) {
					add(IgnoredField, roleValue, append(append([]string(nil), path...), key, role)...)
				}
			}
		case containsKey(roles, key):
		default:
			add(IgnoredField, value, append(append([]string(nil), path...), key)...)
		}
	}
}

func scanModels(raw any, probe *Config, names []string, add addFunc) {
	obj, ok := asObject(raw)
	if !ok {
		return
	}
	known := map[string]struct{}{"kanban": {}, "review": {}, "review_roles": {}, "chat": {}}
	for key, value := range obj {
		if !containsKey(known, key) {
			add(IgnoredField, value, "models", key)
		}
	}
	defaults := DefaultModels()
	customModelDefaults(probe.Agents, &defaults)
	reviewNames := ReviewAgentNames(probe)
	scanAgentSection(obj["kanban"], names, func(agent string) map[string]struct{} { return keySet(defaults.Kanban[agent]) }, add, "models", "kanban")
	scanAgentSection(obj["review"], reviewNames, func(agent string) map[string]struct{} { return keySet(defaults.Review[agent]) }, add, "models", "review")
	scanAgentSection(obj["chat"], names, func(agent string) map[string]struct{} { return chatAllowedFields(probe, agent) }, add, "models", "chat")
	if roles, ok := asObject(obj["review_roles"]); ok {
		schema := reviewRoleFieldSchema()
		for role, entryRaw := range roles {
			if !containsKey(historicalReviewRoleSet(), role) {
				add(IgnoredField, entryRaw, "models", "review_roles", role)
				continue
			}
			entry, ok := asObject(entryRaw)
			if !ok {
				continue
			}
			for field, value := range entry {
				if _, ok := schema[field]; !ok {
					add(IgnoredField, value, "models", "review_roles", role, field)
				}
			}
		}
	}
}

func keySet(m map[string]string) map[string]struct{} {
	out := make(map[string]struct{}, len(m))
	for key := range m {
		out[key] = struct{}{}
	}
	return out
}

// scanAgentSection handles models.kanban, models.review and models.chat: entries for
// agents this version does not know, and unknown fields inside known entries.
func scanAgentSection(raw any, agents []string, fields func(agent string) map[string]struct{}, add addFunc, path ...string) {
	obj, ok := asObject(raw)
	if !ok {
		return
	}
	for agent, entryRaw := range obj {
		if !contains(agents, agent) {
			add(IgnoredAgent, entryRaw, append(append([]string(nil), path...), agent)...)
			continue
		}
		entry, ok := asObject(entryRaw)
		if !ok {
			continue
		}
		known := fields(agent)
		for field, value := range entry {
			if !containsKey(known, field) {
				add(IgnoredField, value, append(append([]string(nil), path...), agent, field)...)
			}
		}
	}
}

// scanAgents reports unknown keys inside usable agent entries; unusable entries are
// kept whole by scanIgnored itself.
func scanAgents(raw any, defs map[string]AgentDefinition, unusable map[string]unusableAgent, add addFunc) {
	obj, ok := asObject(raw)
	if !ok {
		return
	}
	for name, value := range obj {
		if _, bad := unusable[name]; bad {
			continue
		}
		if _, ok := defs[name]; !ok {
			continue
		}
		scanStruct(value, reflect.TypeOf(AgentDefinition{}), add, "agents", name)
	}
}

// scanStruct reports the keys of raw that the struct type does not declare, and
// recurses through nested structs, pointers, and slices of structs. Free-form maps
// (such as env) take any key.
func scanStruct(raw any, t reflect.Type, add addFunc, path ...string) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		obj, ok := asObject(raw)
		if !ok {
			return
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < t.NumField(); i++ {
			if name, ok := jsonName(t.Field(i)); ok {
				fields[name] = t.Field(i).Type
			}
		}
		for key, value := range obj {
			child, known := fields[key]
			if !known {
				add(IgnoredField, value, append(append([]string(nil), path...), key)...)
				continue
			}
			scanStruct(value, child, add, append(append([]string(nil), path...), key)...)
		}
	case reflect.Slice:
		items, ok := raw.([]any)
		if !ok {
			return
		}
		for i, item := range items {
			scanStruct(item, t.Elem(), add, append(append([]string(nil), path...), strconv.Itoa(i))...)
		}
	}
}

// IgnoredNotice renders the localized line for one ignored item.
func IgnoredNotice(item Ignored) string {
	var line string
	switch item.Kind {
	case IgnoredAgent:
		line = Text("config.ignored_agent", item.PathString())
	case IgnoredUnusableAgent:
		line = Text("config.ignored_unusable_agent", item.PathString(), item.Reason)
	default:
		line = Text("config.ignored_field", item.PathString())
	}
	if item.Source != "" {
		line = Text("config.ignored_from", line, item.Source)
	}
	return line
}

// UnknownAgentRefNotices renders one localized line per role that names an agent
// this version cannot use.
func UnknownAgentRefNotices(cfg *Config) []string {
	var out []string
	for _, ref := range UnknownAgentRefs(cfg) {
		out = append(out, Text("config.unknown_agent_reference_notice", ref.Field, ref.Agent))
	}
	return out
}

// structKind reports whether t, after pointers, is a struct type.
func structKind(t reflect.Type) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Kind() == reflect.Struct
}

// pruneToStruct returns a copy of raw that keeps only the keys the struct type
// declares, matched exactly, through nested structs, pointers, and slices. Free-form
// maps keep every key. The standard decoder matches keys case-insensitively, so
// decoding a pruned copy keeps the decoded definition in step with scanStruct.
func pruneToStruct(raw any, t reflect.Type) any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		obj, ok := asObject(raw)
		if !ok {
			return raw
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < t.NumField(); i++ {
			if name, ok := jsonName(t.Field(i)); ok {
				fields[name] = t.Field(i).Type
			}
		}
		out := make(map[string]any, len(obj))
		for key, value := range obj {
			child, known := fields[key]
			if !known {
				continue
			}
			pruned := pruneToStruct(value, child)
			// A nested section whose keys were all unknown is the same as no section:
			// decoding an empty one would fail its required keys. An empty section the
			// user wrote stays, so its own validation still applies.
			if sub, ok := pruned.(map[string]any); ok && len(sub) == 0 && structKind(child) {
				if original, ok := asObject(value); ok && len(original) > 0 {
					continue
				}
			}
			out[key] = pruned
		}
		return out
	case reflect.Slice:
		items, ok := raw.([]any)
		if !ok {
			return raw
		}
		out := make([]any, len(items))
		for i, item := range items {
			out[i] = pruneToStruct(item, t.Elem())
		}
		return out
	}
	return raw
}
