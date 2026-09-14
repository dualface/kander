package terminal

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/terminal/terminaltest"
)

// Preconditions keep a declared order and per-variable diagnostics: a
// before_binary variable is checked before the binary lookup, trim ignores
// surrounding whitespace, and skip_auto keeps a variable out of auto.
func TestDeclarativePrepareEnvOrderMessagesAndTrim(t *testing.T) {
	resetLanguage(t)
	backend := declarativeOp(t, OpReadOutput, `{"steps": [{"store": "out", "argv": ["read"]}], "result": {"text": "{step.out.text}"}}`, func(root map[string]any) {
		requires := object(root, "launchers", "faketerm", "requires")
		requires["env"] = []any{
			map[string]any{"name": "FAKE_ENV", "value": "1", "before_binary": true, "message": "not inside fake"},
			map[string]any{"name": "FAKE_WORKSPACE", "trim": true, "skip_auto": true, "message": "workspace {name} missing"},
		}
		object(root, "ops", "create_container")["steps"] = []any{map[string]any{
			"store": "new", "argv": []any{"new", "{env.FAKE_WORKSPACE}"},
			"output": map[string]any{"source": "stdout", "parse": "json_field:result.id"},
			"fields": map[string]any{"container": "regex:^([^/]+)/", "pane": "regex:/(.+)$"},
		}}
	})
	env := map[string]string{}
	getenv := func(name string) string { return env[name] }
	lookups := 0
	request := PrepareRequest{Getenv: getenv, LookPath: func(string) (string, error) {
		lookups++
		return "", errors.New("missing")
	}}
	if _, err := backend.Prepare(request); err == nil || err.Error() != "not inside fake" || lookups != 0 {
		t.Fatalf("before_binary: err=%v lookups=%d", err, lookups)
	}
	env["FAKE_ENV"] = "1"
	if _, err := backend.Prepare(request); err == nil || err.Error() != "faketerm is not on PATH; launcher faketerm cannot start" {
		t.Fatalf("binary after before_binary env: %v", err)
	}
	fake := terminaltest.New(t, reply(`{"result":{"id":"w1/p1"}}`, "new"))
	request.LookPath = func(string) (string, error) { return fake.Program, nil }
	env["FAKE_WORKSPACE"] = "  \t"
	if _, err := backend.Prepare(request); err == nil || err.Error() != "workspace FAKE_WORKSPACE missing" {
		t.Fatalf("trimmed emptiness: %v", err)
	}
	if !backend.AutoDetect(getenv) {
		t.Fatal("skip_auto variable must not block auto detection")
	}
	env["FAKE_WORKSPACE"] = " w1 "
	withEnv, err := NewDeclarativeBackend(backend.def, "faketerm", getenv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := withEnv.Prepare(request); err != nil {
		t.Fatal(err)
	}
	if _, err := withEnv.CreateContainer(fakeConn(fake), Target{}, "/w", "l"); err != nil {
		t.Fatal(err)
	}
	if calls := fake.Calls(t); len(calls) != 1 || calls[0][1] != "w1" {
		t.Fatalf("trimmed placeholder: %q", calls)
	}
}

// Focus steps use the full step machinery: fail steps, messages, prev_ok,
// on_error degradation and the focus step diagnostic.
func TestDeclarativeFocusStepsUseStepMachinery(t *testing.T) {
	resetLanguage(t)
	focusWith := func(t *testing.T, steps string) *DeclarativeBackend {
		t.Helper()
		return declarativeOp(t, OpFocus, `{"steps": `+steps+`}`, nil)
	}
	facts := []terminaltest.Reply{reply(`{"command":"codex","dead":"0","mode":"0"}`, "facts"), reply("s1", "meta")}
	address := Address{Container: "w1", Pane: "p1"}
	cases := []struct {
		name    string
		steps   string
		replies []terminaltest.Reply
		want    FocusResult
		calls   int
	}{
		{"fail step", `[{"when": "field:container=w1", "fail": "cannot focus {container}"}]`, nil,
			FocusResult{ID: "focus.switch_failed", Args: []any{"cannot focus w1"}}, 2},
		{"default diagnostic", `[{"argv": ["tab", "{container}"]}]`, []terminaltest.Reply{{Args: []string{"tab"}, Code: 1, Stdout: "rejected"}},
			FocusResult{ID: "focus.switch_failed", Args: []any{"tab: rejected"}}, 3},
		{"default diagnostic exit status", `[{"argv": ["tab", "{container}"]}]`, []terminaltest.Reply{{Args: []string{"tab"}, Code: 7}},
			FocusResult{ID: "focus.switch_failed", Args: []any{"tab: exit 7"}}, 3},
		{"declared message", `[{"argv": ["tab", "{container}"], "messages": {"exit": "tab {container} failed: {detail}"}}]`, []terminaltest.Reply{{Args: []string{"tab"}, Code: 2}},
			FocusResult{ID: "focus.switch_failed", Args: []any{"tab w1 failed: exit 2"}}, 3},
		{"degraded by on_error", `[{"argv": ["tab", "{container}"]}, {"argv": ["pane", "{pane}"], "on_error": "continue"}, {"when": "prev_failed", "argv": ["fallback"]}]`,
			[]terminaltest.Reply{{Args: []string{"pane"}, Code: 1, Stderr: "no pane focus"}},
			FocusResult{Success: true, ID: "focus.tab_only", Args: []any{"pane: no pane focus"}}, 5},
		{"prev_ok", `[{"argv": ["tab", "{container}"]}, {"when": "prev_ok", "argv": ["pane", "{pane}"]}]`, nil,
			FocusResult{Success: true, ID: "focus.success"}, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withEnv := focusWith(t, tc.steps)
			fake := terminaltest.New(t, append(append([]terminaltest.Reply{}, tc.replies...), facts...)...)
			got := withEnv.Focus(context.Background(), fakeConn(fake), address)
			if got.Success != tc.want.Success || got.ID != tc.want.ID || len(got.Args) != len(tc.want.Args) || (len(got.Args) > 0 && got.Args[0] != tc.want.Args[0]) {
				t.Fatalf("focus=%+v want %+v", got, tc.want)
			}
			if calls := fake.Calls(t); len(calls) != tc.calls {
				t.Fatalf("calls=%q", calls)
			}
		})
	}
}

func TestDeclarativeRowChecksAndLazyProcessName(t *testing.T) {
	resetLanguage(t)
	backend := declarativeOp(t, OpReverseLookup, `{
		"steps": [{"store": "list", "argv": ["list"]}],
		"rows": {
			"from": "list",
			"fields": {"object": "regex:^(\\{)", "pane": "regex:\"pane\":\"([^\"]+)\""},
			"checks": [
				{"when": "!field_missing:row.object", "message": "row is not an object"},
				{"when": "!field_missing:row.pane", "message": "row has no pane"}
			],
			"match": "field:row.pane={reference}",
			"result": {"container": "{row.pane}", "pane": "{row.pane}"}
		}
	}`, nil)
	identity := Identity{Reference: "p1", ProcessName: func() (string, error) { return "", errors.New("agent definition missing") }}
	for stdout, want := range map[string]string{"[1]\n": "row is not an object", `{"id":1}` + "\n": "row has no pane"} {
		fake := terminaltest.New(t, reply(stdout, "list"))
		if _, err := backend.ReverseLookup(context.Background(), fakeConn(fake), identity); err == nil || err.Error() != want {
			t.Fatalf("stdout %q: %v", stdout, err)
		}
	}
	fake := terminaltest.New(t, reply(`{"pane":"p1"}`+"\n", "list"))
	if found, err := backend.ReverseLookup(context.Background(), fakeConn(fake), identity); err != nil || found.Pane != "p1" {
		t.Fatalf("unreferenced process name must not load: %+v %v", found, err)
	}

	// A process name used only by a row message is still resolved.
	messaged := declarativeOp(t, OpReverseLookup, `{
		"steps": [{"store": "list", "argv": ["list"]}],
		"rows": {
			"from": "list",
			"fields": {"pane": "regex:\"pane\":\"([^\"]+)\""},
			"match": "field:row.pane={reference}",
			"result": {"container": "{row.pane}", "pane": "{row.pane}"},
			"messages": {"none": {"or": ["no {process_name} pane"]}}
		}
	}`, nil)
	named := Identity{Reference: "p9", ProcessName: func() (string, error) { return "codex", nil }}
	if _, err := messaged.ReverseLookup(context.Background(), fakeConn(fake), named); err == nil || err.Error() != "no codex pane" {
		t.Fatalf("message process name: %v", err)
	}
}

func TestDeclarativeRejectedValueFollowsOnError(t *testing.T) {
	resetLanguage(t)
	backend := declarativeOp(t, OpDeliverText, `{"steps": [{"argv": ["type", "{text}"], "on_error": "continue"}, {"argv": ["key", "{pane}"]}]}`, nil)
	fake := terminaltest.New(t)
	if err := backend.DeliverText(context.Background(), fakeConn(fake), "p1", "two\nlines"); err != nil {
		t.Fatal(err)
	}
	if calls := fake.Calls(t); len(calls) != 1 || strings.Join(calls[0], " ") != "key p1" {
		t.Fatalf("calls=%q", calls)
	}
}
