package terminal

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/probe"
)

// A step with unset_env runs only through RunEnv: a Run that fails the test
// proves no runner can ignore the list.
func TestDeclarativeUnsetEnvUsesEnvRunner(t *testing.T) {
	backend := declarativeOp(t, OpReadOutput, `{
		"steps":[{"argv":["read","{pane}"],"unset_env":["HC_TERMINAL_ID","HC_OTHER"],"store":"reply"}],
		"result":{"text":"{step.reply.text}"}}`, nil)
	calls := 0
	conn := Conn{Program: "fake", Run: func(context.Context, string, []string) (probe.Result, error) {
		t.Fatal("an unset_env step must not use the plain runner")
		return probe.Result{}, nil
	}, RunEnv: func(_ context.Context, program string, args, unset []string) (probe.Result, error) {
		calls++
		if program != "fake" || !slices.Equal(args, []string{"read", "p1"}) || !slices.Equal(unset, []string{"HC_TERMINAL_ID", "HC_OTHER"}) {
			t.Fatalf("program=%q args=%q unset=%q", program, args, unset)
		}
		return probe.Result{Stdout: "reply"}, nil
	}}
	got, err := backend.ReadOutput(t.Context(), conn, "p1")
	if err != nil || got != "reply" || calls != 1 {
		t.Fatalf("calls=%d output=%q err=%v", calls, got, err)
	}
}

func TestValidateUnsetEnv(t *testing.T) {
	cases := []struct {
		name string
		step map[string]any
		want string
	}{
		{"bad name", map[string]any{"argv": []any{"read"}, "unset_env": []any{"1BAD"}}, "environment variable name"},
		{"name with equals", map[string]any{"argv": []any{"read"}, "unset_env": []any{"A=B"}}, "environment variable name"},
		{"duplicate", map[string]any{"argv": []any{"read"}, "unset_env": []any{"A", "A"}}, "listed twice"},
		{"empty list", map[string]any{"argv": []any{"read"}, "unset_env": []any{}}, "1 to 16"},
		{"too many", map[string]any{"argv": []any{"read"}, "unset_env": numberedNames(17)}, "1 to 16"},
		{"with stdin", map[string]any{"argv": []any{"read"}, "stdin": "x", "unset_env": []any{"A"}}, "stdin"},
		{"on fail step", map[string]any{"fail": map[string]any{"id": "terminal.output_does_not_match"}, "unset_env": []any{"A"}}, "fail"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := mutateFixture(t, func(root map[string]any) {
				op := object(object(root, "ops"), OpReadOutput)
				op["steps"] = []any{tc.step}
				delete(op, "result")
			})
			if _, err := DecodeDefinition("faketerm.json", data); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want %q", err, tc.want)
			}
		})
	}
	valid := mutateFixture(t, func(root map[string]any) {
		op := object(object(root, "ops"), OpReadOutput)
		op["steps"] = []any{map[string]any{"argv": []any{"read"}, "unset_env": numberedNames(16)}}
		delete(op, "result")
	})
	if _, err := DecodeDefinition("faketerm.json", valid); err != nil {
		t.Fatalf("16 names rejected: %v", err)
	}
}

func numberedNames(count int) []any {
	names := make([]any, count)
	for i := range names {
		names[i] = "VAR_" + strings.Repeat("X", i+1)
	}
	return names
}

func TestWithoutEnv(t *testing.T) {
	environ := []string{"HC_TERMINAL_ID=t2b", "HC_SCOPE=x", "EMPTY=", "NOEQUALS", "PATH=/bin:/usr/bin=ok"}
	got := withoutEnv(environ, []string{"HC_TERMINAL_ID", "NOEQUALS"}, false)
	want := []string{"HC_SCOPE=x", "EMPTY=", "PATH=/bin:/usr/bin=ok"}
	if !slices.Equal(got, want) {
		t.Fatalf("got=%q want=%q", got, want)
	}
	if got := withoutEnv(environ, []string{"hc_terminal_id"}, false); len(got) != 5 {
		t.Fatalf("exact match removed a differently cased name: %q", got)
	}
	if got := withoutEnv(environ, []string{"hc_terminal_id"}, true); slices.Contains(got, "HC_TERMINAL_ID=t2b") || len(got) != 4 {
		t.Fatalf("case-insensitive match failed: %q", got)
	}
	if len(environ) != 5 || environ[0] != "HC_TERMINAL_ID=t2b" {
		t.Fatalf("input was modified: %q", environ)
	}
}
