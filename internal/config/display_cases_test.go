package config

import "fmt"

// The helpers of the generated display test that state and check what a combination holds
// (display_test.go holds the tables and the generator).

// declared returns what the dirty layers of a combination do not hold of what the combination
// states about itself: the content of every placement, and the shape of P in each layer.
func declared(c combination, base baseScope) string {
	holds := func(l layers, scope bool, k kind, content map[string]any) bool {
		layer := l.scope
		if !scope {
			layer = l.overlay
		}
		if layer == nil {
			return len(content) == 0
		}
		p := parentObject(layer, k.parent)
		if p == nil {
			return false
		}
		for key := range content {
			if _, ok := p[key]; !ok {
				return false
			}
		}
		return true
	}
	for _, p := range c.items {
		for _, scope := range p.at.layers() {
			if !holds(c.dirty, scope, p.kind, p.kind.add) {
				return fmt.Sprintf("the dirty %s does not hold the content of %s", layerName(scope), p)
			}
		}
	}
	if c.part != "parent states" {
		return ""
	}
	k, sp := c.kind, parents[c.kind.parentKey()]
	for _, scope := range []bool{true, false} {
		st := c.overlay
		if scope {
			st = c.scope
		}
		layer := c.dirty.scope
		if !scope {
			layer = c.dirty.overlay
		}
		var p map[string]any
		if layer != nil {
			p = parentObject(layer, k.parent)
		}
		ok := true
		switch st {
		case absent:
			ok = len(k.parent) == 0 && !scope && layer == nil || len(k.parent) > 0 && p == nil
		case empty:
			ok = p != nil && len(p) == 0 && (len(k.parent) > 0 || !scope)
		case otherOnly:
			other := sp.other
			if other == nil {
				other = map[string]any{"other_future": 1}
			}
			ok = holds(c.dirty, scope, k, other)
		case known:
			ok = holds(c.dirty, scope, k, base.known(sp, k.parent, scope))
		case only:
			ok = holds(c.dirty, scope, k, k.add)
		case withKnown:
			known := base.known(sp, k.parent, scope)
			if k.known != nil {
				known = k.known
			}
			// The known content must be a content of its own, not the key the placement wrote.
			own := map[string]any{}
			for key, value := range known {
				if _, written := k.add[key]; !written {
					own[key] = value
				}
			}
			ok = holds(c.dirty, scope, k, k.add) && len(own) > 0 && holds(c.dirty, scope, k, own)
		}
		if !ok {
			return fmt.Sprintf("the dirty %s does not hold P in the shape %s", layerName(scope), st)
		}
	}
	return ""
}

func layerName(scope bool) string {
	if scope {
		return "scope"
	}
	return "overlay"
}

// fixed returns the named combinations of the findings of the review runs of the card
// 20261121-config-forward-compat-v2-task: the four inputs of PMQA-003 (one per run, pmqa-1 to
// pmqa-4) and the inputs of PMQA-011, PMQA-013, PMQA-014 and PMQA-015.
func fixed(base baseScope) []combination {
	type named struct {
		name       string
		dirty, ref layers
	}
	scopeOf := func(edit func(doc map[string]any)) map[string]any {
		doc := cloneMap(base.doc)
		edit(doc)
		return doc
	}
	set := func(value any, path ...string) func(map[string]any) {
		return func(doc map[string]any) { OverlaySet(doc, cloneRawValue(value), path...) }
	}
	del := func(path ...string) func(map[string]any) {
		return func(doc map[string]any) { deleteParent(doc, path) }
	}
	both := func(edits ...func(map[string]any)) func(map[string]any) {
		return func(doc map[string]any) {
			for _, edit := range edits {
				edit(doc)
			}
		}
	}
	unusableCodex := map[string]any{"args": map[string]any{"start": []any{}, "resume": []any{}}, "session": map[string]any{"mode": "hook:future-hook"}}
	var out []combination
	for _, n := range []named{
		{"PMQA-003 pmqa-1: an unknown role reference and an unusable built-in agent",
			layers{scope: scopeOf(both(set("futureagent", "chat_agent"), set("futureagent", "kanban_agents", "small"), set(unusableCodex, "agents", "codex")))},
			layers{scope: scopeOf(both(del("chat_agent"), del("kanban_agents", "small")))}},
		{"PMQA-003 pmqa-2: an unknown reference in the overlay takes the value the scope gives",
			layers{scope: scopeOf(set("claude", "kanban_agents", "small")), overlay: map[string]any{"kanban_agents": map[string]any{"small": "futureagent"}}},
			layers{scope: scopeOf(set("claude", "kanban_agents", "small")), overlay: map[string]any{}}},
		{"PMQA-003 pmqa-3: an unknown kanban_agent in the overlay and a legacy reviewer role",
			layers{scope: scopeOf(set(map[string]any{"PM": "futureagent"}, "reviewers")), overlay: map[string]any{"kanban_agent": "futureagent"}},
			layers{scope: scopeOf(set(map[string]any{}, "reviewers")), overlay: map[string]any{}}},
		{"PMQA-003 pmqa-4: a legacy reviewer role and a tui section of only unknown keys",
			layers{scope: scopeOf(both(set(map[string]any{"PM": "futureagent"}, "reviewers"), set(map[string]any{"future": 1}, "tui")))},
			layers{scope: scopeOf(both(set(map[string]any{}, "reviewers"), del("tui")))}},
		{"PMQA-011: a reviewers object of only roles of a newer version",
			layers{scope: scopeOf(set(map[string]any{"large": map[string]any{"FutureRole": "codex"}}, "reviewers"))},
			layers{scope: scopeOf(set(map[string]any{"large": map[string]any{}}, "reviewers"))}},
		{"PMQA-013: an unknown key in a program_status the scope declares empty",
			layers{scope: scopeOf(set(map[string]any{}, "agents", "codex", "program_status")), overlay: map[string]any{"agents": map[string]any{"codex": map[string]any{"program_status": map[string]any{"future_key": 1}}}}},
			layers{scope: scopeOf(set(map[string]any{}, "agents", "codex", "program_status")), overlay: map[string]any{}}},
		{"PMQA-014: a tui section of only unknown keys under an empty overlay tui, and an unknown kanban_agents.small",
			layers{scope: scopeOf(both(set("codex", "kanban_agent"), set("futureagent", "kanban_agents", "small"), set(map[string]any{"future": 1}, "tui"))), overlay: map[string]any{"tui": map[string]any{}, "language": "en"}},
			layers{scope: scopeOf(both(set("codex", "kanban_agent"), del("kanban_agents", "small"), del("tui"))), overlay: map[string]any{"language": "en"}}},
		{"PMQA-015: a built-in agents entry of only unknown keys",
			layers{scope: scopeOf(set(map[string]any{"codex": map[string]any{"future_key": 1}}, "agents"))},
			layers{scope: scopeOf(set(map[string]any{}, "agents"))}},
	} {
		out = append(out, combination{name: n.name, part: "fixed", dirty: n.dirty, ref: n.ref})
	}
	return out
}
