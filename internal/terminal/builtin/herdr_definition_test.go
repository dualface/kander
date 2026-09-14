package builtin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

// These argv values are the herdr Go backend's contract at 2de9628.
func TestHerdrDefinitionArgvMatchesGoBackend(t *testing.T) {
	backend, err := DefinitionBackend("herdr", "herdr", envOf(map[string]string{"HERDR_ENV": "1", "HERDR_WORKSPACE_ID": "w1"}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	address := terminal.Address{Container: "w1:t1", Pane: "w1:p1"}
	cases := []struct {
		name  string
		run   func(terminal.Conn) error
		reply string
		want  [][]string
	}{
		{"create", func(c terminal.Conn) error {
			got, e := backend.CreateContainer(c, terminal.Target{}, "/project path", "task name")
			if e == nil && got != address {
				t.Fatalf("address=%+v", got)
			}
			return e
		}, `{"result":{"tab":{"tab_id":"w1:t1"},"root_pane":{"pane_id":"w1:p1"}}}`, [][]string{{"tab", "create", "--workspace", "w1", "--cwd", "/project path", "--label", "task name", "--no-focus"}}},
		{"ready", func(c terminal.Conn) error { return backend.WaitReady(c, address.Pane) }, "", [][]string{{"pane", "wait-output", "w1:p1", "--regex", `\S`, "--source", "visible", "--timeout", "15000"}}},
		{"run", func(c terminal.Conn) error { return backend.RunCommand(c, address.Pane, "agent 'task file'", true) }, "", [][]string{{"pane", "run", "w1:p1", "agent 'task file'"}}},
		{"facts", func(c terminal.Conn) error { _, e := backend.PaneFacts(ctx, c, address.Pane); return e }, `{"result":{"pane":{"pane_id":"w1:p1"}}}`, [][]string{{"pane", "get", "w1:p1"}}},
		{"read", func(c terminal.Conn) error {
			got, e := backend.ReadOutput(ctx, c, address.Pane)
			if e == nil && got != "output" {
				t.Fatalf("output=%q", got)
			}
			return e
		}, "output", [][]string{{"pane", "read", "w1:p1"}}},
		{"literal wait", func(c terminal.Conn) error { return backend.WaitOutput(ctx, c, address.Pane, "marker.*", 123) }, "", [][]string{{"pane", "wait-output", "w1:p1", "--match", "marker.*", "--source", "recent", "--timeout", "123"}}},
		{"regex wait", func(c terminal.Conn) error { return backend.WaitOutput(ctx, c, address.Pane, `regex:READY\s+NOW`, 123) }, "", [][]string{{"pane", "wait-output", "w1:p1", "--regex", `READY\s+NOW`, "--source", "recent", "--timeout", "123"}}},
		{"notify", func(c terminal.Conn) error {
			return backend.DeliverText(ctx, c, address.Pane, "# kander-notify: /tmp/task file")
		}, "", [][]string{{"agent", "prompt", "w1:p1", "# kander-notify: /tmp/task file"}}},
		{"dismiss", func(c terminal.Conn) error { return backend.DeliverText(ctx, c, address.Pane, "/exit") }, "", [][]string{{"agent", "prompt", "w1:p1", "/exit"}}},
		{"close", func(c terminal.Conn) error { return backend.CloseContainer(ctx, c, address) }, "", [][]string{{"tab", "close", "w1:t1"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &recorder{reply: func([]string) probe.Result { return probe.Result{Stdout: tc.reply} }}
			if err := tc.run(r.conn()); err != nil {
				t.Fatal(err)
			}
			assertCalls(t, r.calls, tc.want)
		})
	}
}

func TestHerdrDefinitionErrorsAndIdentity(t *testing.T) {
	backend := herdrBackendForTest()
	for _, tc := range []struct {
		name, stdout, stderr string
		code                 int
		gone                 bool
		kind                 terminal.ErrorKind
		detail               string
	}{
		{"stdout gone", `{"error":{"code":"pane_not_found"}}`, "", 1, true, 0, "exit 1"},
		{"stderr priority", `{"error":{"code":"pane_not_found"}}`, `{"error":{"code":"permission_denied"}}`, 1, false, terminal.KindExit, ""},
		{"stdout fallback", `{"error":{"code":"pane_not_found"}}`, `{"error":{"code":""}}`, 1, true, 0, `{"error":{"code":""}}`},
		{"plain failure", "", "pane not found", 1, false, terminal.KindExit, ""},
		{"invalid JSON", "not-json", "", 0, false, terminal.KindNotJSON, ""},
		{"non-object", "[]", "", 0, false, terminal.KindNotObject, ""},
		{"missing result", "{}", "", 0, false, terminal.KindMissingResult, ""},
		{"different pane", `{"result":{"pane":{"pane_id":"w1:p2"}}}`, "", 0, false, terminal.KindInvalidResponse, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &recorder{reply: func([]string) probe.Result { return probe.Result{Code: tc.code, Stdout: tc.stdout, Stderr: tc.stderr} }}
			got, err := backend.PaneFacts(context.Background(), r.conn(), "w1:p1")
			if got.Gone != tc.gone || got.GoneDetail != tc.detail {
				t.Fatalf("facts=%+v", got)
			}
			if tc.gone {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			command, ok := terminal.AsCommandError(err)
			if !ok || command.Kind != tc.kind {
				t.Fatalf("error=%v; kind=%v", err, tc.kind)
			}
		})
	}
	r := &recorder{reply: func([]string) probe.Result {
		return probe.Result{Stdout: `{"result":{"pane":{"pane_id":"w1:p1","tab_id":"w1:t1","agent":"codex","agent_status":"blocked","agent_session":{"value":"s1"}}}}`}
	}}
	got, err := backend.PaneFacts(context.Background(), r.conn(), "w1:p1")
	if err != nil || got != (terminal.PaneFacts{Agent: "codex", AgentStatus: "blocked", AgentSession: "s1", Container: "w1:t1"}) {
		t.Fatalf("facts=%+v error=%v", got, err)
	}
}

func TestHerdrDefinitionCreateMissingPaneClosesTab(t *testing.T) {
	backend := herdrBackendForTest()
	for _, closeFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "closed", true: "close failed"}[closeFails], func(t *testing.T) {
			r := &recorder{reply: func(args []string) probe.Result {
				if args[1] == "create" {
					return probe.Result{Stdout: `{"result":{"tab":{"tab_id":"w1:t1"}}}`}
				}
				if closeFails {
					return probe.Result{Code: 1, Stderr: "cannot close"}
				}
				return probe.Result{}
			}}
			_, err := backend.CreateContainer(r.conn(), terminal.Target{}, "/project", "task")
			if err == nil {
				t.Fatal("missing pane accepted")
			}
			if len(r.calls) != 2 || strings.Join(r.calls[1], " ") != "tab close w1:t1" {
				t.Fatalf("calls=%q", r.calls)
			}
			if closeFails && !strings.Contains(err.Error(), "cannot close") {
				t.Fatalf("cleanup failure lost: %v", err)
			}
		})
	}
}

func TestHerdrDefinitionFocusHookOutcomes(t *testing.T) {
	for _, status := range []terminal.HookStatus{terminal.HookOK, terminal.HookDegraded, terminal.HookFailed} {
		name := t.TempDir()
		calls := 0
		var r *recorder
		terminal.RegisterHook(name, func(ctx context.Context, call terminal.HookCall) terminal.HookResult {
			calls++
			if r == nil || len(r.calls) != 2 {
				t.Fatal("pane hook ran before container focus")
			}
			if call.Point != terminal.HookPointFocusPane || call.Values["pane"] != "w1:p1" {
				t.Fatalf("call=%+v", call)
			}
			return terminal.HookResult{Status: status, Note: "socket unavailable", Err: errors.New("hook failed")}
		})
		def, err := Definition("herdr")
		if err != nil {
			t.Fatal(err)
		}
		def.Hooks[terminal.HookPointFocusPane] = name
		backend, err := terminal.NewDeclarativeBackend(def, "herdr", os.Getenv)
		if err != nil {
			t.Fatal(err)
		}
		r = &recorder{reply: func(args []string) probe.Result {
			if args[0] == "pane" {
				return probe.Result{Stdout: `{"result":{"pane":{"pane_id":"w1:p1"}}}`}
			}
			return probe.Result{}
		}}
		got := backend.Focus(context.Background(), r.conn(), terminal.Address{Container: "w1:t1", Pane: "w1:p1"})
		want := map[terminal.HookStatus]string{terminal.HookOK: "focus.success", terminal.HookDegraded: "focus.tab_only", terminal.HookFailed: "focus.switch_failed"}[status]
		if got.ID != want || got.Success != (status != terminal.HookFailed) || calls != 1 {
			t.Fatalf("status=%v result=%+v calls=%d", status, got, calls)
		}
		assertCalls(t, r.calls, [][]string{{"pane", "get", "w1:p1"}, {"tab", "focus", "w1:t1"}})
	}
}

func TestHerdrDefinitionUnknownHooksAndInventory(t *testing.T) {
	registered, ok := terminal.Lookup(Herdr)
	if !ok {
		t.Fatal("herdr not registered")
	}
	if _, ok := registered.(*terminal.DeclarativeBackend); !ok {
		t.Fatalf("herdr registered as %T", registered)
	}
	for _, point := range []string{terminal.HookPointFocusPane, terminal.HookPointReportSession} {
		def, err := Definition("herdr")
		if err != nil {
			t.Fatal(err)
		}
		def.Hooks[point] = "unregistered-herdr-hook"
		source := filepath.Join(t.TempDir(), "herdr.json")
		err = terminal.ValidateDefinition(source, def)
		for _, part := range []string{source, "hooks." + point, "unregistered-herdr-hook"} {
			if err == nil || !strings.Contains(err.Error(), part) {
				t.Fatalf("error=%v; missing %q", err, part)
			}
		}
	}
	docs, err := os.ReadFile("../../../docs/terminal-definitions.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range builtinHooks() {
		if _, ok := terminal.LookupHook(entry.name); !ok {
			t.Fatalf("hook %s not registered", entry.name)
		}
		if !strings.Contains(string(docs), "| `"+entry.name+"`") {
			t.Errorf("hook %s absent from inventory", entry.name)
		}
	}
}

func TestHerdrDefinitionAddressAndLookupPatterns(t *testing.T) {
	backend := herdrBackendForTest()
	for _, tc := range []struct {
		address string
		valid   bool
	}{
		{"herdr:w1:t1:w1:p1", true},
		{"herdr:workspace:tabs:workspace:panes", true},
		{"herdr:w1:t1:w1:p ", false},
		{"herdr:w1:t1:w1:p\t", false},
		{"herdr:w1:t1:w1:p\n", false},
		{"herdr:t1:p1", false},
	} {
		if _, valid := backend.ParseAddress(tc.address); valid != tc.valid {
			t.Errorf("address=%q valid=%v; want=%v", tc.address, valid, tc.valid)
		}
	}
	for _, tc := range []struct {
		panes string
		valid bool
	}{
		{"[{\"tab_id\":\"workspace:tabs\",\"pane_id\":\"workspace:panes\",\"agent\":\"codex\",\"agent_session\":{\"value\":\"s1\"}}]", true},
		{"[{\"tab_id\":\"w1:t1\",\"pane_id\":\"w1:p \",\"agent\":\"codex\",\"agent_session\":{\"value\":\"s1\"}}]", false},
		{"[{\"tab_id\":\"w1:t1\",\"pane_id\":\"w1:p1\",\"agent\":\"codex\",\"agent_session\":42}]", false},
	} {
		r := &recorder{reply: func([]string) probe.Result {
			return probe.Result{Stdout: "{\"result\":{\"panes\":" + tc.panes + "}}"}
		}}
		got, err := backend.ReverseLookup(context.Background(), r.conn(), terminal.Identity{Agent: "codex", Reference: "s1"})
		if (err == nil) != tc.valid {
			t.Fatalf("panes=%s address=%+v error=%v", tc.panes, got, err)
		}
	}
	r := &recorder{reply: func([]string) probe.Result { return probe.Result{Stdout: "{\"result\":{\"pane\":{}}}"} }}
	if _, err := backend.PaneFacts(context.Background(), r.conn(), ""); err == nil {
		t.Fatal("empty requested and returned identities accepted")
	}
}
