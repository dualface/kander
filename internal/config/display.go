package config

import "strings"

// DisplayConfig returns the configuration as kander config shows it: cfg as it would load
// without the content this version ignores. An unusable agents entry and the keys of a newer
// version are left out, so the built-in agent the entry took out of service shows with its
// models again, and a role that names an agent this version does not know is left out of the
// layer that holds it, so the role shows what the other layer or the default gives it. cfg itself
// stays as loaded, because saving keeps all of that; kander config reports what it hid as
// notices. The layers are read again from the files of the current scope, and the usual merge
// and defaults produce the result. When the cleaned layers do not validate, which no known case
// does, cfg is returned as loaded: a display problem must not fail the command.
func DisplayConfig(cfg *Config) (*Config, error) {
	if cfg == nil || (len(cfg.Ignored) == 0 && len(UnknownAgentRefs(cfg)) == 0) {
		return cfg, nil
	}
	scope, overlay, err := displayLayers()
	if err != nil {
		return nil, err
	}
	// Removing a leaf normally leaves its parent object in place, since some objects are
	// required; one that must disappear with its last key (a section of only unknown keys) is
	// found by trying again with empty parents pruned.
	for _, prune := range []bool{false, true} {
		if display, ok := displayWithout(cfg, scope, overlay, prune); ok {
			return display, nil
		}
	}
	return cfg, nil
}

// displayLayers reads the raw scope document and the project overlay of the current directory.
// The overlay is nil when there is none.
func displayLayers() (scope, overlay map[string]any, err error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, nil, err
	}
	_, scope, err = loadScopeRawAt(path, false)
	if err != nil {
		return nil, nil, err
	}
	_, overlay, err = readOverlay("")
	return scope, overlay, err
}

// displayWithout removes the ignored content from copies of the layers and merges them.
func displayWithout(cfg *Config, scope, overlay map[string]any, prune bool) (*Config, bool) {
	scope = cloneRawObjectDeep(scope)
	if overlay != nil {
		overlay = cloneRawObjectDeep(overlay)
	}
	remove := func(path ...string) {
		for _, layer := range []map[string]any{scope, overlay} {
			if layer == nil {
				continue
			}
			if prune {
				OverlayDelete(layer, path...)
			} else {
				deleteLeaf(layer, path)
			}
		}
	}
	merge := func() (*Config, error) {
		if overlay == nil {
			return Validate(cloneRawObjectDeep(scope))
		}
		return MergeOverlayOnRaw(scope, overlay)
	}
	// An unusable entry goes first: with it gone, a built-in agent is known again and its
	// models.* entries are settings, not ignored content. What is still ignored after that is
	// removed until nothing is left; each round removes what the last one reported.
	for _, item := range cfg.Ignored {
		if item.Kind == IgnoredUnusableAgent {
			remove(item.Path...)
		}
	}
	display, err := merge()
	for range 8 {
		if err != nil {
			return nil, false
		}
		if len(display.Ignored) == 0 {
			break
		}
		for _, item := range display.Ignored {
			remove(item.Path...)
		}
		display, err = merge()
	}
	if err != nil {
		return nil, false
	}
	// A role that names an agent this version does not know goes out of the layer that names it
	// before the merge, so the role takes the value the other layer or the defaults give it.
	refs := UnknownAgentRefs(display)
	if len(refs) == 0 {
		return display, true
	}
	if scope["reviewers"] != nil {
		_ = normalizeReviewersField(scope)
	}
	if overlay != nil && overlay["reviewers"] != nil {
		_ = normalizeReviewersField(overlay)
	}
	for _, ref := range refs {
		path := strings.Split(ref.Field, ".")
		for _, layer := range []map[string]any{scope, overlay} {
			if layer == nil {
				continue
			}
			if value, ok := OverlayGet(layer, path...); ok && value == ref.Agent {
				if len(path) == 1 && path[0] == "kanban_agent" {
					// A required key: the default agent stands in for it.
					layer[path[0]] = defaultAgentName()
					continue
				}
				if prune {
					OverlayDelete(layer, path...)
				} else {
					deleteLeaf(layer, path)
				}
			}
		}
	}
	display, err = merge()
	return display, err == nil
}

// deleteLeaf removes the key the path names and leaves every parent object in place.
func deleteLeaf(obj map[string]any, path []string) {
	for _, key := range path[:len(path)-1] {
		next, ok := obj[key].(map[string]any)
		if !ok {
			return
		}
		obj = next
	}
	delete(obj, path[len(path)-1])
}
