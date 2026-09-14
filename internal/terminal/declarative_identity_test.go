package terminal

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal/terminaltest"
)

// agentPaneFacts reads the herdr style pane get response: agent identity,
// status, session reference and owning tab, with the requested pane ID checked.
const agentPaneFacts = `{
	"steps": [{
		"store": "pane",
		"argv": ["pane", "get", "{pane}"],
		"fields": {
			"id": "json_field:result.pane.pane_id",
			"agent": "json_field:result.pane.agent",
			"status": "json_field:result.pane.agent_status",
			"session": "json_field:result.pane.agent_session.value",
			"tab": "json_field:result.pane.tab_id"
		},
		"expect": "field:step.pane.id={pane}",
		"messages": {"invalid": "pane get returned another pane than {pane}"}
	}],
	"result": {"agent": "{step.pane.agent}", "agent_status": "{step.pane.status}", "agent_session": "{step.pane.session}", "container": "{step.pane.tab}"}
}`

// paneListRows scan the herdr style pane list JSON array: every element is
// validated, an agent that is neither a string nor absent invalidates the
// response, and identity decides the match.
const paneListRows = `{
	"from": "list",
	"split": "json_array:result.panes",
	"fields": {
		"tab": "json_field:tab_id",
		"pane": "json_field:pane_id",
		"agent": "json_field:agent",
		"session": "json_field:agent_session.value",
		"bad_agent": "regex:\"agent\":([^\"n])"
	},
	"expect": [["!field_missing:row.tab", "!field_missing:row.pane", "field_missing:row.bad_agent"]],
	"match": "MATCH",
	"result": {"container": "{row.tab}", "pane": "{row.pane}"},
	"messages": {"missing": "pane list has no panes", "invalid": "pane list contains an invalid pane"}
}`

func identityBackend(t *testing.T) *DeclarativeBackend {
	t.Helper()
	return declarativeOp(t, OpPaneFacts, agentPaneFacts, func(root map[string]any) {
		object(root, "capabilities")["agent_identity"] = true
		object(root, "errors")["gone"] = []any{"stdout_json:error.code=pane_not_found", "stderr_json:error.code=pane_not_found"}
		ops := object(root, "ops")
		topology := mustJSON(t, `{"steps": [{"store": "list", "argv": ["pane", "list"]}], "rows": `+
			replaceMatch(paneListRows, `"field:row.tab={container}"`)+`, "result": {"container": "{container}"}}`)
		// Topology rows only collect pane IDs.
		delete(object(topology, "rows", "result"), "container")
		ops["topology"] = topology
		ops["reverse_lookup"] = mustJSON(t, `{"steps": [{"store": "list", "argv": ["pane", "list"]}], "rows": `+
			replaceMatch(paneListRows, `[["field:row.agent={agent}", "field:row.session={reference}"]]`)+`}`)
	})
}

func replaceMatch(rows, match string) string {
	return strings.Replace(rows, `"MATCH"`, match, 1)
}

func mustJSON(t *testing.T, text string) any {
	t.Helper()
	var decoded any
	if err := jsonUnmarshal(text, &decoded); err != nil {
		t.Fatalf("%v: %s", err, text)
	}
	return decoded
}

func TestDeclarativeAgentIdentityFacts(t *testing.T) {
	resetLanguage(t)
	backend := identityBackend(t)
	if !backend.Capabilities().AgentIdentity {
		t.Fatal("agent_identity capability not exposed")
	}
	fake := terminaltest.New(t, reply(`{"result":{"pane":{"pane_id":"w1:p1","tab_id":"w1:t1","agent":"codex","agent_status":"idle","agent_session":{"value":"session-1"}}}}`, "pane", "get", "w1:p1"),
		terminaltest.Reply{Args: []string{"pane", "get", "w1:p9"}, Code: 1, Stderr: `{"error":{"code":"pane_not_found","message":"gone"}}`},
		reply(`{"result":{"pane":{"pane_id":"w1:p2"}}}`, "pane", "get", "w1:p3"),
	)
	facts, err := backend.PaneFacts(context.Background(), fakeConn(fake), "w1:p1")
	want := PaneFacts{Agent: "codex", AgentStatus: "idle", AgentSession: "session-1", Container: "w1:t1"}
	if err != nil || facts != want {
		t.Fatalf("facts=%+v err=%v", facts, err)
	}
	if facts, err := backend.PaneFacts(context.Background(), fakeConn(fake), "w1:p9"); err != nil || !facts.Gone {
		t.Fatalf("stderr json gone: facts=%+v err=%v", facts, err)
	}
	if _, err := backend.PaneFacts(context.Background(), fakeConn(fake), "w1:p3"); err == nil || err.Error() != "pane get returned another pane than w1:p3" {
		t.Fatalf("pane id mismatch: %v", err)
	}
}

func TestDeclarativeJSONArrayRows(t *testing.T) {
	resetLanguage(t)
	backend := identityBackend(t)
	list := `{"result":{"panes":[` +
		`{"pane_id":"w1:p1","tab_id":"w1:t1","agent":"codex","agent_session":{"value":"s1"}},` +
		`{"pane_id":"w1:p2","tab_id":"w1:t2","agent":"claude","agent_session":{"value":"s2"}},` +
		`{"pane_id":"w1:p3","tab_id":"w1:t1","agent_session":null}]}}`
	fake := terminaltest.New(t, reply(list, "pane", "list"))
	topology, err := backend.Topology(context.Background(), fakeConn(fake), Address{Container: "w1:t1"})
	if err != nil || !reflect.DeepEqual(topology, Topology{Container: "w1:t1", Panes: []string{"w1:p1", "w1:p3"}}) {
		t.Fatalf("topology=%+v err=%v", topology, err)
	}
	found, err := backend.ReverseLookup(context.Background(), fakeConn(fake), Identity{Agent: "claude", Reference: "s2"})
	if err != nil || found != (Address{Container: "w1:t2", Pane: "w1:p2"}) {
		t.Fatalf("lookup=%+v err=%v", found, err)
	}
	var matchErr *MatchError
	if _, err := backend.ReverseLookup(context.Background(), fakeConn(fake), Identity{Agent: "claude", Reference: "other"}); !errors.As(err, &matchErr) || matchErr.Matches != 0 {
		t.Fatalf("no match: %v", err)
	}

	fake.SetReplies(t, reply(`{"result":{"panes":[{"pane_id":"w1:p1","tab_id":"w1:t1","agent":5}]}}`, "pane", "list"))
	if _, err := backend.ReverseLookup(context.Background(), fakeConn(fake), Identity{Agent: "codex", Reference: "s1"}); err == nil || err.Error() != "pane list contains an invalid pane" {
		t.Fatalf("non-string agent: %v", err)
	}
	fake.SetReplies(t, reply(`{"result":{}}`, "pane", "list"))
	if _, err := backend.Topology(context.Background(), fakeConn(fake), Address{Container: "w1:t1"}); err == nil || err.Error() != "pane list has no panes" {
		t.Fatalf("missing array: %v", err)
	}
}

// herdrStyleBackend declares herdr-like pane facts, error codes, native
// marker waiting and a prefixed address.
func herdrStyleBackend(t *testing.T) *DeclarativeBackend {
	t.Helper()
	return declarativeOp(t, OpPaneFacts, `{
		"steps": [{
			"store": "pane",
			"argv": ["pane", "get", "{pane}"],
			"output": {"source": "stdout", "parse": "json_field:result"},
			"fields": {"id": "json_field:pane.pane_id", "agent": "json_field:pane.agent"},
			"expect": "field:step.pane.id={pane}",
			"messages": {"not_json": "not json: {detail}", "not_object": "not an object", "missing_result": "no result", "invalid": "wrong pane"}
		}],
		"result": {"agent": "{step.pane.agent}"}
	}`, func(root map[string]any) {
		root["address"] = []any{
			map[string]any{"name": "container", "pattern": `[^:\s]+:[^:\s]+`},
			map[string]any{"name": "pane", "pattern": `[^:\s]+:[^:\s]+`},
		}
		object(root, "errors")["gone"] = []any{"stderr_json:error.code=pane_not_found", "stdout_json:error.code=pane_not_found"}
		object(root, "ops")["wait_output"] = mustJSON(t, `{"steps": [{"argv": ["pane", "wait-output", "{pane}", "--match", "{marker_literal}", "--regex", "{marker_regex}", "--source", "recent", "--timeout", "{timeout_ms}"]}]}`)
	})
}

func TestDeclarativeErrorCodePrecedence(t *testing.T) {
	resetLanguage(t)
	backend := herdrStyleBackend(t)
	for name, tc := range map[string]struct {
		stdout, stderr string
		gone           bool
		detail         string
	}{
		"stderr code":                  {stderr: `{"error":{"code":"pane_not_found"}}`, gone: true, detail: `{"error":{"code":"pane_not_found"}}`},
		"stdout code keeps exit":       {stdout: `{"error":{"code":"pane_not_found"}}`, gone: true, detail: "exit 1"},
		"stderr code decides":          {stdout: `{"error":{"code":"pane_not_found"}}`, stderr: `{"error":{"code":"permission_denied"}}`},
		"empty stderr code falls back": {stdout: `{"error":{"code":"pane_not_found"}}`, stderr: `{"error":{"code":""}}`, gone: true, detail: `{"error":{"code":""}}`},
	} {
		t.Run(name, func(t *testing.T) {
			conn := Conn{Run: func(context.Context, string, []string) (probe.Result, error) {
				return probe.Result{Code: 1, Stdout: tc.stdout, Stderr: tc.stderr}, nil
			}}
			facts, err := backend.PaneFacts(context.Background(), conn, "w1:p1")
			if facts.Gone != tc.gone || facts.GoneDetail != tc.detail || (err == nil) != tc.gone {
				t.Fatalf("facts=%+v err=%v", facts, err)
			}
		})
	}
}

func TestDeclarativeGoneWhenDetail(t *testing.T) {
	resetLanguage(t)
	for name, tc := range map[string]struct {
		message string
		detail  string
	}{
		"neutral default":  {"", "the terminal reported the target as gone"},
		"declared message": {`, "messages": {"gone": "pane {pane} closed"}`, "pane p1 closed"},
	} {
		t.Run(name, func(t *testing.T) {
			backend := declarativeOp(t, OpPaneFacts, `{
				"steps": [{"store": "facts", "argv": ["facts", "{pane}"], "fields": {"state": "json_field:state"}, "gone_when": "field:step.facts.state=closed"`+tc.message+`}],
				"result": {"command": "{step.facts.state}"}
			}`, nil)
			conn := Conn{Run: func(context.Context, string, []string) (probe.Result, error) {
				return probe.Result{Stdout: `{"state":"closed"}`}, nil
			}}
			facts, err := backend.PaneFacts(context.Background(), conn, "p1")
			if err != nil || !facts.Gone || facts.GoneDetail != tc.detail {
				t.Fatalf("facts=%+v err=%v", facts, err)
			}
		})
	}
}

func TestDeclarativeJSONOutputKinds(t *testing.T) {
	resetLanguage(t)
	backend := herdrStyleBackend(t)
	for output, want := range map[string]struct {
		kind    ErrorKind
		message string
	}{
		"not-json":          {KindNotJSON, "not json: invalid character 'o' in literal null (expecting 'u')"},
		"[]":                {KindNotObject, "not an object"},
		"{}":                {KindMissingResult, "no result"},
		`{"result":"text"}`: {KindInvalidResponse, "wrong pane"},
		`{"result":{"pane":{"pane_id":"w1:p2"}}}`: {KindInvalidResponse, "wrong pane"},
	} {
		conn := Conn{Run: func(context.Context, string, []string) (probe.Result, error) {
			return probe.Result{Stdout: output}, nil
		}}
		_, err := backend.PaneFacts(context.Background(), conn, "w1:p1")
		commandErr, ok := AsCommandError(err)
		if !ok || commandErr.Kind != want.kind || commandErr.Error() != want.message {
			t.Fatalf("output %q: err=%v kind=%v", output, err, commandErr)
		}
	}
	conn := Conn{Run: func(context.Context, string, []string) (probe.Result, error) {
		return probe.Result{Stdout: `{"result":{"pane":{"pane_id":"w1:p1","agent":"codex"}}}`}, nil
	}}
	if facts, err := backend.PaneFacts(context.Background(), conn, "w1:p1"); err != nil || facts.Agent != "codex" {
		t.Fatalf("sub-document output: facts=%+v err=%v", facts, err)
	}
}

func TestDeclarativeNativeMarkerFlags(t *testing.T) {
	resetLanguage(t)
	backend := herdrStyleBackend(t)
	for marker, want := range map[string][]string{
		"READY":             {"pane", "wait-output", "w1:p1", "--match", "READY", "--source", "recent", "--timeout", "15000"},
		`regex:READY\s+NOW`: {"pane", "wait-output", "w1:p1", "--regex", `READY\s+NOW`, "--source", "recent", "--timeout", "15000"},
	} {
		var got []string
		if err := backend.WaitOutput(context.Background(), recordingConn(&got), "w1:p1", marker, 15000); err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("marker %q: argv=%q err=%v", marker, got, err)
		}
	}
}

func TestDeclarativeFocusAddressAcceptsPlainSegments(t *testing.T) {
	resetLanguage(t)
	backend := herdrStyleBackend(t)
	for window, want := range map[string]Address{
		"faketerm:w1:t2:w1:p3": {Container: "w1:t2", Pane: "w1:p3"},
		"faketerm:t2:p3":       {Container: "t2", Pane: "p3"},
	} {
		if got, ok := backend.ParseFocusAddress(strings.Split(window, ":")); !ok || got != want {
			t.Fatalf("focus %s: %+v ok=%v", window, got, ok)
		}
	}
	for _, window := range []string{"faketerm:w1:t2:p3", "other:t2:p3"} {
		if _, ok := backend.ParseFocusAddress(strings.Split(window, ":")); ok {
			t.Fatalf("focus %s accepted", window)
		}
	}
	if _, ok := backend.ParseAddress("faketerm:t2:p3"); ok {
		t.Fatal("WINDOW parsing stays strict")
	}
}
