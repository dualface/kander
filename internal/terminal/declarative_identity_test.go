package terminal

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

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
