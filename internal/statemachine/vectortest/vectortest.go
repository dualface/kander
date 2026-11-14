// Package vectortest replays the differential vectors that the Lean toolchain
// in formal/ produces against a generated Go decision kernel.
//
// A vector file starts with a header line naming the machine, the sequence
// depth, the parameter sets, and the event alphabet. Every other line is one
// step the Lean semantics evaluated: a parameter set index, a state, an event
// index, whether the event was accepted, the next state, and the query results
// of the state. Check walks every event sequence of the header's depth from
// the initial state of every parameter set, looks up each step, and compares
// the Go result byte for byte through the canonical JSON encoders.
package vectortest

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Machine bundles a generated kernel and its canonical encoders.
type Machine[P, S, E any] struct {
	Name          string
	Params        []P
	Events        []E
	Initial       func(P) S
	Step          func(S, E, P) (S, bool)
	EncodeParams  func(P) string
	EncodeState   func(S) string
	EncodeEvent   func(E) string
	EncodeQueries func(S, P) string
}

type header struct {
	Machine string            `json:"machine"`
	Depth   int               `json:"depth"`
	Params  []json.RawMessage `json:"params"`
	Events  []json.RawMessage `json:"events"`
}

type row struct {
	Params   int             `json:"params"`
	State    json.RawMessage `json:"state"`
	Event    int             `json:"event"`
	Accepted bool            `json:"accepted"`
	Next     json.RawMessage `json:"next"`
	Queries  json.RawMessage `json:"queries"`
}

type rowKey struct {
	params int
	state  string
	event  int
}

// maxLineBytes bounds one vector line.
const maxLineBytes = 1 << 20

// Replay runs Check on the vector file at path and fails t on any mismatch.
func Replay[P, S, E any](t testing.TB, path string, m Machine[P, S, E]) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open vectors: %v", err)
	}
	defer f.Close()
	steps, err := Check(f, m)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %d steps replayed", m.Name, steps)
}

// Check replays the vectors read from r against m and returns the number of
// steps it compared.
func Check[P, S, E any](r io.Reader, m Machine[P, S, E]) (int, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return 0, fmt.Errorf("read header: %w", err)
		}
		return 0, errors.New("empty vector file")
	}
	var h header
	if err := json.Unmarshal(sc.Bytes(), &h); err != nil {
		return 0, fmt.Errorf("parse header: %w", err)
	}
	if err := checkHeader(h, m); err != nil {
		return 0, err
	}
	rows := make(map[rowKey]row)
	line := 1
	for sc.Scan() {
		line++
		var rw row
		if err := json.Unmarshal(sc.Bytes(), &rw); err != nil {
			return 0, fmt.Errorf("line %d: %w", line, err)
		}
		if rw.Params < 0 || rw.Params >= len(m.Params) || rw.Event < 0 || rw.Event >= len(m.Events) {
			return 0, fmt.Errorf("line %d: parameter or event index out of range", line)
		}
		k := rowKey{rw.Params, string(rw.State), rw.Event}
		if _, dup := rows[k]; dup {
			return 0, fmt.Errorf("line %d: duplicate row", line)
		}
		rows[k] = rw
	}
	if err := sc.Err(); err != nil {
		return 0, fmt.Errorf("read vectors: %w", err)
	}
	w := walker[P, S, E]{m: m, rows: rows, visited: make(map[rowKey]bool)}
	for i, p := range m.Params {
		if err := w.walk(i, p, m.Initial(p), h.Depth, nil); err != nil {
			return w.steps, err
		}
	}
	if len(w.visited) != len(rows) {
		return w.steps, fmt.Errorf("%d of %d rows are not reachable within depth %d", len(rows)-len(w.visited), len(rows), h.Depth)
	}
	return w.steps, nil
}

func checkHeader[P, S, E any](h header, m Machine[P, S, E]) error {
	if h.Machine != m.Name {
		return fmt.Errorf("vectors are for machine %q, want %q", h.Machine, m.Name)
	}
	if h.Depth < 1 {
		return fmt.Errorf("depth %d is below 1", h.Depth)
	}
	if len(h.Params) != len(m.Params) {
		return fmt.Errorf("vectors list %d parameter sets, kernel test lists %d", len(h.Params), len(m.Params))
	}
	for i, p := range m.Params {
		if got, want := m.EncodeParams(p), string(h.Params[i]); got != want {
			return fmt.Errorf("parameter set %d: Go %s, Lean %s", i, got, want)
		}
	}
	if len(h.Events) != len(m.Events) {
		return fmt.Errorf("vectors list %d events, kernel test lists %d", len(h.Events), len(m.Events))
	}
	for i, e := range m.Events {
		if got, want := m.EncodeEvent(e), string(h.Events[i]); got != want {
			return fmt.Errorf("event %d: Go %s, Lean %s", i, got, want)
		}
	}
	return nil
}

type walker[P, S, E any] struct {
	m       Machine[P, S, E]
	rows    map[rowKey]row
	visited map[rowKey]bool
	steps   int
}

func (w *walker[P, S, E]) walk(pi int, p P, s S, depth int, path []int) error {
	if depth == 0 {
		return nil
	}
	enc := w.m.EncodeState(s)
	for ei, e := range w.m.Events {
		// Copy so sibling sequences never share a backing array.
		seq := append(path[:len(path):len(path)], ei)
		k := rowKey{pi, enc, ei}
		rw, ok := w.rows[k]
		if !ok {
			return fmt.Errorf("params %d, events %v: no Lean row for state %s", pi, seq, enc)
		}
		w.visited[k] = true
		w.steps++
		if got := w.m.EncodeQueries(s, p); got != string(rw.Queries) {
			return fmt.Errorf("params %d, events %v: queries Go %s, Lean %s", pi, seq[:len(seq)-1], got, rw.Queries)
		}
		next, accepted := w.m.Step(s, e, p)
		if accepted != rw.Accepted {
			return fmt.Errorf("params %d, events %v: accepted Go %t, Lean %t", pi, seq, accepted, rw.Accepted)
		}
		if got := w.m.EncodeState(next); got != string(rw.Next) {
			return fmt.Errorf("params %d, events %v: next state Go %s, Lean %s", pi, seq, got, rw.Next)
		}
		if err := w.walk(pi, p, next, depth-1, seq); err != nil {
			return err
		}
	}
	return nil
}

// Bool encodes a boolean.
func Bool(b bool) string {
	return strconv.FormatBool(b)
}

// Uint encodes a natural number.
func Uint(n uint64) string {
	return strconv.FormatUint(n, 10)
}

// Str encodes an identifier. Lean restricts names to ASCII letters and
// digits, so no escaping is needed.
func Str(s string) string {
	return `"` + s + `"`
}

// Object encodes alternating keys and encoded values.
func Object(kv ...string) string {
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i+1 < len(kv); i += 2 {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(Str(kv[i]))
		b.WriteByte(':')
		b.WriteString(kv[i+1])
	}
	b.WriteByte('}')
	return b.String()
}

// Array encodes already encoded elements.
func Array(xs ...string) string {
	return "[" + strings.Join(xs, ",") + "]"
}

// Event encodes an event name and alternating argument names and encoded
// values.
func Event(name string, args ...string) string {
	return Object("event", Str(name), "args", Object(args...))
}
