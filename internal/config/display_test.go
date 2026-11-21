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
		known: map[bool]map[string]any{false: {"PMQA": "claude"}},
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
	// setup adds what the kind needs in the scope document of both sides of a combination.
	setup func(scope map[string]any)
	// reviewers names the form of the reviewers object the kind writes: "flat" (a legacy role
	// object) or "scale" (a scale object); a flat and a scale form cannot share a layer.
	reviewers string
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
		k.setup = func(scope map[string]any) { OverlaySet(scope, customAgent(), "agents", "custom") }
		return k
	}
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
		kindAt("an unknown scale in reviewers", "reviewers", map[string]any{"future_scale": map[string]any{"PMQA": "codex"}}),
		kindAt("an unknown role in review_stages.small", "review_stages.small", map[string]any{"FutureRole": "auto"}),
		kindAt("an unknown section in models", "models", map[string]any{"future_section": map[string]any{"a": "b"}}),
		kindAt("an unknown field in models.kanban.codex", "models.kanban.codex", map[string]any{"future_field": "x"}),
		kindAt("an unknown field in models.review.codex", "models.review.codex", map[string]any{"future_field": "x"}),
		kindAt("an unknown field in models.chat.codex", "models.chat.codex", map[string]any{"future_field": "x"}),
		kindAt("an unknown field in models.review_roles.PMQA", "models.review_roles.PMQA", map[string]any{"future_field": "x"}),
		withCustom(kindAt("an unknown key in a custom agent", "agents.custom", map[string]any{"future_key": "x"})),
		withCustom(kindAt("an unknown key in the session of a custom agent", "agents.custom.session", map[string]any{"future_session_key": "x"})),
		withCustom(kindAt("an unknown key in the session file of a custom agent", "agents.custom.session.file", map[string]any{"future_format": "transcript-v2"})),
		withCustom(kindAt("an unknown key in the args of a custom agent", "agents.custom.args", map[string]any{"future_arg": 1})),
		withCustom(kindAt("an unknown key in the prompt delivery of a custom agent", "agents.custom.prompt_delivery", map[string]any{"future_key": 1})),
		emptyStatus,
		kindAt("an unknown key in program_status", "agents.claude.program_status", map[string]any{"future_key": 1}),
		kindAt("an unknown key in server", "server", map[string]any{"token": "t"}),
		kindAt("an unknown key in project", "project", map[string]any{"extra": true}),
		kindAt("a models.kanban entry of an unknown agent", "models.kanban", map[string]any{"future_agent": map[string]any{"large_model": "m"}}),
		kindAt("a models.review entry of an unknown agent", "models.review", map[string]any{"future_agent": map[string]any{"model": "m"}}),
		kindAt("a models.chat entry of an unknown agent", "models.chat", map[string]any{"future_agent": map[string]any{"model": "m"}}),
		kindAt("an agents entry this version cannot use", "agents", map[string]any{"badagent": unusableEntry}),
		kindAt("an unusable agents entry of a built-in agent", "agents", map[string]any{"codex": map[string]any{
			"args": map[string]any{"start": []any{}, "resume": []any{}}, "session": map[string]any{"mode": "hook:future-hook"},
		}}),
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
func place(dirty, ref *layers, base baseScope, k kind, scope bool, st shape) {
	sp := parents[k.parentKey()]
	dl, rl := dirty.object(scope), ref.object(scope)
	if k.setup != nil {
		k.setup(dirty.scope)
		k.setup(ref.scope)
	}
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
	// P holds nothing but the content, so what P held before is gone.
	replaced := st == only
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
		fresh := replaced || k.known != nil
		if dirtyP == nil || fresh {
			dirtyP = cloneMap(held)
		}
		if refP == nil || fresh {
			refP = cloneMap(held)
		}
	}
	for key, value := range k.add {
		dirtyP[key] = cloneRawValue(value)
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
		if layer != nil {
			deleteParent(layer, k.parent)
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
	// parent, scope and overlay describe a parent-state combination: P and its shape in each layer.
	parent         string
	scope, overlay shape
}

func newCombination(base baseScope, part, name string) combination {
	return combination{name: name, part: part, dirty: layers{scope: cloneMap(base.doc)}, ref: layers{scope: cloneMap(base.doc)}}
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
			place(&c.dirty, &c.ref, base, p.kind, scope, shapeOf(p.kind, scope))
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

// parentCombinations: for one placement, P of the other layer takes every shape that layer can
// express, and P of the layer that holds the content holds only the content, or the content
// next to known settings. A placement in both layers has no other layer: both take the shape.
func parentCombinations(base baseScope, p placement) []combination {
	k := p.kind
	sp := parents[k.parentKey()]
	top := len(k.parent) == 0
	var out []combination
	for _, this := range []shape{only, withKnown} {
		if !slices.ContainsFunc(p.at.layers(), func(scope bool) bool { return !expressible(sp, top, scope, this) }) {
			others := []shape{absent, empty, otherOnly, known}
			if p.at == inBoth {
				others = []shape{-1}
			}
			for _, other := range others {
				if other >= 0 && !expressible(sp, top, p.at == inOverlay, other) {
					continue
				}
				out = append(out, parentCombination(base, p, this, other))
			}
		}
	}
	return out
}

// parentStateCount is the number of parent-state combinations of a placement, counted from the
// shapes alone: the shapes of the layer that holds the content, times the shapes of the other.
func parentStateCount(p placement) int {
	k := p.kind
	sp := parents[k.parentKey()]
	top := len(k.parent) == 0
	count := 0
	for _, this := range []shape{only, withKnown} {
		if slices.ContainsFunc(p.at.layers(), func(scope bool) bool { return !expressible(sp, top, scope, this) }) {
			continue
		}
		if p.at == inBoth {
			count++
			continue
		}
		for _, other := range []shape{absent, empty, otherOnly, known} {
			if expressible(sp, top, p.at == inOverlay, other) {
				count++
			}
		}
	}
	return count
}

func parentCombination(base baseScope, p placement, this, other shape) combination {
	name := fmt.Sprintf("%s | this=%s", p, this)
	if other >= 0 {
		name += " | other=" + other.String()
	}
	c := newCombination(base, "parent states", name)
	c.parent = p.kind.parentKey()
	c.scope, c.overlay = this, this
	for _, scope := range p.at.layers() {
		place(&c.dirty, &c.ref, base, p.kind, scope, this)
	}
	if other >= 0 {
		otherScope := p.at == inOverlay
		placeOther(&c.dirty, &c.ref, base, p.kind, otherScope, other)
		if otherScope {
			c.scope = other
		} else {
			c.overlay = other
		}
	}
	settle(&c.ref, p.kind)
	return c
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

func sharesLayer(a, b placement) bool {
	return slices.ContainsFunc(a.at.layers(), func(scope bool) bool { return slices.Contains(b.at.layers(), scope) })
}

var unloadables = []unloadable{
	{
		name: "a scale object of reviewers written into the layer of a flat legacy reviewers object",
		match: func(c combination) bool {
			return c.part == "pairs" && c.items[0].kind.reviewers == "flat" && c.items[1].kind.reviewers == "scale" && sharesLayer(c.items[0], c.items[1])
		},
		count: 28,
	},
	{
		name: "the scope file holds an empty section the loader requires keys of",
		match: func(c combination) bool {
			return c.part == "parent states" && parents[c.parent].requiresKeys && c.scope == empty
		},
		count: 10,
	},
	{
		name: "the overlay holds part of a section the scope file does not hold",
		match: func(c combination) bool {
			sp := parents[c.parent]
			return c.part == "parent states" && sp.partialOverlay && slices.Contains([]shape{absent, otherOnly, only}, c.scope) && slices.Contains([]shape{known, withKnown}, c.overlay)
		},
		count: 6,
	},
}

// The size of the matrix. The generator computes each number; the constants keep it from
// shrinking unnoticed.
const (
	wantPlacements = 111
	wantPairs      = 6216
	wantParents    = 633
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
