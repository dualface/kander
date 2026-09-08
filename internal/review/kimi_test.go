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

// The private home must keep the user's providers and credentials so the reviewer can
// authenticate, while appending the deny rules that make the run read-only.
func TestPrepareKimiHomeCopiesCredentialsAndAppendsDenyRules(t *testing.T) {
	runtime, source := t.TempDir(), t.TempDir()
	userConfig := "default_model = \"kimi-code/k3\"\n\n[thinking]\neffort = \"max\"\n"
	if err := os.WriteFile(filepath.Join(source, "config.toml"), []byte(userConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "credentials"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "credentials", "kimi-code.json"), []byte(`{"token":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	home, err := prepareKimiHome(runtime, source)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(home, runtime) {
		t.Fatalf("private home escaped the runtime: %s", home)
	}
	data, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `default_model = "kimi-code/k3"`) {
		t.Fatalf("user config not carried over:\n%s", text)
	}
	for _, want := range []string{`pattern = "Bash"`, `pattern = "Write"`, `pattern = "Edit"`, `decision = "deny"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s:\n%s", want, text)
		}
	}
	// A denial only holds if it precedes the allowances: rules are first-match-wins.
	if strings.Index(text, `decision = "deny"`) > strings.Index(text, `pattern = "Read"`) {
		t.Fatalf("deny rules must precede the allowances:\n%s", text)
	}
	credential, err := os.ReadFile(filepath.Join(home, "credentials", "kimi-code.json"))
	if err != nil || string(credential) != `{"token":"secret"}` {
		t.Fatalf("credential=%q err=%v", credential, err)
	}

	// The user's own home must be left untouched.
	original, err := os.ReadFile(filepath.Join(source, "config.toml"))
	if err != nil || string(original) != userConfig {
		t.Fatalf("source config was modified: %q %v", original, err)
	}
}

// Without a source config the run still has to be fenced by the deny rules.
func TestPrepareKimiHomeWithoutSourceConfigStillDenies(t *testing.T) {
	runtime, source := t.TempDir(), t.TempDir()
	home, err := prepareKimiHome(runtime, source)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `pattern = "Bash"`) {
		t.Fatalf("missing deny rules:\n%s", data)
	}
}
