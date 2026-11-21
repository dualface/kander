package config

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// The tests of this file pin one property of the display of kander config (LoadWithDisplay):
// for any two layers (the scope file and the project overlay), the display equals the
// configuration the loader gives for the reference documents, the same layers without the
// content this version ignores (docs/config-forward-compat.md, "The reference configuration").
// Every combination declares its reference documents in the tables below, from the rules of that
// page; none of them is produced by the display code.

// layers are the two raw documents of one combination. The overlay is nil when there is no file.
type layers struct {
	scope, overlay map[string]any
}

// object returns the layer: the scope document, or the overlay (created when there is none).
func (l *layers) object(scope bool) map[string]any {
	if scope {
		return l.scope
	}
	if l.overlay == nil {
		l.overlay = map[string]any{}
	}
	return l.overlay
}

// loadLayers is the loader on the documents of two layers: the scope document is validated on
// its own, then merged with the overlay and validated again, as Load does for the two files.
func loadLayers(l layers) (*Config, error) {
	scopeConfig, err := Validate(cloneRawObjectDeep(l.scope))
	if err != nil {
		return nil, err
	}
	docs := layerDocuments{scopeConfig: scopeConfig, scope: l.scope, overlay: l.overlay}
	loaded, _, err := docs.effective()
	return loaded, err
}

// displayLayers is the code under test.
func displayLayers(l layers) (*Config, error) {
	loaded, err := loadLayers(l)
	if err != nil {
		return nil, err
	}
	scopeConfig, _ := Validate(cloneRawObjectDeep(l.scope))
	docs := layerDocuments{scopeConfig: scopeConfig, scope: l.scope, overlay: l.overlay}
	return docs.reference(loaded)
}

// parentSpec describes P, the object that directly holds the unknown content of a kind.
type parentSpec struct {
	// known is the content P holds in a layer when it holds known settings: the scope's value is
	// the one of the default document unless it is set here; the overlay's is sparse.
	known map[bool]map[string]any
	// other is another unknown content of P (one that no kind adds).
	other map[string]any
	// dropOnlyUnknown: an object of an agents entry (the entry itself, its program_status) that
	// held only unknown keys is absent from that layer, and so is an entry left with nothing;
	// an object the layer wrote as {} stays.
	dropOnlyUnknown bool
	// requiresKeys: the loader does not accept the section empty or partial (tui, session). When
	// no layer of the reference holds a known key of it, it is absent from both layers.
	requiresKeys bool
	// partialOverlay: the known content of the overlay is a part of the section, which the
	// loader accepts only on top of a complete one in the scope file.
	partialOverlay bool
	// scopeRequired: the scope file cannot omit P.
	scopeRequired bool
}

var unusableEntry = map[string]any{"path": "sh", "dialect": "futuredialect"}

var parents = map[string]parentSpec{
	"":      {known: map[bool]map[string]any{false: {"language": "en"}}, scopeRequired: true},
	"tui":   {known: map[bool]map[string]any{false: {"refresh": 45}}, requiresKeys: true, partialOverlay: true},
	"rules": {known: map[bool]map[string]any{false: {"reporting": false}}},
	"git":   {known: map[bool]map[string]any{false: {"branch_mode": "main-only"}}},
	"issue": {known: map[bool]map[string]any{false: {"result_on_done": "off"}}},

	"kanban_agents": {known: map[bool]map[string]any{false: {"large": "claude"}}},
	"reviewers":     {known: map[bool]map[string]any{false: {"large": map[string]any{"PMQA": "claude"}}}, scopeRequired: true},
	"reviewers.large": {
		// Not PMQA: the kinds that name an unknown agent write that role.
		known: map[bool]map[string]any{false: {"Security": "claude"}},
	},
	"review_stages.small": {known: map[bool]map[string]any{false: {"PMQA": "skip"}}},

	"models":                   {known: map[bool]map[string]any{false: {"kanban": map[string]any{"codex": map[string]any{"large_model": "m"}}}}},
	"models.kanban":            {known: map[bool]map[string]any{false: {"codex": map[string]any{"large_model": "m"}}}},
	"models.review":            {known: map[bool]map[string]any{false: {"codex": map[string]any{"effort": "low"}}}},
	"models.chat":              {known: map[bool]map[string]any{false: {"codex": map[string]any{"effort": "low"}}}},
	"models.kanban.codex":      {known: map[bool]map[string]any{false: {"large_model": "m"}}},
	"models.review.codex":      {known: map[bool]map[string]any{false: {"effort": "low"}}},
	"models.chat.codex":        {known: map[bool]map[string]any{false: {"effort": "low"}}},
	"models.review_roles.PMQA": {known: map[bool]map[string]any{false: {"effort": "low"}}},

	"agents": {
		known: map[bool]map[string]any{true: {"helper": customAgent()}, false: {"helper2": customAgent()}},
		other: map[string]any{"otherbad": unusableEntry},
	},
	"agents.custom":                 {known: map[bool]map[string]any{true: customAgent(), false: {"process_name": "p"}}},
	"agents.custom.session":         {known: map[bool]map[string]any{true: {"mode": "generated"}, false: {"mode": "generated"}}, dropOnlyUnknown: true, requiresKeys: true},
	"agents.custom.session.file":    {known: map[bool]map[string]any{true: {"format": "jsonl_header", "id_field": "id"}, false: {"format": "jsonl_header", "id_field": "id"}}, dropOnlyUnknown: true, requiresKeys: true},
	"agents.codex.session":          {requiresKeys: true},
	"agents.codex.session.file":     {known: map[bool]map[string]any{true: {"format": "jsonl_header", "id_field": "id"}, false: {"format": "jsonl_header", "id_field": "id"}}, dropOnlyUnknown: true, requiresKeys: true},
	"agents.custom.args":            {known: map[bool]map[string]any{true: {"start": []any{}, "resume": []any{}}, false: {"start": []any{}, "resume": []any{}}}, dropOnlyUnknown: true, requiresKeys: true},
	"agents.custom.prompt_delivery": {known: map[bool]map[string]any{true: {"mode": "argv"}, false: {"mode": "argv"}}, dropOnlyUnknown: true},
	"agents.codex":                  {known: map[bool]map[string]any{true: {"process_name": "codex"}, false: {"process_name": "codex"}}, dropOnlyUnknown: true},
	"agents.claude.program_status": {
		known:           map[bool]map[string]any{true: {"reports": true, "app": "claude-code"}, false: {"reports": true, "app": "claude-code"}},
		dropOnlyUnknown: true,
	},

	"server":  {known: map[bool]map[string]any{true: {"url": "http://localhost:1"}, false: {"url": "http://localhost:1"}}},
	"project": {known: map[bool]map[string]any{true: {"name": "demo"}, false: {"name": "demo"}}},
}

// kind is one sort of content of a newer version, as it sits in P.
type kind struct {
	name string
	// parent is the path of P, nil for the top level of the document.
	parent []string
	// add is the content put into P.
	add map[string]any
	// refAdd is what the reference holds in P in the layer in place of add: nothing for most
	// kinds. Only the scope has a required kanban_agent, which falls back to the default agent.
	refAdd func(scope bool) map[string]any
	// only: the kind makes P hold nothing but the content (a section of only unknown keys).
	only bool
	// setting: the content is a known setting that stays in the reference.
	setting bool
	// known replaces the known content of P in both layers (the flat form of reviewers).
	known map[string]any
	// setup adds what the kind needs in the scope document of both sides of a combination; kinds
	// with the same setupKey share one setup, which runs once.
	setup    func(scope map[string]any)
	setupKey string
	// unusable names the agents entry the content makes unusable: the reference holds that entry
	// in no layer.
	unusable string
	// reviewers names the form of the reviewers object the kind writes: "flat" (a legacy role
	// object) or "scale" (a scale object); a flat and a scale form cannot share a layer.
	reviewers string
	// scaleInScope: the kind writes next to the scale object of the scope document only (an
	// unknown key of reviewers, which is no scale and no role in the overlay).
	scaleInScope bool
}

func (k kind) parentKey() string { return strings.Join(k.parent, ".") }

func kindAt(name string, parent string, add map[string]any) kind {
	var path []string
	if parent != "" {
		path = strings.Split(parent, ".")
	}
	return kind{name: name, parent: path, add: add}
}

func withReviewers(k kind, form string) kind { k.reviewers = form; return k }

func kinds() []kind {
	withCustom := func(k kind) kind {
		k.setupKey = "custom"
		k.setup = func(scope map[string]any) { OverlaySet(scope, customAgent(), "agents", "custom") }
		return k
	}
	// The scope file holds the entry of the built-in agent next to which the content sits.
	withBuiltin := func(k kind) kind {
		k.setupKey = "builtin"
		k.setup = func(scope map[string]any) {
			if _, ok := OverlayGet(scope, "agents", "codex"); !ok {
				OverlaySet(scope, map[string]any{"process_name": "codex"}, "agents", "codex")
			}
		}
		return k
	}
	scaleInScope := func(k kind) kind { k.scaleInScope = true; return k }
	unusableKind := func(k kind, name string) kind { k.unusable = name; return k }
	only := func(k kind) kind { k.only = true; return k }
	roleDefault := func(scope bool) map[string]any {
		if scope {
			return map[string]any{"kanban_agent": defaultAgentName()}
		}
		return nil
	}
	kanbanAgent := kindAt("kanban_agent names an unknown agent", "", map[string]any{"kanban_agent": "futureagent"})
	kanbanAgent.refAdd = roleDefault
	flatKnown := map[string]any{"Security": "codex"}
	legacy := withReviewers(only(kindAt("reviewers PM names an unknown agent, QA a known one", "reviewers", map[string]any{"PM": "futureagent", "QA": "codex"})), "flat")
	legacy.refAdd = func(bool) map[string]any { return map[string]any{"QA": "codex"} }
	legacy.known = flatKnown
	legacyPM := withReviewers(only(kindAt("reviewers PM names an unknown agent", "reviewers", map[string]any{"PM": "futureagent"})), "flat")
	legacyPM.known = flatKnown
	emptyStatus := kindAt("an empty program_status", "agents.claude.program_status", map[string]any{})
	emptyStatus.setting = true
	return []kind{
		kindAt("a top-level key", "", map[string]any{"future_top": map[string]any{"enabled": true}}),
		kindAt("an unknown key in tui", "tui", map[string]any{"future": "x"}),
		kindAt("an unknown key in rules", "rules", map[string]any{"future_module": true}),
		kindAt("an unknown key in git", "git", map[string]any{"future": "x"}),
		kindAt("an unknown key in issue", "issue", map[string]any{"future": "x"}),
		kindAt("an unknown scale in kanban_agents", "kanban_agents", map[string]any{"future_scale": "codex"}),
		withReviewers(kindAt("an unknown role in reviewers.large", "reviewers.large", map[string]any{"FutureRole": "codex"}), "scale"),
		scaleInScope(kindAt("an unknown scale in reviewers", "reviewers", map[string]any{"future_scale": map[string]any{"PMQA": "codex"}})),
		kindAt("an unknown role in review_stages.small", "review_stages.small", map[string]any{"FutureRole": "auto"}),
		kindAt("an unknown section in models", "models", map[string]any{"future_section": map[string]any{"a": "b"}}),
		kindAt("an unknown field in models.kanban.codex", "models.kanban.codex", map[string]any{"future_field": "x"}),
		kindAt("an unknown field in models.review.codex", "models.review.codex", map[string]any{"future_field": "x"}),
		kindAt("an unknown field in models.chat.codex", "models.chat.codex", map[string]any{"future_field": "x"}),
		kindAt("an unknown field in models.review_roles.PMQA", "models.review_roles.PMQA", map[string]any{"future_field": "x"}),
		withCustom(kindAt("an unknown key in a custom agent", "agents.custom", map[string]any{"future_key": "x"})),
		withCustom(kindAt("an unknown key in the session of a custom agent", "agents.custom.session", map[string]any{"future_session_key": "x"})),
		withCustom(kindAt("an unknown key in the session file of a custom agent", "agents.custom.session.file", map[string]any{"future_format": "transcript-v2"})),
		only(withBuiltin(kindAt("an unknown key in the session file of a built-in agent", "agents.codex.session.file", map[string]any{"future_format": "transcript-v2"}))),
		withCustom(kindAt("an unknown key in the args of a custom agent", "agents.custom.args", map[string]any{"future_arg": 1})),
		withCustom(kindAt("an unknown key in the prompt delivery of a custom agent", "agents.custom.prompt_delivery", map[string]any{"future_key": 1})),
		emptyStatus,
		kindAt("an unknown key in program_status", "agents.claude.program_status", map[string]any{"future_key": 1}),
		kindAt("an unknown key in server", "server", map[string]any{"token": "t"}),
		kindAt("an unknown key in project", "project", map[string]any{"extra": true}),
		kindAt("a models.kanban entry of an unknown agent", "models.kanban", map[string]any{"future_agent": map[string]any{"large_model": "m"}}),
		kindAt("a models.review entry of an unknown agent", "models.review", map[string]any{"future_agent": map[string]any{"model": "m"}}),
		kindAt("a models.chat entry of an unknown agent", "models.chat", map[string]any{"future_agent": map[string]any{"model": "m"}}),
		unusableKind(kindAt("an agents entry this version cannot use", "agents", map[string]any{"badagent": unusableEntry}), "badagent"),
		unusableKind(kindAt("an unusable agents entry of a built-in agent", "agents", map[string]any{"codex": map[string]any{
			"args": map[string]any{"start": []any{}, "resume": []any{}}, "session": map[string]any{"mode": "hook:future-hook"},
		}}), "codex"),
		kanbanAgent,
		kindAt("kanban_agents.small names an unknown agent", "kanban_agents", map[string]any{"small": "futureagent"}),
		kindAt("chat_agent names an unknown agent", "", map[string]any{"chat_agent": "futureagent"}),
		legacy,
		legacyPM,
		withReviewers(kindAt("reviewers.large.PMQA names an unknown agent", "reviewers.large", map[string]any{"PMQA": "futureagent"}), "scale"),
		only(kindAt("a tui section of only unknown keys", "tui", map[string]any{"future": 1})),
		withReviewers(only(kindAt("a reviewers.large of only unknown roles", "reviewers.large", map[string]any{"FutureRole": "codex"})), "scale"),
		only(kindAt("a built-in agents entry of only unknown keys", "agents.codex", map[string]any{"future_key": 1})),
	}
}

// layerSet is where a placement puts its content.
type layerSet int

const (
	inScope layerSet = iota
	inOverlay
	inBoth
)

func (s layerSet) String() string { return [...]string{"scope", "overlay", "both"}[s] }

func (s layerSet) layers() []bool {
	switch s {
	case inScope:
		return []bool{true}
	case inOverlay:
		return []bool{false}
	}
	return []bool{true, false}
}

// placement is a kind in a position.
type placement struct {
	kind kind
	at   layerSet
}

func (p placement) String() string { return p.kind.name + " (" + p.at.String() + ")" }

func placements() []placement {
	var out []placement
	for _, k := range kinds() {
		for _, at := range []layerSet{inScope, inOverlay, inBoth} {
			out = append(out, placement{k, at})
		}
	}
	return out
}

// shape of P in a layer.
type shape int

const (
	absent    shape = iota // P is not in the layer
	empty                  // P is {}
	otherOnly              // P holds another unknown content only
	known                  // P holds known settings
	only                   // P holds the placement's content only
	withKnown              // P holds known settings and the placement's content
)

func (s shape) String() string {
	return [...]string{"absent", "empty", "other-only", "known", "only", "with-known"}[s]
}

// baseScope is the scope document every combination starts from.
type baseScope struct{ doc map[string]any }

func (b baseScope) known(sp parentSpec, path []string, scope bool) map[string]any {
	if content, ok := sp.known[scope]; ok {
		return content
	}
	if len(path) == 0 {
		if scope {
			return b.doc
		}
		return nil
	}
	if value, ok := OverlayGet(b.doc, path...); ok {
		if m, ok := value.(map[string]any); ok {
			return m
		}
	}
	return map[string]any{}
}

// parentObject returns P in the layer, or nil when the layer has none; the top level is the layer.
func parentObject(layer map[string]any, path []string) map[string]any {
	if len(path) == 0 {
		return layer
	}
	value, ok := OverlayGet(layer, path...)
	if !ok {
		return nil
	}
	m, _ := value.(map[string]any)
	return m
}

func setParent(layer map[string]any, path []string, content map[string]any) {
	if len(path) == 0 {
		for key, value := range content {
			layer[key] = cloneRawValue(value)
		}
		return
	}
	OverlaySet(layer, cloneRawValue(content), path...)
}

func deleteParent(layer map[string]any, path []string) {
	if len(path) == 0 {
		return
	}
	if parent := parentObject(layer, path[:len(path)-1]); parent != nil {
		delete(parent, path[len(path)-1])
	}
}

// dropAbsentObject removes P from a reference layer under the dropEmptyEntry rule, and with it
// the agents entry above it when that holds nothing else.
func dropAbsentObject(layer map[string]any, path []string) {
	deleteParent(layer, path)
	if len(path) > 2 && path[0] == "agents" {
		if entry := parentObject(layer, path[:2]); entry != nil && len(entry) == 0 {
			deleteParent(layer, path[:2])
		}
	}
}

func cloneMap(m map[string]any) map[string]any { return cloneRawObjectDeep(m) }

// place puts the content of the kind into P of one layer of both sides, in the given shape. The
// dirty side holds the content, the reference side what the rules of the page leave of it.
func place(c *combination, base baseScope, k kind, scope bool, st shape) {
	sp := parents[k.parentKey()]
	dirty, ref := &c.dirty, &c.ref
	dl, rl := dirty.object(scope), ref.object(scope)
	c.setUp(k)
	var held map[string]any
	switch st {
	case only:
		held = map[string]any{}
	case withKnown:
		held = cloneMap(base.known(sp, k.parent, scope))
		if k.known != nil {
			held = cloneMap(k.known)
		}
	}
	// P holds nothing but the content, so what the scope document held there is gone. A later
	// placement in the same P of the same layer, or in an object below it, adds to what the earlier
	// ones put there.
	touch := fmt.Sprint(scope, k.parentKey())
	first := !c.touched[touch]
	for i := 1; i <= len(k.parent); i++ {
		c.touched[fmt.Sprint(scope, strings.Join(k.parent[:i], "."))] = true
	}
	replaced := st == only && first
	dirtyP, refP := parentObject(dl, k.parent), parentObject(rl, k.parent)
	if len(k.parent) == 0 {
		if st == withKnown && !scope {
			setParent(dl, nil, held)
			setParent(rl, nil, held)
		}
		dirtyP, refP = dl, rl
	} else {
		// A kind with its own known content (the flat reviewers object) replaces the P of the
		// scope document, which holds the other form.
		fresh := replaced || (k.known != nil && first)
		if dirtyP == nil || fresh {
			dirtyP = cloneMap(held)
		}
		if refP == nil || fresh {
			refP = cloneMap(held)
		}
	}
	for key, value := range k.add {
		// Content that lands on an object an earlier placement wrote below adds to it.
		if existing, ok := dirtyP[key].(map[string]any); ok {
			if added, ok := value.(map[string]any); ok {
				dirtyP[key] = deepMerge(existing, added)
			} else {
				dirtyP[key] = cloneRawValue(value)
			}
		} else {
			dirtyP[key] = cloneRawValue(value)
		}
		if !k.setting {
			delete(refP, key)
		} else {
			refP[key] = cloneRawValue(value)
		}
	}
	if k.refAdd != nil {
		for key, value := range k.refAdd(scope) {
			refP[key] = cloneRawValue(value)
		}
	}
	if len(k.parent) == 0 {
		return
	}
	setParent(dl, k.parent, dirtyP)
	if len(refP) == 0 && !k.setting && sp.dropOnlyUnknown {
		dropAbsentObject(rl, k.parent)
		return
	}
	setParent(rl, k.parent, refP)
}

// placeOther gives P in the layer that holds none of the placement's content the shape st.
func placeOther(dirty, ref *layers, base baseScope, k kind, scope bool, st shape) {
	sp := parents[k.parentKey()]
	dl, rl := dirty.object(scope), ref.object(scope)
	switch st {
	case absent:
		if len(k.parent) == 0 {
			if !scope {
				dirty.overlay, ref.overlay = nil, nil
			}
			return
		}
		deleteParent(dl, k.parent)
		deleteParent(rl, k.parent)
	case empty:
		if len(k.parent) > 0 {
			setParent(dl, k.parent, map[string]any{})
			setParent(rl, k.parent, map[string]any{})
		}
	case otherOnly:
		other := sp.other
		if other == nil {
			other = map[string]any{"other_future": 1}
		}
		if len(k.parent) == 0 {
			if !scope {
				clear(dl)
				clear(rl)
				setParent(dl, nil, other)
			} else {
				setParent(dl, nil, other)
			}
			return
		}
		setParent(dl, k.parent, other)
		if sp.dropOnlyUnknown {
			dropAbsentObject(rl, k.parent)
		} else {
			setParent(rl, k.parent, map[string]any{})
		}
	case known:
		if len(k.parent) > 0 {
			content := cloneMap(base.known(sp, k.parent, scope))
			setParent(dl, k.parent, content)
			setParent(rl, k.parent, content)
		} else if !scope {
			setParent(dl, nil, sp.known[false])
			setParent(rl, nil, sp.known[false])
		}
	}
}

// settle applies the rule of a section the loader does not accept empty: when no layer of the
// reference holds a known key of it, the section is absent from both layers.
func settle(ref *layers, k kind) {
	if k.unusable != "" {
		for _, layer := range []map[string]any{ref.scope, ref.overlay} {
			if layer != nil {
				deleteParent(layer, []string{"agents", k.unusable})
			}
		}
	}
	sp := parents[k.parentKey()]
	if !sp.requiresKeys {
		return
	}
	for _, layer := range []map[string]any{ref.scope, ref.overlay} {
		if layer == nil {
			continue
		}
		if p := parentObject(layer, k.parent); len(p) > 0 {
			return
		}
	}
	for _, layer := range []map[string]any{ref.scope, ref.overlay} {
		if layer == nil {
			continue
		}
		deleteParent(layer, k.parent)
		// A section that held nothing but the one that went out is empty too, and goes when it
		// requires keys itself (the session of an agent).
		for i := len(k.parent) - 1; i > 0; i-- {
			path := k.parent[:i]
			if p := parentObject(layer, path); p != nil && len(p) == 0 && parents[strings.Join(path, ".")].requiresKeys {
				deleteParent(layer, path)
			}
		}
	}
}

// combination is one input of the property with its reference, and what it declares about
// itself for the predicates of the combinations the loader rejects.
type combination struct {
	name       string
	part       string
	dirty, ref layers
	// items are the placements of a pairwise combination.
	items []placement
	// kind is the placement of a parent-state combination.
	kind kind
	// parent, scope and overlay describe a parent-state combination: P and its shape in each layer.
	parent         string
	scope, overlay shape
	// touched lists the P of each layer a placement has written to.
	touched map[string]bool
}

// setUp adds what a kind needs in the scope document of both sides, once, before any placement of
// the combination writes next to it.
func (c *combination) setUp(k kind) {
	if key := "setup " + k.setupKey; k.setup != nil && !c.touched[key] {
		c.touched[key] = true
		k.setup(c.dirty.scope)
		k.setup(c.ref.scope)
	}
}

func newCombination(base baseScope, part, name string) combination {
	return combination{name: name, part: part, dirty: layers{scope: cloneMap(base.doc)}, ref: layers{scope: cloneMap(base.doc)}, touched: map[string]bool{}}
}

// shapeOf is the shape a placement in a layer takes in the pairwise part: the scope file holds
// the content next to its known settings, the overlay holds the content alone.
func shapeOf(k kind, scope bool) shape {
	if k.only || !scope {
		return only
	}
	return withKnown
}

// pairs: every unordered pair of placements, the first with the lower index (a placement with itself
// included); the content of the second is put after the one of the first.
func pairCombination(base baseScope, a, b placement) combination {
	c := newCombination(base, "pairs", a.String()+" + "+b.String())
	c.items = []placement{a, b}
	for _, p := range c.items {
		for _, scope := range p.at.layers() {
			place(&c, base, p.kind, scope, shapeOf(p.kind, scope))
		}
		settle(&c.ref, p.kind)
	}
	return c
}

// expressible says whether a layer can hold P in the shape: the scope file always has its
// required objects and keys.
func expressible(sp parentSpec, topLevel, scope bool, st shape) bool {
	switch {
	case !scope:
		return true
	case topLevel && (st == absent || st == empty || st == only):
		return false
	case st == absent && sp.scopeRequired:
		return false
	}
	return true
}

// layerShapes are the shapes P can take in one layer that the layer can express: the layer that
// holds the content of the placement holds only the content or the content next to known
// settings; a layer that holds none holds nothing, an empty object, another unknown content, or
// known settings.
func layerShapes(p placement, scope bool) []shape {
	sp := parents[p.kind.parentKey()]
	top := len(p.kind.parent) == 0
	candidates := []shape{absent, empty, otherOnly, known}
	if slices.Contains(p.at.layers(), scope) {
		candidates = []shape{only, withKnown}
	}
	return slices.DeleteFunc(candidates, func(st shape) bool { return !expressible(sp, top, scope, st) })
}

// parentCombinations: for one placement, every shape of P in the scope file times every shape of
// P in the overlay. A placement in both layers takes its two own-layer shapes independently, so a
// content that sits next to known settings in one layer can sit alone in the other.
func parentCombinations(base baseScope, p placement) []combination {
	var out []combination
	for _, scopeShape := range layerShapes(p, true) {
		for _, overlayShape := range layerShapes(p, false) {
			out = append(out, parentCombination(base, p, scopeShape, overlayShape))
		}
	}
	return out
}

// parentStateCount is the number of parent-state combinations of a placement, counted from the
// number of shapes each layer can express.
func parentStateCount(p placement) int {
	return len(layerShapes(p, true)) * len(layerShapes(p, false))
}

func parentCombination(base baseScope, p placement, scopeShape, overlayShape shape) combination {
	c := newCombination(base, "parent states", fmt.Sprintf("%s | scope=%s | overlay=%s", p, scopeShape, overlayShape))
	c.parent = p.kind.parentKey()
	c.kind, c.items = p.kind, []placement{p}
	c.setUp(p.kind)
	c.scope, c.overlay = scopeShape, overlayShape
	for _, scope := range []bool{true, false} {
		st := overlayShape
		if scope {
			st = scopeShape
		}
		if slices.Contains(p.at.layers(), scope) {
			place(&c, base, p.kind, scope, st)
		} else {
			placeOther(&c.dirty, &c.ref, base, p.kind, scope, st)
		}
	}
	settle(&c.ref, p.kind)
	return c
}

// decoded returns the layers as the files give them: numbers decoded as json.Number.
func (l layers) decoded(t *testing.T) layers {
	t.Helper()
	round := func(m map[string]any) map[string]any {
		if m == nil {
			return nil
		}
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := decodeJSON(data)
		if err != nil {
			t.Fatal(err)
		}
		return raw.(map[string]any)
	}
	return layers{scope: round(l.scope), overlay: round(l.overlay)}
}

func marshalConfig(t *testing.T, cfg *Config) string {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func newBase(t *testing.T) baseScope {
	t.Helper()
	doc := scopeDocument(t)
	defaultScope = cloneMap(doc)
	return baseScope{doc: doc}
}

// describe prints the layers; the scope document only where it differs from the default one.
func describe(l layers) string {
	changed := map[string]any{}
	for key, value := range l.scope {
		if want, ok := defaultScope[key]; !ok || !jsonEqual(want, value) {
			changed[key] = value
		}
	}
	for key := range defaultScope {
		if _, ok := l.scope[key]; !ok {
			changed[key] = "<removed>"
		}
	}
	scope, _ := json.Marshal(changed)
	overlay, _ := json.Marshal(l.overlay)
	return fmt.Sprintf("scope+%s overlay=%s", scope, overlay)
}

// defaultScope is the scope document the combinations start from, set by newBase.
var defaultScope map[string]any

func jsonEqual(a, b any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

// firstDifference shows the two texts around the first position where they differ.
func firstDifference(got, want string) string {
	i := 0
	for i < len(got) && i < len(want) && got[i] == want[i] {
		i++
	}
	from := max(i-60, 0)
	return fmt.Sprintf("...%s\n want ...%s", got[from:min(i+120, len(got))], want[from:min(i+120, len(want))])
}

// check runs one combination: it returns the error of the loader when the dirty layers do not
// load, and otherwise a description of what differs between the display and the reference.
func check(t *testing.T, c combination) (loadErr error, problem string) {
	t.Helper()
	dirty, ref := c.dirty.decoded(t), c.ref.decoded(t)
	if _, err := loadLayers(dirty); err != nil {
		return err, ""
	}
	want, err := loadLayers(ref)
	if err != nil {
		return nil, fmt.Sprintf("the reference does not load: %v\n reference %s", err, describe(c.ref))
	}
	got, err := displayLayers(dirty)
	if err != nil {
		return nil, fmt.Sprintf("the display fails: %v\n dirty %s", err, describe(c.dirty))
	}
	if g, w := marshalConfig(t, got), marshalConfig(t, want); g != w {
		return nil, fmt.Sprintf("display differs from the reference:\n got  %s\n dirty %s\n reference %s", firstDifference(g, w), describe(c.dirty), describe(c.ref))
	}
	return nil, ""
}

// unloadable names a kind of combination the loader rejects even without a display, by what the
// combination declares about itself; the test counts what each predicate matches and asserts
// that those combinations do not load, and that no other combination fails to load.
type unloadable struct {
	name  string
	match func(c combination) bool
	count int
}

// form is the form of the reviewers object a placement writes into one layer.
func form(p placement, scope bool) string {
	if p.kind.scaleInScope {
		if scope {
			return "scale"
		}
		return ""
	}
	return p.kind.reviewers
}

// mixesReviewers: one layer receives a flat and a scale form of the reviewers object.
func mixesReviewers(a, b placement) bool {
	for _, scope := range a.at.layers() {
		if !slices.Contains(b.at.layers(), scope) {
			continue
		}
		forms := []string{form(a, scope), form(b, scope)}
		if slices.Contains(forms, "flat") && slices.Contains(forms, "scale") {
			return true
		}
	}
	return false
}

var unloadables = []unloadable{
	{
		name: "the session of a built-in agent holds a file and no mode",
		match: func(c combination) bool {
			known := func(st shape) bool { return st == known || st == withKnown }
			return c.part == "parent states" && c.parent == "agents.codex.session.file" && (known(c.scope) || known(c.overlay))
		},
		count: 13,
	},
	{
		name: "a scale object and a flat legacy object of reviewers written into one layer",
		match: func(c combination) bool {
			return c.part == "pairs" && mixesReviewers(c.items[0], c.items[1])
		},
		count: 50,
	},
	{
		name: "the scope file holds an empty section the loader requires keys of",
		match: func(c combination) bool {
			return c.part == "parent states" && parents[c.parent].requiresKeys && c.scope == empty
		},
		count: 11,
	},
	{
		name: "the overlay holds part of a section the scope file does not hold",
		match: func(c combination) bool {
			sp := parents[c.parent]
			return c.part == "parent states" && sp.partialOverlay && slices.Contains([]shape{absent, otherOnly, only}, c.scope) && slices.Contains([]shape{known, withKnown}, c.overlay)
		},
		count: 8,
	},
}

// The size of the matrix. The generator computes each number; the constants keep it from
// shrinking unnoticed.
const (
	wantPlacements = 114
	wantPairs      = 6555
	wantParents    = 724
	wantFixed      = 8
)

func TestDisplayEqualsTheReferenceConfiguration(t *testing.T) {
	base := newBase(t)
	all := placements()
	var combinations []combination
	var pairs, parentStates, counted int
	for i := range all {
		for j := i; j < len(all); j++ {
			combinations = append(combinations, pairCombination(base, all[i], all[j]))
			pairs++
		}
	}
	for _, p := range all {
		counted += parentStateCount(p)
		for _, c := range parentCombinations(base, p) {
			combinations = append(combinations, c)
			parentStates++
		}
	}
	named := fixed(base)
	combinations = append(combinations, named...)
	if len(all) != wantPlacements || pairs != len(all)*(len(all)+1)/2 || pairs != wantPairs || parentStates != wantParents || counted != parentStates || len(named) != wantFixed {
		t.Fatalf("placements=%d (want %d) pairs=%d (want %d) parent states=%d (want %d) fixed=%d (want %d)",
			len(all), wantPlacements, pairs, wantPairs, parentStates, wantParents, len(named), wantFixed)
	}
	hits := make([]int, len(unloadables))
	failures := 0
	for _, c := range combinations {
		if missing := declared(c, base); missing != "" {
			t.Fatalf("%s: %s", c.name, missing)
		}
		loadErr, problem := check(t, c)
		matched := -1
		for i, u := range unloadables {
			if u.match(c) {
				matched = i
				break
			}
		}
		switch {
		case matched >= 0 && loadErr == nil:
			failures++
			if failures <= 20 {
				t.Errorf("%s: matches %q but loads", c.name, unloadables[matched].name)
			}
		case matched >= 0:
			hits[matched]++
		case loadErr != nil:
			failures++
			if failures <= 20 {
				t.Errorf("%s: the layers do not load, and no predicate says so: %v", c.name, loadErr)
			}
		case problem != "":
			failures++
			if failures <= 20 {
				t.Errorf("%s: %s", c.name, problem)
			}
		}
	}
	for i, u := range unloadables {
		if hits[i] != u.count {
			t.Errorf("predicate %q matches %d combinations, want %d", u.name, hits[i], u.count)
		}
	}
	if failures > 20 {
		t.Errorf("%d combinations failed in all", failures)
	}
}
