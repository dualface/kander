package config

import (
	"reflect"
	"slices"
)

// LoadWithDisplay reads the scope file and the project overlay once and returns the
// configuration as Load gives it and the configuration kander config shows. The display is the
// reference configuration of docs/config-forward-compat.md: the same layers without the content
// this version ignores. An unusable agents entry and the keys of a newer version stay out of
// what is shown, so the built-in agent the entry took out of service shows with its models
// again, and a role that names an agent this version does not know shows what the other layer
// or the default gives it. loaded itself stays as Load returns it, because saving keeps all of
// that; kander config reports what it hid as notices.
func LoadWithDisplay(missingOK bool) (loaded, display *Config, err error) {
	layers, err := readLayers("", missingOK)
	if err != nil {
		return nil, nil, err
	}
	loaded, _, err = layers.effective()
	if err != nil {
		return nil, nil, err
	}
	display, err = layers.reference(loaded)
	if err != nil {
		return nil, nil, err
	}
	return loaded, display, nil
}

// reference builds the reference configuration of the layers. The loader already ignores the
// keys it does not know, so they stay in the copies; only the three kinds of content that
// change a setting are taken out, each one from the layer that holds it:
//
//  1. an unusable agents entry, from every layer that holds it;
//  2. a role key that names an agent this version does not know;
//  3. the keys this version does not know in an agents entry, which empties a section or an
//     entry that held only such keys;
//
// and a section the loader requires keys of is dropped from both layers when neither layer holds
// one of its keys (dropUnfilledSections). The steps run once, in this order, and one validation sits between rules 1 and 2 because
// whether an agent is known depends on the entries that remain. The layers are not modified.
func (l layerDocuments) reference(loaded *Config) (*Config, error) {
	scope := cloneRawObjectDeep(l.scope)
	layers := []map[string]any{scope}
	var overlay map[string]any
	if l.overlay != nil {
		overlay = cloneRawObjectDeep(l.overlay)
		layers = append(layers, overlay)
	}
	for _, item := range loaded.Ignored {
		if item.Kind != IgnoredUnusableAgent {
			continue
		}
		for _, layer := range layers {
			if agents, ok := layer["agents"].(map[string]any); ok {
				delete(agents, item.Path[len(item.Path)-1])
			}
		}
	}
	known, _, err := validateLayers(scope, overlay)
	if err != nil {
		return nil, err
	}
	for _, layer := range layers {
		dropUnknownRoles(layer, known)
		pruneAgentEntries(layer)
	}
	dropUnfilledSections(layers)
	// The scope file requires kanban_agent, so the default agent stands in for an unknown one;
	// the overlay's key is removed like the other roles.
	if name, ok := scope["kanban_agent"].(string); ok && !HasAgent(known, name) {
		scope["kanban_agent"] = defaultAgentName()
	}
	if name, ok := overlay["kanban_agent"].(string); ok && !HasAgent(known, name) {
		delete(overlay, "kanban_agent")
	}
	display, _, err := validateLayers(scope, overlay)
	return display, err
}

// dropUnknownRoles removes from one layer the role keys that name an agent known does not
// have: the task scales of kanban_agents, chat_agent and the reviewer roles under their own
// names (PM and QA are removed as PM and QA, before they fold into PMQA). kanban_agent is left
// to the caller, because the scope file requires it.
func dropUnknownRoles(layer map[string]any, known *Config) {
	unknown := func(value any) bool {
		name, ok := value.(string)
		return ok && name != "" && !HasAgent(known, name)
	}
	if unknown(layer["chat_agent"]) {
		delete(layer, "chat_agent")
	}
	if scales, ok := layer["kanban_agents"].(map[string]any); ok {
		for _, scale := range TaskScales {
			if unknown(scales[scale]) {
				delete(scales, scale)
			}
		}
	}
	// The merge reads reviewers in the two-scale form, so a flat legacy object is read that way.
	if err := normalizeReviewersField(layer); err != nil {
		return
	}
	reviewers, _ := layer["reviewers"].(map[string]any)
	for _, scale := range TaskScales {
		roles, ok := reviewers[scale].(map[string]any)
		if !ok {
			continue
		}
		for _, role := range historicalReviewRoles {
			if unknown(roles[role]) {
				delete(roles, role)
			}
		}
	}
}

// pruneAgentEntries reduces the agents entries of one layer to the keys this version knows,
// with the loader's own pruneToStruct: a section of an entry that held only unknown keys (a
// program_status of a newer version) is absent from the entry, as it is when the layer is
// loaded alone, and an entry that was not empty and has nothing left is absent from the layer. A
// section or an entry the layer wrote as {} is a declaration and stays.
func pruneAgentEntries(layer map[string]any) {
	agents, ok := layer["agents"].(map[string]any)
	if !ok {
		return
	}
	for name, entry := range agents {
		fields, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		pruned, _ := pruneToStruct(fields, reflect.TypeFor[AgentDefinition]()).(map[string]any)
		if len(pruned) == 0 && len(fields) > 0 {
			delete(agents, name)
			continue
		}
		agents[name] = pruned
	}
}

// dropUnfilledSections removes from the layers the sections the loader does not accept empty
// (tui, and the session of an agents entry, whose keys are required) when no layer holds one of
// their keys. Such a section is only empty here because the unknown keys that were in it, or a
// {} that another layer wrote next to them, are not part of the reference; the loader reads the
// section as absent then.
func dropUnfilledSections(layers []map[string]any) {
	type section struct {
		path  []string
		known map[string]struct{}
	}
	sections := []section{{[]string{"tui"}, jsonKeys(reflect.TypeFor[TUI]())}}
	session := jsonKeys(reflect.TypeFor[AgentSessionDefinition]())
	seen := map[string]bool{}
	for _, layer := range layers {
		agents, _ := layer["agents"].(map[string]any)
		for name := range agents {
			if !seen[name] {
				seen[name] = true
				sections = append(sections, section{[]string{"agents", name, "session"}, session})
			}
		}
	}
	for _, s := range sections {
		filled := slices.ContainsFunc(layers, func(layer map[string]any) bool {
			value, _ := OverlayGet(layer, s.path...)
			obj, _ := value.(map[string]any)
			for key := range obj {
				if containsKey(s.known, key) {
					return true
				}
			}
			return false
		})
		if filled {
			continue
		}
		for _, layer := range layers {
			if parent, ok := OverlayGet(layer, s.path[:len(s.path)-1]...); ok {
				if obj, ok := parent.(map[string]any); ok {
					delete(obj, s.path[len(s.path)-1])
				}
			}
		}
	}
}
