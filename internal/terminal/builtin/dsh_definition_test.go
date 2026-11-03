package builtin

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

const dshHomeSocket = "/home/u/.dsh/kander/mux.sock"

func dshBackendForTest(t *testing.T, env map[string]string) terminal.Backend {
	t.Helper()
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "1")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "en"})
	if env == nil {
		env = map[string]string{"HOME": "/home/u"}
	}
	backend, err := DefinitionBackend("dsh", "dsh", envOf(env))
	if err != nil {
		t.Fatal(err)
	}
	return backend
}

// curlCall is the argv the definition gives curl for one socket operation.
func curlCall(socket []string, op string, fields ...string) []string {
	argv := append([]string{"-sS", "--fail-with-body", "--max-time", "10"}, socket...)
	if len(fields) == 0 {
		argv = append(argv, "-X", "POST")
	}
	for _, field := range fields {
		argv = append(argv, "--data-urlencode", field)
	}
	return append(argv, "http://dsh/op/"+op)
}

var homeSocket = []string{"--unix-socket", dshHomeSocket}

// Every operation is one curl call on the plugin socket; dsh never takes part
// in auto, and the launcher is POSIX only.
func TestDshDefinitionShape(t *testing.T) {
	def, err := Definition("dsh")
	if err != nil {
		t.Fatal(err)
	}
	if def.Binary != "curl" || len(def.Hooks) != 0 {
		t.Fatalf("binary=%q hooks=%v", def.Binary, def.Hooks)
	}
	caps := def.Capabilities
	if !caps.Container || !caps.Focus || !caps.PaneMetadata || !caps.ForegroundProcess || caps.AgentIdentity || caps.WaitOutput || caps.SessionReport {
		t.Fatalf("capabilities=%+v", caps)
	}
	launcher := def.Launchers["dsh"]
	if launcher.Requires.Platform != "posix" || !dshBackendForTest(t, nil).Capabilities().POSIXOnly {
		t.Fatalf("platform=%q", launcher.Requires.Platform)
	}
	if launcher.AutoPriority != 0 {
		t.Fatalf("auto_priority=%d", launcher.AutoPriority)
	}
	for name, op := range def.Ops {
		for index, step := range op.Steps {
			if len(step.Argv) == 0 {
				continue
			}
			if !slices.Contains(step.Argv, "--unix-socket") || !slices.Contains(step.Argv, "--fail-with-body") {
				t.Fatalf("%s step %d does not call the plugin socket: %q", name, index, step.Argv)
			}
		}
	}
	env := envOf(map[string]string{"HOME": "/home/u", "DSH_HOME": "/d", "TMUX": "/tmp/tmux-1/default,1,0"})
	for _, inside := range []bool{false, true} {
		if backend, ok := terminal.ResolveAuto(inside, env); ok && backend.Name() == "dsh" {
			t.Fatal("auto resolved to dsh")
		}
	}
}

// DSH_HOME names the socket directory; without it the socket is under
// ~/.dsh. curl uses the last --unix-socket, and an empty DSH_HOME drops that
// pair.
func TestDshDefinitionSocketPath(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{"home", map[string]string{"HOME": "/home/u"}, homeSocket},
		{"empty DSH_HOME", map[string]string{"HOME": "/home/u", "DSH_HOME": ""}, homeSocket},
		{"DSH_HOME", map[string]string{"HOME": "/home/u", "DSH_HOME": "/srv/dsh"},
			[]string{"--unix-socket", dshHomeSocket, "--unix-socket", "/srv/dsh/kander/mux.sock"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &recorder{reply: func([]string) probe.Result {
				return probe.Result{Stdout: `{"command":"claude","in_mode":"0","dead":"0","session_marker":"s1"}`}
			}}
			if _, err := dshBackendForTest(t, tc.env).PaneFacts(context.Background(), r.conn(), "p1-9"); err != nil {
				t.Fatal(err)
			}
			assertCalls(t, r.calls, [][]string{curlCall(tc.want, "pane_facts", "pane=p1-9")})
		})
	}
}

func TestDshDefinitionCreateChecksTheProtocolFirst(t *testing.T) {
	backend := dshBackendForTest(t, nil)
	r := &recorder{reply: func(args []string) probe.Result {
		if args[len(args)-1] == "http://dsh/op/hello" {
			return probe.Result{Stdout: `{"protocol":"1","upgrade":"","pid":42}`}
		}
		return probe.Result{Stdout: `{"container":"c7-42","pane":"p7-42"}`}
	}}
	got, err := backend.CreateContainer(r.conn(), terminal.Target{Project: "/work"}, "/work/dir", "kb-card")
	if err != nil {
		t.Fatal(err)
	}
	if got != (terminal.Address{Container: "c7-42", Pane: "p7-42"}) {
		t.Fatalf("address=%+v", got)
	}
	assertCalls(t, r.calls, [][]string{
		curlCall(homeSocket, "hello", "protocol=1"),
		curlCall(homeSocket, "create_container", "cwd=/work/dir", "label=kb-card", "project=/work"),
	})
	if window := terminal.FormatAddress(backend, got); window != "dsh:c7-42:p7-42" {
		t.Fatalf("WINDOW=%q", window)
	}
	if parsed, address, ok := terminal.ParseWindow("dsh:c7-42:p7-42"); !ok || parsed.Name() != "dsh" || address != got {
		t.Fatalf("WINDOW parsed as %+v ok=%v", address, ok)
	}
}

// A protocol mismatch or an unreachable host fails before create_container,
// and the error names the side to upgrade or the host to start.
func TestDshDefinitionProtocolFailures(t *testing.T) {
	cases := []struct {
		name  string
		reply probe.Result
		want  string
	}{
		{"newer plugin", probe.Result{Stdout: `{"protocol":"2","upgrade":"kander","pid":42}`}, "speaks dsh launcher protocol 2, but this kander speaks protocol 1: upgrade kander"},
		{"older plugin", probe.Result{Stdout: `{"protocol":"0","upgrade":"plugin","pid":42}`}, "protocol 0, but this kander speaks protocol 1: upgrade the kander-dsh plugin"},
		{"plugin without verdict", probe.Result{Stdout: `{"protocol":"3"}`}, "upgrade the kander-dsh plugin"},
		{"no host", probe.Result{Code: 7, Stderr: "curl: (7) Failed to connect to dsh port 80"}, "no dsh host with the kander-dsh plugin answers"},
		{"plugin without hello", probe.Result{Code: 22, Stdout: `{"error":"unknown op"}`, Stderr: "curl: (22) The requested URL returned error: 404"}, `answers on $DSH_HOME/kander/mux.sock (~/.dsh/kander/mux.sock without DSH_HOME): {"error":"unknown op"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &recorder{reply: func([]string) probe.Result { return tc.reply }}
			_, err := dshBackendForTest(t, nil).CreateContainer(r.conn(), terminal.Target{}, "/work", "kb-card")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v, want %q", err, tc.want)
			}
			if len(r.calls) != 1 {
				t.Fatalf("calls after a failed hello: %q", r.calls)
			}
		})
	}
}

// The plugin answers a missing pane or container with 404 {"error":"gone"}:
// pane facts report gone and the container does not exist. A host that does
// not answer is an ordinary failure, never gone.
func TestDshDefinitionGone(t *testing.T) {
	backend := dshBackendForTest(t, nil)
	ctx := context.Background()
	address := terminal.Address{Container: "c7-42", Pane: "p7-42"}
	gone := &recorder{reply: func([]string) probe.Result {
		return probe.Result{Code: 22, Stdout: `{"error":"gone"}`, Stderr: "curl: (22) The requested URL returned error: 404"}
	}}
	facts, err := backend.PaneFacts(ctx, gone.conn(), "p7-42")
	if err != nil || !facts.Gone {
		t.Fatalf("facts=%+v err=%v", facts, err)
	}
	if exists, err := backend.ContainerExists(ctx, gone.conn(), address); err != nil || exists {
		t.Fatalf("exists=%v err=%v", exists, err)
	}
	down := &recorder{reply: func([]string) probe.Result {
		return probe.Result{Code: 7, Stderr: "curl: (7) Failed to connect to dsh port 80"}
	}}
	facts, err = backend.PaneFacts(ctx, down.conn(), "p7-42")
	var commandErr *terminal.CommandError
	if facts.Gone || !errors.As(err, &commandErr) || !strings.Contains(err.Error(), "kander-dsh pane_facts failed") {
		t.Fatalf("facts=%+v err=%v", facts, err)
	}
}

func TestDshDefinitionOperations(t *testing.T) {
	backend := dshBackendForTest(t, nil)
	ctx := context.Background()
	address := terminal.Address{Container: "c7-42", Pane: "p7-42"}
	cases := []struct {
		name  string
		run   func(terminal.Conn) error
		reply string
		want  [][]string
	}{
		{"run", func(c terminal.Conn) error {
			return backend.RunCommand(c, "p7-42", "'/work dir'", "/abs/claude 'task file'", true)
		}, `{}`, [][]string{curlCall(homeSocket, "run_command", "pane=p7-42", "cwd_quoted='/work dir'", "command=/abs/claude 'task file'", "posix=true")}},
		{"marker", func(c terminal.Conn) error { return backend.SetSessionMarker(c, "p7-42", "s1") },
			`{}`, [][]string{curlCall(homeSocket, "set_session_marker", "pane=p7-42", "value=s1")}},
		{"facts", func(c terminal.Conn) error {
			got, e := backend.PaneFacts(ctx, c, "p7-42")
			if e == nil && got != (terminal.PaneFacts{Command: "claude", InMode: "0", Dead: "0", SessionMarker: "s1"}) {
				t.Fatalf("facts=%+v", got)
			}
			return e
		}, `{"command":"claude","in_mode":"0","dead":"0","session_marker":"s1"}`, [][]string{curlCall(homeSocket, "pane_facts", "pane=p7-42")}},
		{"read", func(c terminal.Conn) error {
			got, e := backend.ReadOutput(ctx, c, "p7-42")
			if e == nil && got != "line one\nready> " {
				t.Fatalf("output=%q", got)
			}
			return e
		}, `{"text":"line one\nready> "}`, [][]string{curlCall(homeSocket, "read_output", "pane=p7-42")}},
		{"deliver", func(c terminal.Conn) error { return backend.DeliverText(ctx, c, "p7-42", "/exit") },
			`{}`, [][]string{curlCall(homeSocket, "deliver_text", "pane=p7-42", "text=/exit"), curlCall(homeSocket, "enter", "pane=p7-42")}},
		{"topology", func(c terminal.Conn) error {
			got, e := backend.Topology(ctx, c, address)
			if e == nil && (got.Container != "c7-42" || got.PaneCount != "1") {
				t.Fatalf("topology=%+v", got)
			}
			return e
		}, `{"container":"c7-42","pane_count":"1"}`, [][]string{curlCall(homeSocket, "topology", "pane=p7-42")}},
		{"exists", func(c terminal.Conn) error {
			got, e := backend.ContainerExists(ctx, c, address)
			if e == nil && !got {
				t.Fatal("live container reported missing")
			}
			return e
		}, `{"container":"c7-42"}`, [][]string{curlCall(homeSocket, "container_exists", "container=c7-42")}},
		{"reverse lookup", func(c terminal.Conn) error {
			got, e := backend.ReverseLookup(ctx, c, terminal.Identity{Agent: "claude", Reference: "s1", ProcessName: func() (string, error) { return "claude", nil }})
			if e == nil && got != address {
				t.Fatalf("address=%+v", got)
			}
			return e
		}, `{"panes":[{"pane":"p6-42","container":"c6-42","command":"claude","dead":"1","marker":"s1"},` +
			`{"pane":"p7-42","container":"c7-42","command":"claude","dead":"0","marker":"s1"},` +
			`{"pane":"p8-42","container":"c8-42","command":"claude","dead":"0","marker":"s2"}]}`,
			[][]string{curlCall(homeSocket, "list")}},
		{"close", func(c terminal.Conn) error { return backend.CloseContainer(ctx, c, address) },
			`{}`, [][]string{curlCall(homeSocket, "close_container", "container=c7-42")}},
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
