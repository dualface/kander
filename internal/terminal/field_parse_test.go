package terminal

import (
	"testing"

	"github.com/dualface/kander/internal/process"
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

func TestScopedJSONFieldDefinitionValidation(t *testing.T) {
	for _, tc := range []struct {
		parse string
		valid bool
	}{
		{`json_regex:labels.kander-session:^(null|".*")$`, true},
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
