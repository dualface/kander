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
	if display, ok := displayWithout(cfg, scope, overlay); ok {
		return display, nil
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
func displayWithout(cfg *Config, scope, overlay map[string]any) (*Config, bool) {
	scope = cloneRawObjectDeep(scope)
	if overlay != nil {
		overlay = cloneRawObjectDeep(overlay)
	}
	remove := func(path ...string) {
		for _, layer := range []map[string]any{scope, overlay} {
			if layer == nil {
				continue
			}
			deleteAndTidy(layer, path)
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
	// before the merge, so the role takes the value the other layer or the defaults give it. A
	// layer that held an unknown agent the other layer overrode shows it after the first round,
	// so the rounds repeat until no role names one.
	for range 4 {
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
			for i, layer := range []map[string]any{scope, overlay} {
				if layer == nil {
					continue
				}
				for _, key := range roleKeys(path) {
					if value, ok := OverlayGet(layer, key...); ok && value == ref.Agent {
						if i == 0 && len(key) == 1 && key[0] == "kanban_agent" {
							// The scope file requires kanban_agent: the default agent stands in for it.
							layer[key[0]] = defaultAgentName()
						} else {
							deleteAndTidy(layer, key)
						}
					}
				}
			}
		}
		if display, err = merge(); err != nil {
			return nil, false
		}
	}
	return display, true
}

// deleteAndTidy removes the key the path names. A parent object that this leaves empty stays
// when the configuration accepts it empty (the required reviewers object) and goes when it needs
// content or may be absent (a tui section of only unknown keys, an agents entry).
func deleteAndTidy(obj map[string]any, path []string) {
	chain := []map[string]any{obj}
	for _, key := range path[:len(path)-1] {
		next, ok := chain[len(chain)-1][key].(map[string]any)
		if !ok {
			return
		}
		chain = append(chain, next)
	}
	delete(chain[len(chain)-1], path[len(path)-1])
	for depth := len(chain) - 1; depth > 0 && len(chain[depth]) == 0; depth-- {
		if !acceptsEmptyObject(path[:depth]) {
			delete(chain[depth-1], path[depth-1])
		}
	}
}

// acceptsEmptyObject reports whether the default configuration stays valid with the object at
// path emptied. When it does not, the object is removed instead when the default configuration
// stays valid without it; when neither holds, it is kept.
func acceptsEmptyObject(path []string) bool {
	doc, err := DocumentFromConfig(DefaultConfig())
	if err != nil {
		return true
	}
	parent := doc
	for _, key := range path[:len(path)-1] {
		next, ok := parent[key].(map[string]any)
		if !ok {
			// The default configuration has no such parent: an empty object here is a
			// stranger to it, so it goes.
			return false
		}
		parent = next
	}
	last := path[len(path)-1]
	parent[last] = map[string]any{}
	if _, err := Validate(doc); err == nil {
		return true
	}
	delete(parent, last)
	_, err = Validate(doc)
	return err != nil
}

// roleKeys lists the keys a layer can hold the role at the path under. A reviewer role is read
// from its historical names too (PM and QA fold into PMQA, CSA and Hacker into Security), and the
// raw layer still has them under those names.
func roleKeys(path []string) [][]string {
	if len(path) != 3 || path[0] != "reviewers" {
		return [][]string{path}
	}
	var keys [][]string
	for _, role := range ReviewRoleSourceNames(path[2]) {
		keys = append(keys, []string{path[0], path[1], role})
	}
	return keys
}
