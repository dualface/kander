package builtin

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

func herdrCoreBackendForTest(t *testing.T, env map[string]string) terminal.Backend {
	t.Helper()
	backend, err := DefinitionBackend("herdr-core", HerdrCore, envOf(env))
	if err != nil {
		t.Fatal(err)
	}
	return backend
}

const hcNotFound = `{"error":{"hint":"see ` + "`hc ls --all`" + `","kind":"not_found","message":"no terminal \"t8\""}}`

func hcGet(state, status string, labels string) string {
	return `{"address":"t8","argv":["/bin/sh"],"labels":` + labels + `,"reported":` + map[bool]string{true: "true", false: "false"}[status != "null"] +
		`,"state":"` + state + `","status":` + status + `,"turn":0}`
}

// Every operation calls only hc and reads its --json output; no hook, no
// focus and no session report are declared, and the launcher is POSIX only.
func TestHerdrCoreDefinitionShape(t *testing.T) {
	def, err := Definition("herdr-core")
	if err != nil {
		t.Fatal(err)
	}
	if def.Binary != HerdrCoreExecutable || len(def.Hooks) != 0 {
		t.Fatalf("binary=%q hooks=%v", def.Binary, def.Hooks)
	}
	caps := def.Capabilities
	if caps.Focus || caps.SessionReport || !caps.AgentIdentity || !caps.ProgramStatus || caps.ForegroundProcess {
		t.Fatalf("capabilities=%+v", caps)
	}
	launcher := def.Launchers[HerdrCore]
	if launcher.Requires.Platform != "posix" || !herdrCoreBackendForTest(t, nil).Capabilities().POSIXOnly {
		t.Fatalf("platform=%q", launcher.Requires.Platform)
	}
	if launcher.AutoPriority <= 100 || launcher.AutoPriority >= 150 {
		t.Fatalf("auto_priority=%d, want between tmux (100) and luvus (150)", launcher.AutoPriority)
	}
	if len(launcher.Requires.Env) != 1 || launcher.Requires.Env[0].Name != "HC_TERMINAL_ID" || !launcher.Requires.Env[0].AutoOnly {
		t.Fatalf("env=%+v", launcher.Requires.Env)
	}
	for name, op := range def.Ops {
		for index, step := range op.Steps {
			if len(step.Argv) == 0 {
				continue
			}
			if !slices.Contains(step.Argv, "--json") {
				t.Fatalf("%s step %d does not read --json output: %q", name, index, step.Argv)
			}
		}
	}
}

func TestHerdrCoreDefinitionArgvAndParsing(t *testing.T) {
	backend := herdrCoreBackendForTest(t, nil)
	ctx := context.Background()
	address := terminal.Address{Container: "t8", Pane: "t8"}
	cases := []struct {
		name  string
		run   func(terminal.Conn) error
		reply string
		want  [][]string
	}{
		{"ready", func(c terminal.Conn) error { return backend.WaitReady(c, "t8", 15000) },
			`{"complete":true,"how":"screen","text":"\n$ "}`, [][]string{{"read", "--json", "t8"}}},
		{"run", func(c terminal.Conn) error {
			return backend.RunCommand(c, "t8", "'/work dir'", "/abs/claude 'task file'", true)
		},
			`{"started":null,"status":null,"turn":0}`, [][]string{{"send", "--json", "t8", "cd -- '/work dir' && exec /abs/claude 'task file'"}}},
		{"marker", func(c terminal.Conn) error { return backend.SetSessionMarker(c, "t8", "s1") },
			`{}`, [][]string{{"label", "--json", "t8", "kander-session=s1"}}},
		{"facts", func(c terminal.Conn) error {
			got, e := backend.PaneFacts(ctx, c, "t8")
			want := terminal.PaneFacts{SessionMarker: "s1", AgentSession: "s1", AgentStatus: "idle", Container: "t8", ProgramStatus: true, AgentApp: "claude-code"}
			if e == nil && got != want {
				t.Fatalf("facts=%+v", got)
			}
			return e
		}, hcGet("live", `{"app":"claude-code","state":"idle"}`, `{"kander-session":"s1"}`), [][]string{{"get", "--json", "t8"}}},
		{"read", func(c terminal.Conn) error {
			got, e := backend.ReadOutput(ctx, c, "t8")
			if e == nil && got != "line one\nline two" {
				t.Fatalf("output=%q", got)
			}
			return e
		}, `{"complete":true,"how":"screen","text":"line one\nline two"}`, [][]string{{"read", "--json", "t8"}}},
		{"deliver", func(c terminal.Conn) error { return backend.DeliverText(ctx, c, "t8", "/exit") },
			`{"started":true,"status":"idle","turn":1}`, [][]string{{"send", "--json", "t8", "/exit"}}},
		{"topology", func(c terminal.Conn) error {
			got, e := backend.Topology(ctx, c, address)
			if e == nil && (got.Container != "t8" || !slices.Equal(got.Panes, []string{"t8"})) {
				t.Fatalf("topology=%+v", got)
			}
			return e
		}, `{"hidden_finished":0,"terminals":[{"address":"t3","state":"live"},{"address":"t8","state":"live"}]}`, [][]string{{"ls", "--json"}}},
		{"reverse lookup", func(c terminal.Conn) error {
			got, e := backend.ReverseLookup(ctx, c, terminal.Identity{Agent: "claude", Reference: "s1"})
			if e == nil && got != address {
				t.Fatalf("address=%+v", got)
			}
			return e
		}, `{"jsonrpc":"2.0","id":1,"result":{"hidden_finished":0,"terminals":[{"address":"t8","state":"live","labels":{"kander-session":"s1"}}]}}`, [][]string{{"connect", "--json"}}},
		{"exists", func(c terminal.Conn) error {
			got, e := backend.ContainerExists(ctx, c, address)
			if e == nil && !got {
				t.Fatal("live terminal reported missing")
			}
			return e
		}, hcGet("live", "null", `{}`), [][]string{{"get", "--json", "t8"}}},
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

// The new terminal has no parent, is removed when its program exits, starts
// in the startup directory with a unique name and a label naming the card,
// and runs a POSIX shell that inherits the forwarded Kander environment; an
// unset variable is not forwarded.
func TestHerdrCoreDefinitionCreate(t *testing.T) {
	backend := herdrCoreBackendForTest(t, map[string]string{"PATH": "/usr/bin:/home/u/.local/bin", "KANDER_CONFIG": "/cfg/config.json"})
	r := &recorder{reply: func([]string) probe.Result {
		return probe.Result{Stdout: `{"address":"t8","agent":"sh","cwd":"/work","name":"kb-x-1a2b3c4d","state":"live","turn":0}`}
	}}
	got, err := backend.CreateContainer(r.conn(), terminal.Target{}, "/work", "kb-Add herdr-core 启动器")
	if err != nil {
		t.Fatal(err)
	}
	if got != (terminal.Address{Container: "t8", Pane: "t8"}) {
		t.Fatalf("address=%+v", got)
	}
	if len(r.calls) != 1 {
		t.Fatalf("calls=%q", r.calls)
	}
	argv := r.calls[0]
	name := argv[slices.Index(argv, "--name")+1]
	if !regexp.MustCompile(`^kb-add-herdr-core-[0-9a-f]{8}$`).MatchString(name) {
		t.Fatalf("name=%q", name)
	}
	want := []string{"run", "--bg", "--no-parent", "--rm", "--json", "--cwd", "/work", "--name", name,
		"--label", "kander-window=kb-Add herdr-core 启动器", "--", "/usr/bin/env",
		"PATH=/usr/bin:/home/u/.local/bin", "KANDER_CONFIG=/cfg/config.json", "/bin/sh"}
	assertCalls(t, r.calls, [][]string{want})
	if opaque := terminal.FormatAddress(backend, got); opaque != "herdr-core:t8" {
		t.Fatalf("WINDOW=%q", opaque)
	}
	if parsed, address, ok := terminal.ParseWindow("herdr-core:t8"); !ok || parsed.Name() != HerdrCore || address != got {
		t.Fatalf("WINDOW herdr-core:t8 parsed as %+v ok=%v", address, ok)
	}
	// A second launch of the same card gets another name: a takeover opens
	// its terminal while the old one still lives.
	second := &recorder{reply: r.reply}
	if _, err := backend.CreateContainer(second.conn(), terminal.Target{}, "/work", "kb-Add herdr-core 启动器"); err != nil {
		t.Fatal(err)
	}
	if other := second.calls[0][slices.Index(second.calls[0], "--name")+1]; other == name {
		t.Fatalf("two launches share the name %q", name)
	}
}

// Prepare does not require HC_TERMINAL_ID: an explicit --launcher herdr-core
// works outside an hc terminal, only auto needs it.
func TestHerdrCoreDefinitionPrepareOutsideTerminal(t *testing.T) {
	backend := herdrCoreBackendForTest(t, nil)
	request := terminal.PrepareRequest{Getenv: envOf(nil), LookPath: func(string) (string, error) { return "/bin/hc", nil }}
	target, err := backend.Prepare(request)
	if err != nil || target.Program != "/bin/hc" {
		t.Fatalf("target=%+v err=%v", target, err)
	}
	request.LookPath = func(string) (string, error) { return "", errors.New("missing") }
	if _, err := backend.Prepare(request); err == nil || !strings.Contains(err.Error(), "hc") {
		t.Fatalf("missing hc: %v", err)
	}
	if backend.AutoDetect(envOf(nil)) || !backend.AutoDetect(envOf(map[string]string{"HC_TERMINAL_ID": "t3"})) {
		t.Fatal("auto detection does not follow HC_TERMINAL_ID")
	}
}

// One table covers the OSC 7501 state rules of a herdr-core terminal: a
// finished or lost terminal is gone whatever its status says; a live one
// passes Kander's own state words through, reports error as blocked with the
// reason kept, and names no agent while its status is null.
func TestHerdrCoreDefinitionStatusRules(t *testing.T) {
	backend := herdrCoreBackendForTest(t, nil)
	cases := []struct {
		name, state, status string
		gone                bool
		app, agentStatus    string
		reason              string
	}{
		{"finished", "finished", `{"app":"claude-code","state":"idle"}`, true, "", "", ""},
		{"lost", "lost", `{"app":"claude-code","state":"working"}`, true, "", "", ""},
		{"working", "live", `{"app":"claude-code","state":"working"}`, false, "claude-code", "working", ""},
		{"blocked", "live", `{"app":"grok-build","state":"blocked","kind":"permission"}`, false, "grok-build", "blocked", ""},
		{"idle", "live", `{"app":"codex","state":"idle"}`, false, "codex", "idle", ""},
		{"done", "live", `{"app":"claude-code","state":"done"}`, false, "claude-code", "done", ""},
		{"error", "live", `{"app":"codex","state":"error"}`, false, "codex", "blocked", "error"},
		{"unreported", "live", "null", false, "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &recorder{reply: func([]string) probe.Result { return probe.Result{Stdout: hcGet(tc.state, tc.status, `{}`)} }}
			facts, err := backend.PaneFacts(context.Background(), r.conn(), "t8")
			if err != nil {
				t.Fatal(err)
			}
			if facts.Gone != tc.gone {
				t.Fatalf("gone=%v facts=%+v", facts.Gone, facts)
			}
			if tc.gone {
				return
			}
			if !facts.ProgramStatus || facts.AgentApp != tc.app || facts.AgentStatus != tc.agentStatus || facts.AgentStatusReason != tc.reason {
				t.Fatalf("facts=%+v", facts)
			}
		})
	}
}

// A removed terminal is gone, and closing it is not an error: an exited agent
// already took its --rm terminal away.
func TestHerdrCoreDefinitionRemovedTerminal(t *testing.T) {
	backend := herdrCoreBackendForTest(t, nil)
	ctx := context.Background()
	address := terminal.Address{Container: "t8", Pane: "t8"}
	r := &recorder{reply: func([]string) probe.Result { return probe.Result{Code: 3, Stderr: hcNotFound} }}
	facts, err := backend.PaneFacts(ctx, r.conn(), "t8")
	if err != nil || !facts.Gone {
		t.Fatalf("facts=%+v err=%v", facts, err)
	}
	if exists, err := backend.ContainerExists(ctx, r.conn(), address); err != nil || exists {
		t.Fatalf("exists=%v err=%v", exists, err)
	}
	r.calls = nil
	if err := backend.CloseContainer(ctx, r.conn(), address); err != nil {
		t.Fatal(err)
	}
	assertCalls(t, r.calls, [][]string{{"kill", "--json", "t8"}, {"rm", "--json", "t8"}})
	// Any other failure still fails the close.
	r = &recorder{reply: func([]string) probe.Result {
		return probe.Result{Code: 5, Stderr: `{"error":{"kind":"unavailable","message":"core is down"}}`}
	}}
	if err := backend.CloseContainer(ctx, r.conn(), address); err == nil {
		t.Fatal("close succeeded while hc is unavailable")
	}
}

// Without a recorded session the lookup completes with zero matches instead
// of matching every terminal.
func TestHerdrCoreDefinitionLookupWithoutReference(t *testing.T) {
	r := &recorder{reply: func([]string) probe.Result {
		return probe.Result{Stdout: `{"jsonrpc":"2.0","id":1,"result":{"hidden_finished":0,"terminals":[{"address":"t3","state":"live"},{"address":"t8","state":"live"}]}}`}
	}}
	_, err := herdrCoreBackendForTest(t, nil).ReverseLookup(context.Background(), r.conn(), terminal.Identity{Agent: "claude"})
	var match *terminal.MatchError
	if !errors.As(err, &match) || match.Matches != 0 {
		t.Fatalf("err=%v", err)
	}
}

// auto prefers the innermost terminal environment: herdr and luvus panes may
// run inside a herdr-core terminal, while TMUX can leak into every terminal of
// a core started from tmux.
func TestHerdrCoreAutoResolution(t *testing.T) {
	inHC := map[string]string{"HC_TERMINAL_ID": "t3"}
	with := func(extra map[string]string) map[string]string {
		out := map[string]string{"HC_TERMINAL_ID": "t3"}
		for key, value := range extra {
			out[key] = value
		}
		return out
	}
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"herdr-core only", inHC, HerdrCore},
		{"herdr too", with(map[string]string{"HERDR_ENV": "1", "HERDR_WORKSPACE_ID": "w1"}), Herdr},
		{"luvus too", with(map[string]string{"LUVUS_ENV": "1", "LUVUS_PANE_ID": "3"}), Luvus},
		{"tmux too", with(map[string]string{"TMUX": "/tmp/tmux-1/default,1,0"}), HerdrCore},
		{"none", map[string]string{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend, ok := terminal.ResolveAuto(false, envOf(tc.env))
			got := ""
			if ok {
				got = backend.Name()
			}
			if got != tc.want {
				t.Fatalf("auto=%q, want %q", got, tc.want)
			}
		})
	}
}
