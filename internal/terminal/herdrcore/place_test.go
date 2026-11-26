package herdrcore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/probe"
	"github.com/dualface/kander/v2/internal/terminal"
)

const layoutBefore = `{
  "spaces": [
    {"folder": "/a", "id": "sp1", "name": "", "pinned": 9007199254740993, "tabs": [
      {"id": "tbCaller", "name": "", "root": {"children": [{"size": {"weight": 0.520574152469635}, "content": {"terminal": "t1"}}], "type": "row"}},
      {"id": "tbRight", "name": "right", "root": null}
    ]},
    {"folder": "/b", "id": "sp2", "name": "second", "tabs": [
      {"id": "tbOther", "name": "kept", "root": null}
    ]}
  ],
  "future": {"id": 18446744073709551615, "text": "a<b>&c"}
}`

// fakeHC plays hc and Herdr App: a label that places a terminal makes the tab
// appear in the layout document after appearAfter further reads of it.
type fakeHC struct {
	t           *testing.T
	callerGet   string
	getErr      bool
	labelErr    bool
	docErr      bool
	appearAfter int
	neverAppear bool
	conflicts   int    // doc.set calls answered with a conflict before one is accepted
	insertAfter string // tab Herdr App inserts the new one after; empty appends
	lateTab     string // tab Herdr App appends to the space while a conflict is answered
	blockInput  bool

	layout   string
	version  uint64
	labels   [][]string
	gets     int
	reads    int
	sets     int
	pending  string // tab to add once reads reach appearAfter
	pendingS string
}

func (f *fakeHC) conn() terminal.Conn {
	return terminal.Conn{
		Program: "hc",
		Run: func(ctx context.Context, _ string, args []string) (probe.Result, error) {
			switch args[0] {
			case "get":
				f.gets++
				if f.getErr {
					return probe.Result{Code: 3, Stderr: `{"error":{"kind":"not_found"}}`}, nil
				}
				return probe.Result{Stdout: f.callerGet}, nil
			case "label":
				f.labels = append(f.labels, append([]string{}, args...))
				if f.labelErr {
					return probe.Result{Code: 5, Stderr: "down"}, nil
				}
				var space, tab string
				for _, arg := range args[3:] {
					switch {
					case strings.HasPrefix(arg, "herdr.space="):
						space = strings.TrimPrefix(arg, "herdr.space=")
					case strings.HasPrefix(arg, "herdr.tab="):
						tab = strings.TrimPrefix(arg, "herdr.tab=")
					}
				}
				f.pendingS, f.pending = space, tab
				return probe.Result{Stdout: `{}`}, nil
			}
			f.t.Fatalf("unexpected hc %q", args)
			return probe.Result{}, nil
		},
		RunInput: func(ctx context.Context, _ string, args []string, input string) (probe.Result, error) {
			if f.blockInput {
				<-ctx.Done()
				return probe.Result{}, ctx.Err()
			}
			if !reflect.DeepEqual(args, []string{"connect", "--json"}) {
				f.t.Fatalf("unexpected argv %q", args)
			}
			var request struct {
				ID     int             `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := json.Unmarshal([]byte(input), &request); err != nil {
				f.t.Fatalf("request %q: %v", input, err)
			}
			// An event line precedes the response, as the real stream may.
			event := `{"event":"layout.changed"}` + "\n"
			reply := func(result string) probe.Result {
				return probe.Result{Stdout: event + `{"jsonrpc":"2.0","id":` + string(rune('0'+request.ID)) + `,"result":` + result + "}\n"}
			}
			fail := func(kind string) probe.Result {
				return probe.Result{Stdout: event + `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"m","data":{"kind":"` + kind + `"}}}` + "\n"}
			}
			if f.docErr {
				return fail("internal"), nil
			}
			switch request.Method {
			case "doc.get":
				f.reads++
				if f.pending != "" && !f.neverAppear && f.reads > f.appearAfter {
					f.addTab()
				}
				return reply(`{"value":` + f.layout + `,"version":` + strconv.FormatUint(f.version, 10) + `}`), nil
			case "doc.set":
				f.sets++
				var params struct {
					Name    string          `json:"name"`
					Value   json.RawMessage `json:"value"`
					Version uint64          `json:"version"`
				}
				if err := json.Unmarshal(request.Params, &params); err != nil || params.Name != "herdr-app" {
					f.t.Fatalf("params %s: %v", request.Params, err)
				}
				if f.conflicts > 0 {
					f.conflicts--
					if f.lateTab != "" {
						f.appendTab(f.pendingS, f.lateTab)
						f.lateTab = ""
					}
					f.version++
					return fail("conflict"), nil
				}
				if params.Version != f.version {
					return fail("conflict"), nil
				}
				f.layout = string(params.Value)
				f.version++
				return reply(`{"version":` + strconv.FormatUint(f.version, 10) + `}`), nil
			}
			f.t.Fatalf("unexpected method %q", request.Method)
			return probe.Result{}, nil
		},
	}
}

// addTab plays Herdr App creating the placed tab in its space: after
// insertAfter when that is set, at the end otherwise.
func (f *fakeHC) addTab() {
	doc := decode(f.t, f.layout)
	spaces := doc["spaces"].([]any)
	for _, entry := range spaces {
		space := entry.(map[string]any)
		if space["id"] != f.pendingS {
			continue
		}
		tabs := space["tabs"].([]any)
		added := map[string]any{"id": f.pending, "name": "", "root": nil}
		at := len(tabs)
		for i, tab := range tabs {
			if tab.(map[string]any)["id"] == f.insertAfter {
				at = i + 1
			}
		}
		space["tabs"] = append(tabs[:at:at], append([]any{added}, tabs[at:]...)...)
	}
	f.layout = encode(f.t, doc)
	f.version++
	f.pending = ""
}

// appendTab plays Herdr App appending an unrelated tab to a space.
func (f *fakeHC) appendTab(spaceID, id string) {
	doc := decode(f.t, f.layout)
	for _, entry := range doc["spaces"].([]any) {
		space := entry.(map[string]any)
		if space["id"] == spaceID {
			space["tabs"] = append(space["tabs"].([]any), map[string]any{"id": id, "name": "late", "root": nil})
		}
	}
	f.layout = encode(f.t, doc)
	f.version++
}

func decode(t *testing.T, text string) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var doc map[string]any
	if err := decoder.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func encode(t *testing.T, doc any) string {
	t.Helper()
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(doc); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out.String())
}

// tabName is the name of the tab with the given ID in the stored layout.
func tabName(t *testing.T, layout, tab string) any {
	t.Helper()
	for _, space := range decode(t, layout)["spaces"].([]any) {
		for _, entry := range space.(map[string]any)["tabs"].([]any) {
			if entry.(map[string]any)["id"] == tab {
				return entry.(map[string]any)["name"]
			}
		}
	}
	t.Fatalf("tab %q not in %s", tab, layout)
	return nil
}

// tabOrder is the IDs of the tabs of a space in the stored layout, in order.
func tabOrder(t *testing.T, layout, spaceID string) []string {
	t.Helper()
	for _, space := range decode(t, layout)["spaces"].([]any) {
		if space.(map[string]any)["id"] != spaceID {
			continue
		}
		var ids []string
		for _, entry := range space.(map[string]any)["tabs"].([]any) {
			ids = append(ids, entry.(map[string]any)["id"].(string))
		}
		return ids
	}
	t.Fatalf("space %q not in %s", spaceID, layout)
	return nil
}

func newFake(t *testing.T, callerLabels string) *fakeHC {
	t.Helper()
	oldTotal, oldCall, oldWait, oldPoll, oldMax := totalBudget, callBudget, tabWait, tabPoll, maxConflicts
	t.Cleanup(func() {
		totalBudget, callBudget, tabWait, tabPoll, maxConflicts = oldTotal, oldCall, oldWait, oldPoll, oldMax
	})
	totalBudget, callBudget, tabWait, tabPoll = 5*time.Second, time.Second, 300*time.Millisecond, time.Millisecond
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(layoutBefore)); err != nil {
		t.Fatal(err)
	}
	return &fakeHC{t: t, layout: compact.String(), version: 52, callerGet: `{"address":"t3","labels":` + callerLabels + `}`}
}

func place(f *fakeHC, env map[string]string, title, label string) terminal.HookResult {
	return PlaceTab(context.Background(), terminal.HookCall{
		Launcher: "herdr-core", Point: terminal.HookPointPlaceContainer, Conn: f.conn(),
		Getenv: func(key string) string { return env[key] },
		Values: map[string]string{"container": "t9", "pane": "t9", "title": title, "label": label},
	})
}

var callerEnv = map[string]string{"HC_TERMINAL_ID": "t3"}

func placedLabels(t *testing.T, f *fakeHC) (space, tab string) {
	t.Helper()
	if len(f.labels) != 1 {
		t.Fatalf("labels=%q", f.labels)
	}
	args := f.labels[0]
	if len(args) != 5 || args[0] != "label" || args[1] != "--json" || args[2] != "t9" {
		t.Fatalf("label argv=%q", args)
	}
	return strings.TrimPrefix(args[3], "herdr.space="), strings.TrimPrefix(args[4], "herdr.tab=")
}

// The new terminal gets the caller's space and a new random tab, whatever tab
// the caller sits in, and Herdr App's tab is named and ends up last in the
// space, wherever Herdr App inserted it; nothing else in the layout document
// changes, including the order of the other tabs and numbers float64 cannot
// hold.
func TestPlaceTabNamesNewTab(t *testing.T) {
	for _, labels := range []string{`{"herdr.space":"sp1","herdr.tab":"tbCaller","where":"w / 1"}`, `{"herdr.space":"sp1"}`} {
		for _, insertAfter := range []string{"tbCaller", "tbRight"} {
			f := newFake(t, labels)
			f.insertAfter = insertAfter
			f.appearAfter = 2
			if result := place(f, callerEnv, "Open each agent", "kb-x"); result.Status != terminal.HookOK {
				t.Fatalf("labels %s insertAfter %q: %+v", labels, insertAfter, result)
			}
			if f.sets != 1 {
				t.Fatalf("insertAfter %q: sets=%d, want the rename and the move in one write", insertAfter, f.sets)
			}
			space, tab := placedLabels(t, f)
			if space != "sp1" || !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(tab) || tab == "tbCaller" {
				t.Fatalf("space=%q tab=%q", space, tab)
			}
			for _, arg := range f.labels[0] {
				if strings.HasPrefix(arg, "where=") {
					t.Fatalf("where copied: %q", f.labels[0])
				}
			}
			want := decode(t, layoutBefore)
			sp1 := want["spaces"].([]any)[0].(map[string]any)
			sp1["tabs"] = append(sp1["tabs"].([]any), map[string]any{"id": tab, "name": "Open each agent", "root": nil})
			if got := decode(t, f.layout); !reflect.DeepEqual(got, want) {
				t.Fatalf("insertAfter %q: layout changed beyond the tab name and place\n got=%s\nwant=%s", insertAfter, encode(t, got), encode(t, want))
			}
			for _, text := range []string{"9007199254740993", "18446744073709551615", "0.520574152469635", "a<b>&c"} {
				if !strings.Contains(f.layout, text) {
					t.Fatalf("layout lost %q: %s", text, f.layout)
				}
			}
		}
	}
}

// Without a caller terminal or a caller space nothing is placed and the
// result is a plain OK, as before this hook existed.
func TestPlaceTabWithoutCallerSpace(t *testing.T) {
	for name, tc := range map[string]struct {
		env    map[string]string
		labels string
	}{
		"no HC_TERMINAL_ID":  {nil, `{"herdr.space":"sp1","herdr.tab":"tb"}`},
		"blank id":           {map[string]string{"HC_TERMINAL_ID": "  "}, `{"herdr.space":"sp1"}`},
		"no labels":          {callerEnv, `null`},
		"only tab":           {callerEnv, `{"herdr.tab":"tb1"}`},
		"empty space":        {callerEnv, `{"herdr.space":""}`},
		"illegal space":      {callerEnv, `{"herdr.space":"sp 1"}`},
		"non-string space":   {callerEnv, `{"herdr.space":1}`},
		"forged label value": {callerEnv, `{"note":"{\"herdr.space\":\"x\"}"}`},
	} {
		f := newFake(t, tc.labels)
		if result := place(f, tc.env, "t", "l"); result.Status != terminal.HookOK {
			t.Fatalf("%s: %+v", name, result)
		}
		if len(f.labels) != 0 || f.reads != 0 || f.sets != 0 {
			t.Fatalf("%s: labels=%q reads=%d sets=%d", name, f.labels, f.reads, f.sets)
		}
		if tc.env == nil && f.gets != 0 {
			t.Fatalf("%s: read the caller without an ID", name)
		}
	}
}

func TestPlaceTabDegrades(t *testing.T) {
	const placed = `{"herdr.space":"sp1","herdr.tab":"tbCaller"}`
	cases := map[string]func(*fakeHC){
		"caller read fails":   func(f *fakeHC) { f.getErr = true },
		"caller not JSON":     func(f *fakeHC) { f.callerGet = "not json" },
		"label fails":         func(f *fakeHC) { f.labelErr = true },
		"layout read fails":   func(f *fakeHC) { f.docErr = true },
		"tab never appears":   func(f *fakeHC) { f.neverAppear = true },
		"space not in layout": func(f *fakeHC) { f.callerGet = `{"labels":{"herdr.space":"gone"}}` },
	}
	for name, setup := range cases {
		f := newFake(t, placed)
		setup(f)
		result := place(f, callerEnv, "t", "l")
		if result.Status != terminal.HookDegraded || result.Note == "" {
			t.Fatalf("%s: %+v", name, result)
		}
		if f.sets != 0 {
			t.Fatalf("%s: wrote the layout", name)
		}
	}
}

// A version conflict rereads the document and tries again, a bounded number of
// times; the tab's name and place are the only differences that get written.
func TestPlaceTabRetriesConflicts(t *testing.T) {
	f0 := newFake(t, `{"herdr.space":"sp1"}`)
	f0.insertAfter = "tbCaller"
	f0.conflicts = 1
	f0.lateTab = "tbLate"
	if result := place(f0, callerEnv, "name", "l"); result.Status != terminal.HookOK {
		t.Fatalf("%+v", result)
	}
	_, tab0 := placedLabels(t, f0)
	if got := tabOrder(t, f0.layout, "sp1"); !reflect.DeepEqual(got, []string{"tbCaller", "tbRight", "tbLate", tab0}) {
		t.Fatalf("a tab Herdr App appended during the conflict: sp1 tabs=%q", got)
	}
	if tabName(t, f0.layout, tab0) != "name" || tabName(t, f0.layout, "tbLate") != "late" {
		t.Fatalf("names wrong: %s", f0.layout)
	}

	f := newFake(t, `{"herdr.space":"sp1"}`)
	f.conflicts = 1
	if result := place(f, callerEnv, "name", "l"); result.Status != terminal.HookOK {
		t.Fatalf("%+v", result)
	}
	if f.sets != 2 {
		t.Fatalf("sets=%d, want a conflict then a success", f.sets)
	}
	if _, tab := placedLabels(t, f); tabName(t, f.layout, tab) != "name" {
		t.Fatalf("tab not named: %s", f.layout)
	}

	f = newFake(t, `{"herdr.space":"sp1"}`)
	f.conflicts = 100
	result := place(f, callerEnv, "name", "l")
	if result.Status != terminal.HookDegraded {
		t.Fatalf("%+v", result)
	}
	if f.sets != maxConflicts {
		t.Fatalf("sets=%d, want the cap %d", f.sets, maxConflicts)
	}
}

// The hook stops when its own budget ends, even when hc never answers.
func TestPlaceTabTotalBudget(t *testing.T) {
	f := newFake(t, `{"herdr.space":"sp1"}`)
	f.blockInput = true
	totalBudget = 60 * time.Millisecond
	start := time.Now()
	result := place(f, callerEnv, "t", "l")
	if result.Status != terminal.HookDegraded {
		t.Fatalf("%+v", result)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("hook ran %v past its budget", elapsed)
	}
}

// The title is a JSON string value: quotes and backslashes are escaped, control
// characters are gone, and an empty title falls back to the container label.
func TestPlaceTabTitleIsData(t *testing.T) {
	for _, tc := range []struct{ title, label, want string }{
		{"say \"hi\" \\ <b>\x00\x1b[31m\n tail", "l", `say "hi" \ <b>[31m tail`},
		{"", "kb-fallback", "kb-fallback"},
		{"\x01\x02", "kb-fallback", "kb-fallback"},
		{`"],"x":{`, "l", `"],"x":{`},
	} {
		f := newFake(t, `{"herdr.space":"sp1"}`)
		if result := place(f, callerEnv, tc.title, tc.label); result.Status != terminal.HookOK {
			t.Fatalf("%q: %+v", tc.title, result)
		}
		_, tab := placedLabels(t, f)
		if name := tabName(t, f.layout, tab); name != tc.want {
			t.Fatalf("title %q: name=%q, want %q", tc.title, name, tc.want)
		}
		if _, extra := decode(t, f.layout)["x"]; extra {
			t.Fatalf("title %q injected a field", tc.title)
		}
	}
}

func TestPlaceTabWithoutAnyTitle(t *testing.T) {
	f := newFake(t, `{"herdr.space":"sp1"}`)
	if result := place(f, callerEnv, "\x01", "\x02"); result.Status != terminal.HookOK {
		t.Fatalf("%+v", result)
	}
	if len(f.labels) != 1 || f.reads != 0 {
		t.Fatalf("labels=%q reads=%d", f.labels, f.reads)
	}
}

func TestPlaceTabLastIgnoresMalformedDocuments(t *testing.T) {
	for _, doc := range []any{nil, "x", []any{}, map[string]any{"spaces": "x"}, map[string]any{"spaces": []any{1, map[string]any{"id": "sp", "tabs": []any{nil}}}}} {
		if placeTabLast(doc, "sp", "tb", "n") {
			t.Fatalf("named a tab in %v", doc)
		}
	}
}

func TestRPCErrors(t *testing.T) {
	f := newFake(t, `{}`)
	conn := f.conn()
	conn.RunInput = func(context.Context, string, []string, string) (probe.Result, error) {
		return probe.Result{Stdout: `{"id":7,"result":{}}` + "\n"}, nil
	}
	if _, err := (hc{conn: conn}).rpc(context.Background(), "doc.get", nil); err == nil || !strings.Contains(err.Error(), "no response") {
		t.Fatalf("err=%v", err)
	}
	conn.RunInput = func(context.Context, string, []string, string) (probe.Result, error) {
		return probe.Result{Code: 2, Stderr: "boom"}, nil
	}
	if _, err := (hc{conn: conn}).rpc(context.Background(), "doc.get", nil); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err=%v", err)
	}
	conn.RunInput = func(context.Context, string, []string, string) (probe.Result, error) {
		return probe.Result{}, errors.New("spawn failed")
	}
	if _, err := (hc{conn: conn}).rpc(context.Background(), "doc.get", nil); err == nil {
		t.Fatal("spawn failure not reported")
	}
}
