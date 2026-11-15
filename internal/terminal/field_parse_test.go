package terminal

import (
	"strings"
	"testing"

	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/terminal/terminaltest"
)

func TestScopedJSONFieldValidation(t *testing.T) {
	fields := map[string]string{
		"local":  `json_regex:address:^"(t[0-9a-z]+)"$`,
		"app":    `json_regex:status.app:^(null|"(?:[^"\\]|\\.)*")$`,
		"status": `json_regex:status:^(null|\{.*\})$`,
	}
	for _, tc := range []struct {
		name, text, local, app, status string
	}{
		{"nested-name", `{"address":"remote/t8","labels":{"address":"t8","status":42},"status":{"app":"claude"}}`, "", `"claude"`, `{"app":"claude"}`},
		{"formatted", "{\n\"address\": \"t8\", \"status\": {\"app\": \"a\\\"b\"}}", "t8", `"a\"b"`, `{"app":"a\"b"}`},
		{"missing", `{"address":"t8"}`, "t8", "null", "null"},
		{"null", `{"address":"t8","status":null}`, "t8", "null", "null"},
		{"wrong-type", `{"address":8,"status":{"app":42}}`, "", "", `{"app":42}`},
		{"invalid-parent", `{"address":"t8","status":[]}`, "t8", "", ""},
		{"invalid-root", `[]`, "", "", ""},
		{"invalid-json", `{"address":"t8"} trailing`, "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseFields(fields, process.SourceStdout, tc.text)
			if got["local"] != tc.local || got["app"] != tc.app || got["status"] != tc.status {
				t.Fatalf("fields=%q; want local=%q app=%q status=%q", got, tc.local, tc.app, tc.status)
			}
		})
	}
}

func TestScopedJSONFieldEscapedPath(t *testing.T) {
	fields := map[string]string{
		"session": `json_regex:labels.kander\.session:^(null|"(?:[^"\\]|\\.)*")$`,
		"nested":  `json_regex:kander.session:^(null|".*")$`,
	}
	for _, tc := range []struct {
		name, text, session, nested string
	}{
		{"flat-key", `{"labels":{"kander.session":"s1"}}`, `"s1"`, "null"},
		{"old-label", `{"labels":{"kander-session":"s1"}}`, "null", "null"},
		{"nested-object", `{"kander":{"session":"s2"},"labels":{}}`, "null", `"s2"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseFields(fields, process.SourceStdout, tc.text)
			if got["session"] != tc.session || got["nested"] != tc.nested {
				t.Fatalf("fields=%q; want session=%q nested=%q", got, tc.session, tc.nested)
			}
		})
	}
}

func TestDeclarativeRowsEscapedPath(t *testing.T) {
	resetLanguage(t)
	op := strings.NewReplacer(
		`json_array:agents`, `json_array:a\\.b.agents`,
		`json_field:agent_session.value`, `json_field:agent_session.v\\.x`,
	).Replace(agentListOp)
	backend := declarativeOp(t, OpAgentList, op, withAgentIdentity)
	fake := terminaltest.New(t, reply(`{"result":{"a.b":{"agents":[{"pane_id":"w1:p1","agent":"claude","agent_session":{"v.x":"s1"}}]}}}`, "agent", "list"))
	panes, err := backend.AgentList(t.Context(), fakeConn(fake))
	if err != nil || len(panes) != 1 || panes[0].Pane != "w1:p1" || panes[0].AgentSession != "s1" {
		t.Fatalf("panes=%+v err=%v", panes, err)
	}
	missing := terminaltest.New(t, reply(`{"result":{"a":{"b":{"agents":[]}}}}`, "agent", "list"))
	if _, err := backend.AgentList(t.Context(), fakeConn(missing)); err == nil {
		t.Fatal("rows path a.b matched the nested objects instead of the key a.b")
	}
}

func TestScopedJSONFieldDefinitionValidation(t *testing.T) {
	for _, tc := range []struct {
		parse string
		valid bool
	}{
		{`json_regex:labels.kander\.session:^(null|".*")$`, true},
		{`json_regex:labels.kander\x:(x)`, false},
		{`json_regex:labels.kander\:(x)`, false},
		{`json_regex:address:^"(t[0-9a-z]+)"$`, true},
		{`json_regex::(x)`, false},
		{`json_regex:a..b:(x)`, false},
		{`json_regex:address`, false},
		{`json_regex:address:[`, false},
		{`json_regex:address:.*`, false},
		{`json_regex:address:(a)(b)`, false},
	} {
		t.Run(tc.parse, func(t *testing.T) {
			definition := mutateFixture(t, func(root map[string]any) {
				firstStep(root, "topology")["fields"].(map[string]any)["count"] = tc.parse
			})
			_, err := DecodeDefinition("faketerm.json", definition)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}
