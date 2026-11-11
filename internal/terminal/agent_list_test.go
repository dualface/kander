package terminal

import (
	"errors"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/terminal/terminaltest"
)

const agentListOp = `{
	"steps":[{"argv":["agent","list"],"store":"list","output":{"source":"stdout","parse":"json_field:result"}}],
	"rows":{"from":"list","split":"json_array:agents",
		"fields":{"pane":"json_field:pane_id","agent":"json_field:agent","status":"json_field:agent_status",
			"kind":"json_field:agent_session.kind","reference":"json_field:agent_session.value"},
		"expect":[["!field_missing:row.pane"]],
		"match":[["!field_missing:row.pane"]],
		"result":{"pane":"{row.pane}","agent":"{row.agent}","agent_status":"{row.status}",
			"agent_session":"{row.reference}","agent_session_kind":"{row.kind}"}}}`

func withAgentIdentity(root map[string]any) {
	object(root, "capabilities")["agent_identity"] = true
}

func TestDeclarativeAgentList(t *testing.T) {
	resetLanguage(t)
	backend := declarativeOp(t, OpAgentList, agentListOp, withAgentIdentity)
	for _, tc := range []struct {
		name, output string
		want         []AgentPane
		ok           bool
	}{
		{"rows", `{"result":{"agents":[
			{"pane_id":"w1:p1","agent":"claude","agent_status":"working","agent_session":{"kind":"id","value":"s1"}},
			{"pane_id":"w1:p2","agent":"codex","agent_status":"blocked"}]}}`,
			[]AgentPane{
				{Pane: "w1:p1", Agent: "claude", AgentStatus: "working", AgentSession: "s1", AgentSessionKind: "id"},
				{Pane: "w1:p2", Agent: "codex", AgentStatus: "blocked"},
			}, true},
		{"empty", `{"result":{"agents":[]}}`, []AgentPane{}, true},
		{"missing-array", `{"result":{}}`, nil, false},
		{"row-without-pane", `{"result":{"agents":[{"agent":"claude"}]}}`, nil, false},
		{"duplicate-pane", `{"result":{"agents":[{"pane_id":"w1:p1"},{"pane_id":"w1:p1"}]}}`, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := terminaltest.New(t, reply(tc.output, "agent", "list"))
			panes, err := backend.AgentList(t.Context(), fakeConn(fake))
			if (err == nil) != tc.ok {
				t.Fatalf("panes=%+v err=%v", panes, err)
			}
			if !tc.ok {
				return
			}
			if len(panes) != len(tc.want) {
				t.Fatalf("panes=%+v", panes)
			}
			for index := range panes {
				if panes[index] != tc.want[index] {
					t.Fatalf("pane %d=%+v; want %+v", index, panes[index], tc.want[index])
				}
			}
		})
	}
	if _, err := fixtureBackend(t, noEnv).AgentList(t.Context(), Conn{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("undeclared agent_list: %v", err)
	}
}

func TestValidateAgentList(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   string
		change func(root map[string]any)
		want   string
	}{
		{"needs-agent-identity", agentListOp, nil, "agent_identity is false"},
		{"needs-rows", `{"steps":[{"argv":["agent","list"],"store":"list"}]}`, withAgentIdentity, "require rows"},
		{"no-operation-result", strings.Replace(agentListOp, `"steps":`, `"result":{"pane":"x"},"steps":`, 1), withAgentIdentity, "is not a result field"},
		{"no-session-identity", strings.Replace(agentListOp, `"match":`, `"session":{"kind":"kind","value":"reference"},"match":`, 1), withAgentIdentity, "only reverse_lookup declares a session"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := mutateFixture(t, func(root map[string]any) {
				var decoded any
				if err := jsonUnmarshal(tc.body, &decoded); err != nil {
					t.Fatal(err)
				}
				object(root, "ops")[OpAgentList] = decoded
				if tc.change != nil {
					tc.change(root)
				}
			})
			if _, err := DecodeDefinition("faketerm.json", data); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v; want %q", err, tc.want)
			}
		})
	}
}
