package config

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// ignoredOfDocument lists what this version ignores in a raw configuration
// document, tolerating invalid known values so that a document Validate rejects
// still has its unknown content reported and kept.
func ignoredOfDocument(root map[string]any) []Ignored {
	var defs map[string]AgentDefinition
	var unusable map[string]unusableAgent
	if raw, ok := root["agents"]; ok {
		defs, unusable, _ = agentEntries(raw)
	}
	return scanIgnored(root, defs, unusable)
}

// ignoredOfFile reads the config file at path and lists what this version ignores
// in it. A missing, unreadable, or non-object file has nothing to keep.
func ignoredOfFile(path string) []Ignored {
	data, err := readConfigBytes(path)
	if err != nil || data == nil {
		return nil
	}
	raw, err := decodeJSON(data)
	if err != nil {
		return nil
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	return ignoredOfDocument(obj)
}

// encodeConfigKeeping encodes cfg like encodeConfig and writes the ignored items
// back into the document, so a save does not lose what a newer version wrote. An
// item is placed only where its parent object already exists in the encoding, so a
// section the user removed does not come back. The exceptions are parents that exist
// only for the kept content (see mayCreate): the agents object and the agent entries
// and sections below it, and the legacy role object of models.review_roles that
// holds only unknown fields. An item whose key the encoding already has is skipped:
// the current value wins.
func encodeConfigKeeping(cfg *Config, items []Ignored) ([]byte, error) {
	if len(items) == 0 {
		return encodeConfig(cfg)
	}
	items = withCurrentSwitches(cfg, items)
	compact, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	tree, err := parseJSONTree(compact)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		value, err := json.Marshal(item.Value)
		if err != nil {
			return nil, err
		}
		leaf, err := parseJSONTree(value)
		if err != nil {
			return nil, err
		}
		tree.insert(item.Path, leaf)
	}
	var out bytes.Buffer
	if err := json.Indent(&out, tree.render(nil), "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// jsonTree is a JSON value that keeps object key order, so unknown keys can be
// appended without reordering the keys the encoder produced.
type jsonTree struct {
	kind  byte // '{', '[', or 0 for a scalar
	keys  []string
	attrs map[string]*jsonTree
	elems []*jsonTree
	leaf  json.RawMessage
}

func parseJSONTree(data []byte) (*jsonTree, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return readJSONTree(dec)
}

func readJSONTree(dec *json.Decoder) (*jsonTree, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch tok {
	case json.Delim('{'):
		node := &jsonTree{kind: '{', attrs: map[string]*jsonTree{}}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key := keyTok.(string)
			child, err := readJSONTree(dec)
			if err != nil {
				return nil, err
			}
			node.keys = append(node.keys, key)
			node.attrs[key] = child
		}
		_, err := dec.Token()
		return node, err
	case json.Delim('['):
		node := &jsonTree{kind: '['}
		for dec.More() {
			child, err := readJSONTree(dec)
			if err != nil {
				return nil, err
			}
			node.elems = append(node.elems, child)
		}
		_, err := dec.Token()
		return node, err
	}
	leaf, err := json.Marshal(tok)
	return &jsonTree{leaf: leaf}, err
}

// insert places value at path. Missing parents are not created, except where the
// parent exists only because of what is kept: see mayCreate.
func (n *jsonTree) insert(path []string, value *jsonTree) {
	n.insertAt(path, nil, path, value)
}

func (n *jsonTree) insertAt(path, trail, full []string, value *jsonTree) {
	if len(path) == 0 {
		return
	}
	head := path[0]
	switch n.kind {
	case '{':
		if len(path) == 1 {
			if _, exists := n.attrs[head]; !exists {
				n.keys = append(n.keys, head)
				n.attrs[head] = value
			}
			return
		}
		next := append(append([]string(nil), trail...), head)
		child, exists := n.attrs[head]
		if !exists {
			if !mayCreate(next, full) {
				return
			}
			child = &jsonTree{kind: '{', attrs: map[string]*jsonTree{}}
			n.keys = append(n.keys, head)
			n.attrs[head] = child
		}
		child.insertAt(path[1:], next, full, value)
	case '[':
		// An array element has no key to add; only descend into it.
		index, err := strconv.Atoi(head)
		if err != nil || index < 0 || index >= len(n.elems) || len(path) == 1 {
			return
		}
		n.elems[index].insertAt(path[1:], append(append([]string(nil), trail...), head), full, value)
	}
}

// mayCreate says whether the missing object at prefix may be created to hold the
// item at full. Only parents that exist for no reason but the kept content are: the
// agents object, an agents entry, or a section of an entry that the encoding left
// out (an entry whose known content was cleared, or whose section held only unknown
// keys), and the legacy role object of models.review_roles that holds only unknown
// fields.
func mayCreate(prefix, full []string) bool {
	if prefix[0] == "agents" {
		return true
	}
	return len(full) == 4 && len(prefix) == 3 && prefix[0] == "models" && prefix[1] == "review_roles"
}

// render writes compact JSON.
func (n *jsonTree) render(buf []byte) []byte {
	switch n.kind {
	case '{':
		buf = append(buf, '{')
		for i, key := range n.keys {
			if i > 0 {
				buf = append(buf, ',')
			}
			name, _ := json.Marshal(key)
			buf = append(buf, name...)
			buf = append(buf, ':')
			buf = n.attrs[key].render(buf)
		}
		return append(buf, '}')
	case '[':
		buf = append(buf, '[')
		for i, elem := range n.elems {
			if i > 0 {
				buf = append(buf, ',')
			}
			buf = elem.render(buf)
		}
		return append(buf, ']')
	}
	return append(buf, n.leaf...)
}

// withCurrentSwitches applies the install_rules value cfg holds for each unusable
// built-in entry to the copy that is written back, so an edit of that switch (an
// uninstall, the installer, Options) reaches the file while the rest of the entry
// stays as written. Other entries are untouched.
func withCurrentSwitches(cfg *Config, items []Ignored) []Ignored {
	out := make([]Ignored, len(items))
	copy(out, items)
	for i, item := range out {
		if item.Kind != IgnoredUnusableAgent || len(item.Path) != 2 {
			continue
		}
		policy, tracked := cfg.UnusablePolicy[item.Path[1]]
		entry, ok := asObject(item.Value)
		if !tracked || !ok {
			continue
		}
		patched := cloneRawObject(entry)
		if policy.InstallRules != nil {
			patched["install_rules"] = *policy.InstallRules
		} else {
			delete(patched, "install_rules")
		}
		out[i].Value = patched
	}
	return out
}
