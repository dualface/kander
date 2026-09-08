package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The transcript is newline-delimited OpenAI-shaped messages. Only assistant turns carry
// report text: a tool call leaves content null, the tool result is echoed under role "tool",
// and the trailing meta line carries a resume hint that must not reach the report.
func TestKimiReviewTextKeepsOnlyAssistantTurns(t *testing.T) {
	for _, test := range []struct {
		name, raw, want string
		valid           bool
	}{
		{
			name: "assistant only",
			raw:  `{"role":"assistant","content":"FINDING ONE"}` + "\n",
			want: "FINDING ONE", valid: true,
		},
		{
			name: "tool call and result are skipped",
			raw: `{"role":"assistant","content":null,"tool_calls":[{"type":"function","id":"c1","function":{"name":"Bash","arguments":"{}"}}]}` + "\n" +
				`{"role":"tool","tool_call_id":"c1","content":"Tool \"Bash\" was denied by permission rule."}` + "\n" +
				`{"role":"assistant","content":"BLOCKED and reported"}` + "\n",
			want: "BLOCKED and reported", valid: true,
		},
		{
			name: "resume hint is not report text",
			raw: `{"role":"assistant","content":"REAL REPORT"}` + "\n" +
				`{"role":"meta","type":"session.resume_hint","session_id":"session_x","content":"To resume this session: kimi -r session_x"}` + "\n",
			want: "REAL REPORT", valid: true,
		},
		{
			name: "multiple assistant turns join",
			raw: `{"role":"assistant","content":"first"}` + "\n" +
				`{"role":"assistant","content":"second"}` + "\n",
			want: "first\nsecond", valid: true,
		},
		{
			name: "only a resume hint is not a report",
			raw:  `{"role":"meta","type":"session.resume_hint","content":"To resume this session: kimi -r session_x"}` + "\n",
			want: "", valid: false,
		},
		{
			name: "empty output is not a report",
			raw:  "", want: "", valid: false,
		},
		{
			name: "malformed lines are ignored",
			raw:  "not json\n" + `{"role":"assistant","content":"survives"}` + "\n",
			want: "survives", valid: true,
		},
		{
			name: "crlf transcript",
			raw:  `{"role":"assistant","content":"windows"}` + "\r\n",
			want: "windows", valid: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, valid := kimiReviewText([]byte(test.raw))
			if got != test.want || valid != test.valid {
				t.Fatalf("text=%q valid=%v, want %q %v", got, valid, test.want, test.valid)
			}
		})
	}
}

// Isolation comes from the agent definition's tool allowlist, not from flags: kimi-code has
// none, and its read-only --plan mode cannot be combined with --prompt.
func TestWriteKimiReviewerAgentAllowlistsReadOnlyTools(t *testing.T) {
	runtime := t.TempDir()
	path, err := writeKimiReviewerAgent(runtime, "Use only the Read, Grep, and Glob tools to inspect code.")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != runtime {
		t.Fatalf("agent definition escaped the runtime: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	head, body, found := strings.Cut(strings.TrimPrefix(text, "---\n"), "\n---\n")
	if !found {
		t.Fatalf("no frontmatter:\n%s", text)
	}
	if !strings.Contains(head, "tools: [Read, Grep, Glob]") {
		t.Fatalf("tool allowlist missing:\n%s", head)
	}
	// description is required by kimi-code; a definition without it is rejected at load.
	if !strings.Contains(head, "description:") || !strings.Contains(head, "name: kander-reviewer") {
		t.Fatalf("incomplete frontmatter:\n%s", head)
	}
	// An allowlist is exhaustive, so naming a mutating tool anywhere in it would grant it.
	for _, denied := range []string{"Bash", "Write", "Edit", "WebSearch", "FetchURL", "Agent"} {
		if strings.Contains(head, denied) {
			t.Fatalf("%s must not appear in the allowlist:\n%s", denied, head)
		}
	}
	// The body replaces the system prompt, so it has to carry the inspection rules.
	if !strings.Contains(body, "Use only the Read, Grep, and Glob tools") {
		t.Fatalf("inspection rules missing from the system prompt:\n%s", body)
	}
	if !strings.Contains(body, "never change it") {
		t.Fatalf("read-only role missing from the system prompt:\n%s", body)
	}
}
