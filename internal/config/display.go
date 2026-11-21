package config

import (
	"sort"
	"strings"
)

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
func displayWithout(cfg *Config, loadedScope, loadedOverlay map[string]any) (*Config, bool) {
	scope := cloneRawObjectDeep(loadedScope)
	var overlay map[string]any
	if loadedOverlay != nil {
		overlay = cloneRawObjectDeep(loadedOverlay)
	}
	// acceptsEmpty asks the loader, on the layers as they were loaded, whether the object at path
	// may be empty in the given layer (0 the scope file, 1 the overlay).
	acceptsEmpty := func(index int, path []string) bool {
		probeScope, probeOverlay := cloneRawObjectDeep(loadedScope), loadedOverlay
		if loadedOverlay != nil {
			probeOverlay = cloneRawObjectDeep(loadedOverlay)
		}
		probe := probeScope
		if index == 1 {
			probe = probeOverlay
		}
		OverlaySet(probe, map[string]any{}, path...)
		_, _, err := validateLayers(probeScope, probeOverlay)
		return err == nil
	}
	// kept lists the objects a removal left empty because the loader takes them empty in their
	// own layer. The merge of two layers can still be empty where each alone was not (a tui
	// section of only unknown keys in both), so mergeTidy prunes them if the merge fails.
	var kept []keptEmpty
	layers := []map[string]any{scope, overlay}
	remove := func(path ...string) {
		for i, layer := range layers {
			if layer == nil {
				continue
			}
			for _, parent := range deleteAndTidy(layer, path, func(parent []string) bool { return acceptsEmpty(i, parent) }) {
				kept = append(kept, keptEmpty{layer: i, path: parent})
			}
		}
	}
	merge := func() (*Config, error) {
		cfg, _, err := validateLayers(scope, overlay)
		return cfg, err
	}
	mergeTidy := func() (*Config, error) {
		cfg, err := merge()
		if err == nil {
			return cfg, nil
		}
		for _, group := range keptGroups(kept) {
			undo := pruneEmpty(layers, group)
			if pruned, mergeErr := merge(); mergeErr == nil {
				return pruned, nil
			}
			undo()
		}
		return nil, err
	}
	// An unusable entry goes first: with it gone, a built-in agent is known again and its
	// models.* entries are settings, not ignored content. What is still ignored after that is
	// removed until nothing is left; each round removes what the last one reported.
	for _, item := range cfg.Ignored {
		if item.Kind == IgnoredUnusableAgent {
			remove(item.Path...)
		}
	}
	display, err := mergeTidy()
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
		display, err = mergeTidy()
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
							deleteAndTidy(layer, key, func(parent []string) bool { return acceptsEmpty(i, parent) })
						}
					}
				}
			}
		}
		if display, err = mergeTidy(); err != nil {
			return nil, false
		}
	}
	return display, true
}

// keptEmpty is an object a removal left empty in one layer (0 the scope file, 1 the overlay).
type keptEmpty struct {
	layer int
	path  []string
}

// keptGroups lists the distinct paths of the kept objects, deepest first.
func keptGroups(kept []keptEmpty) [][]string {
	var groups [][]string
	seen := map[string]bool{}
	for _, item := range kept {
		if key := strings.Join(item.path, "\x00"); !seen[key] {
			seen[key] = true
			groups = append(groups, item.path)
		}
	}
	sort.SliceStable(groups, func(i, j int) bool { return len(groups[i]) > len(groups[j]) })
	return groups
}

// pruneEmpty removes the object at path from every layer where it is empty, and returns the
// function that puts them back.
func pruneEmpty(layers []map[string]any, path []string) (undo func()) {
	var restore []func()
	for _, layer := range layers {
		if layer == nil {
			continue
		}
		parent := layer
		for _, key := range path[:len(path)-1] {
			next, ok := parent[key].(map[string]any)
			if !ok {
				parent = nil
				break
			}
			parent = next
		}
		last := path[len(path)-1]
		if object, ok := parent[last].(map[string]any); parent != nil && ok && len(object) == 0 {
			delete(parent, last)
			restore = append(restore, func() { parent[last] = map[string]any{} })
		}
	}
	return func() {
		for _, put := range restore {
			put()
		}
	}
}

// deleteAndTidy removes the key the path names, when the layer has it, and returns the parent
// objects it left empty and kept. A parent that this leaves empty stays when accepts says the
// loader takes it empty (the required reviewers object, an empty program_status that declares no
// status reports) and goes when it does not (a tui section of only unknown keys). A layer that
// did not hold the key is left as it is.
func deleteAndTidy(obj map[string]any, path []string, accepts func(parent []string) bool) (kept [][]string) {
	chain := []map[string]any{obj}
	for _, key := range path[:len(path)-1] {
		next, ok := chain[len(chain)-1][key].(map[string]any)
		if !ok {
			return nil
		}
		chain = append(chain, next)
	}
	last := path[len(path)-1]
	if _, ok := chain[len(chain)-1][last]; !ok {
		return nil
	}
	delete(chain[len(chain)-1], last)
	for depth := len(chain) - 1; depth > 0 && len(chain[depth]) == 0; depth-- {
		if !accepts(path[:depth]) {
			delete(chain[depth-1], path[depth-1])
			continue
		}
		kept = append(kept, append([]string(nil), path[:depth]...))
	}
	return kept
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
