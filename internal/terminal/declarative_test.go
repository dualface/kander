package terminal

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal/terminaltest"
)

func recordingConn(got *[]string) Conn {
	return Conn{Program: "faketerm", Run: func(_ context.Context, _ string, args []string) (probe.Result, error) {
		*got = append([]string{}, args...)
		return probe.Result{}, nil
	}}
}

func fakeConn(fake *terminaltest.Fake) Conn {
	return Conn{Program: fake.Program, Run: ProbeRunner}
}

func noEnv(string) string { return "" }

func wantCalls(t *testing.T, fake *terminaltest.Fake, want ...[]string) {
	t.Helper()
	if got := fake.Calls(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls=%q\nwant=%q", got, want)
	}
}

func reply(stdout string, args ...string) terminaltest.Reply {
	return terminaltest.Reply{Args: args, Stdout: stdout}
}

// Every Backend operation of the fixture runs through the real process
// runners against the fake executable.
func TestDeclarativeBackendOperations(t *testing.T) {
	resetLanguage(t)
	ctx := context.Background()
	backend := fixtureBackend(t, noEnv)
	fake := terminaltest.New(t,
		reply(`{"result":{"id":"w1/p1"}}`, "new"),
		reply("prompt>", "ready"),
		reply(`{"command":"codex","dead":"0","mode":"0"}`, "facts"),
		reply("session-1 extra\n", "meta", "get"),
		reply("line one\nMARKER here\n", "read"),
		reply(`{"container":"w1","count":"2"}`, "topology"),
		reply("w1 p1 other codex\nw1 p2 session-1 codex\n", "list"),
	)
	conn := fakeConn(fake)
	spawn := Conn{Program: fake.Program, Run: SpawnRunner}

	address, err := backend.CreateContainer(spawn, Target{}, "/work dir", "task")
	if err != nil || address != (Address{Container: "w1", Pane: "p1"}) {
		t.Fatalf("create=%+v err=%v", address, err)
	}
	wantCalls(t, fake, []string{"new", "--cwd", "/work dir", "--label", "task"})
	if err := backend.WaitReady(spawn, "p1"); err != nil {
		t.Fatal(err)
	}
	wantCalls(t, fake, []string{"ready", "p1"})
	if err := backend.RunCommand(spawn, "p1", "codex --flag", true); err != nil {
		t.Fatal(err)
	}
	wantCalls(t, fake, []string{"run", "p1", "codex --flag"})
	if err := backend.SetSessionMarker(spawn, "p1", "session-1"); err != nil {
		t.Fatal(err)
	}
	wantCalls(t, fake, []string{"meta", "set", "p1", "session-1"})
	facts, err := backend.PaneFacts(ctx, conn, "p1")
	if err != nil || facts != (PaneFacts{Command: "codex", Dead: "0", InMode: "0", SessionMarker: "session-1"}) {
		t.Fatalf("facts=%+v err=%v", facts, err)
	}
	wantCalls(t, fake, []string{"facts", "p1"}, []string{"meta", "get", "p1"})
	text, err := backend.ReadOutput(ctx, conn, "p1")
	if err != nil || text != "line one\nMARKER here\n" {
		t.Fatalf("text=%q err=%v", text, err)
	}
	fake.Calls(t)
	if err := backend.WaitOutput(ctx, conn, "p1", "MARKER", 30000); err != nil {
		t.Fatal(err)
	}
	if err := backend.WaitOutput(ctx, conn, "p1", `regex:MARK\w+`, 30000); err != nil {
		t.Fatal(err)
	}
	fake.Calls(t)
	if err := backend.DeliverText(ctx, conn, "p1", "hello"); err != nil {
		t.Fatal(err)
	}
	wantCalls(t, fake, []string{"type", "p1", "hello"}, []string{"key", "p1", "Enter"})
	topology, err := backend.Topology(ctx, conn, address)
	if err != nil || !reflect.DeepEqual(topology, Topology{Container: "w1", PaneCount: "2"}) {
		t.Fatalf("topology=%+v err=%v", topology, err)
	}
	exists, err := backend.ContainerExists(ctx, conn, address)
	if err != nil || !exists {
		t.Fatalf("exists=%v err=%v", exists, err)
	}
	found, err := backend.ReverseLookup(ctx, conn, Identity{Reference: "session-1", ProcessName: func() (string, error) { return "codex", nil }})
	if err != nil || found != (Address{Container: "w1", Pane: "p2"}) {
		t.Fatalf("lookup=%+v err=%v", found, err)
	}
	fake.Calls(t)
	if result := backend.Focus(ctx, conn, address); !result.Success || result.ID != "focus.success" {
		t.Fatalf("focus=%+v", result)
	}
	wantCalls(t, fake, []string{"facts", "p1"}, []string{"meta", "get", "p1"}, []string{"focus", "w1", "p1"})
	if err := backend.CloseContainer(ctx, conn, address); err != nil {
		t.Fatal(err)
	}
	wantCalls(t, fake, []string{"close", "w1"})
	if lines := backend.StartedLines("started", Target{}, address, noEnv); !reflect.DeepEqual(lines, []string{"started pane=p1"}) {
		t.Fatalf("lines=%q", lines)
	}
	if got := FormatAddress(backend, address); got != "faketerm:w1:p1" {
		t.Fatalf("window=%q", got)
	}
	if parsed, ok := backend.ParseAddress("faketerm:w1:p1"); !ok || parsed != address {
		t.Fatalf("parsed=%+v ok=%v", parsed, ok)
	}
	if err := backend.ReportSession(SessionReport{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("report=%v", err)
	}
}

// declarativeOp builds a backend whose read_output operation is replaced by
// the given JSON, for exercising one composition feature at a time.
func declarativeOp(t *testing.T, op string, body string, change func(root map[string]any)) *DeclarativeBackend {
	t.Helper()
	data := mutateFixture(t, func(root map[string]any) {
		var decoded any
		if err := jsonUnmarshal(body, &decoded); err != nil {
			t.Fatal(err)
		}
		object(root, "ops")[op] = decoded
		if change != nil {
			change(root)
		}
	})
	def, err := DecodeDefinition("faketerm.json", data)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := NewDeclarativeBackend(def, "faketerm", noEnv)
	if err != nil {
		t.Fatal(err)
	}
	return backend
}

func TestDeclarativeStepComposition(t *testing.T) {
	resetLanguage(t)
	ctx := context.Background()
	backend := declarativeOp(t, OpReadOutput, `{
		"steps": [
			{"store": "first", "argv": ["first"], "output": {"source": "stdout", "parse": "regex:id=(\\w+)"}},
			{"store": "optional", "argv": ["optional", "{step.first.text}"], "on_error": "continue"},
			{"when": "prev_failed", "argv": ["after-failure"]},
			{"when": "field:step.optional.ok=true", "argv": ["skipped-when-failed"]},
			{"store": "meta", "argv": ["meta"], "on_error": "meta_missing", "stop_on_success": true},
			{"store": "meta", "argv": ["legacy"], "on_error": "meta_missing", "stop_on_success": true},
			{"argv": ["never"]}
		],
		"result": {"text": "{step.first.text}/{step.optional.detail}/{step.meta.text}"}
	}`, nil)
	fake := terminaltest.New(t,
		reply("id=abc\n", "first"),
		terminaltest.Reply{Args: []string{"optional"}, Code: 1, Stderr: "boom\n"},
		terminaltest.Reply{Args: []string{"meta"}, Code: 4},
		reply("legacy-value", "legacy"),
	)
	text, err := backend.ReadOutput(ctx, fakeConn(fake), "p1")
	if err != nil || text != "abc/boom/legacy-value" {
		t.Fatalf("text=%q err=%v", text, err)
	}
	wantCalls(t, fake, []string{"first"}, []string{"optional", "abc"}, []string{"after-failure"}, []string{"meta"}, []string{"legacy"})

	// meta_missing only absorbs a failure its rules classify.
	fake.SetReplies(t, reply("id=abc", "first"), terminaltest.Reply{Args: []string{"meta"}, Code: 9, Stderr: "denied"})
	_, err = backend.ReadOutput(ctx, fakeConn(fake), "p1")
	var commandErr *CommandError
	if !errors.As(err, &commandErr) || commandErr.Error() != "denied" || commandErr.Kind != KindExit || commandErr.Code != 9 {
		t.Fatalf("err=%v", err)
	}
}

func TestDeclarativePoll(t *testing.T) {
	resetLanguage(t)
	backend := fixtureBackend(t, noEnv)
	fake := terminaltest.New(t,
		terminaltest.Reply{Args: []string{"read"}, Stdout: "starting", Times: 2},
		reply("READY now", "read"),
	)
	if err := backend.WaitOutput(context.Background(), fakeConn(fake), "p1", "READY", 30000); err != nil {
		t.Fatal(err)
	}
	if calls := fake.Calls(t); len(calls) != 3 {
		t.Fatalf("polls=%d", len(calls))
	}
	fake.SetReplies(t, reply("still starting", "read"))
	start := time.Now()
	err := backend.WaitOutput(context.Background(), fakeConn(fake), "p1", "READY", 60)
	if err == nil || !strings.Contains(err.Error(), "timed out waiting until matched") || time.Since(start) > 5*time.Second {
		t.Fatalf("err=%v", err)
	}
	fake.SetReplies(t, terminaltest.Reply{Args: []string{"ready"}, Stdout: "", Times: 1}, reply("{}", "ready"))
	if err := backend.WaitReady(Conn{Program: fake.Program, Run: SpawnRunner}, "p1"); err != nil {
		t.Fatalf("nonempty poll: %v", err)
	}
	json := declarativeOp(t, OpWaitReady, `{"steps": [{"argv": ["state"], "poll": {"interval": "5ms", "timeout": "30s", "until": "json_field:state=ready"}}]}`, nil)
	fake.SetReplies(t, terminaltest.Reply{Args: []string{"state"}, Stdout: `{"state":"booting"}`, Times: 1}, reply(`{"state":"ready"}`, "state"))
	if err := json.WaitReady(Conn{Program: fake.Program, Run: SpawnRunner}, "p1"); err != nil {
		t.Fatalf("json_field poll: %v", err)
	}
}

func TestDeclarativeCandidates(t *testing.T) {
	resetLanguage(t)
	backend := declarativeOp(t, OpPrepare, `{
		"candidates": {
			"values": ["main", "main-2", "main-3"],
			"steps": [
				{"store": "exists", "argv": ["has", "{candidate}"], "on_error": "continue"},
				{"store": "owner", "when": "field:step.exists.ok=true", "argv": ["owner", "{candidate}"]}
			],
			"select": [
				{"when": "field:step.exists.ok=false", "result": {"session": "{candidate}", "session_exists": "false"}},
				{"when": "field:step.owner.text={project}", "result": {"session": "{candidate}", "session_exists": "true"}}
			],
			"fallback": [{"fail": "no session for {project}"}]
		}
	}`, nil)
	request := func(fake *terminaltest.Fake) PrepareRequest {
		return PrepareRequest{Project: "/p", LookPath: func(string) (string, error) { return fake.Program, nil }, Getenv: func(name string) string {
			if name == "FAKETERM" {
				return "1"
			}
			return ""
		}}
	}
	fake := terminaltest.New(t, reply("", "has"), reply("/other", "owner", "main"), reply("/p", "owner", "main-2"))
	target, err := backend.Prepare(request(fake))
	if err != nil || target.Session != "main-2" || !target.SessionExists {
		t.Fatalf("hit on second candidate: %+v err=%v", target, err)
	}
	wantCalls(t, fake, []string{"has", "main"}, []string{"owner", "main"}, []string{"has", "main-2"}, []string{"owner", "main-2"})

	fake.SetReplies(t, reply("", "has", "main"), terminaltest.Reply{Args: []string{"has"}, Code: 1}, reply("/other", "owner"))
	target, err = backend.Prepare(request(fake))
	if err != nil || target.Session != "main-2" || target.SessionExists {
		t.Fatalf("fresh candidate: %+v err=%v", target, err)
	}
	fake.Calls(t)
	fake.SetReplies(t, reply("", "has"), reply("/other", "owner"))
	if _, err := backend.Prepare(request(fake)); err == nil || err.Error() != "no session for /p" {
		t.Fatalf("fallback: %v", err)
	}
	if calls := fake.Calls(t); len(calls) != 6 {
		t.Fatalf("calls=%q", calls)
	}
}

func TestDeclarativeRequiresRejectBeforeCommands(t *testing.T) {
	resetLanguage(t)
	backend := fixtureBackend(t, noEnv)
	fake := terminaltest.New(t)
	request := PrepareRequest{Project: "/p", LookPath: func(string) (string, error) { return fake.Program, nil }, Getenv: noEnv}
	if _, err := backend.Prepare(request); err == nil || err.Error() != "launcher faketerm requires the environment variable FAKETERM" {
		t.Fatalf("env: %v", err)
	}
	request.LookPath = func(string) (string, error) { return "", errors.New("missing") }
	if _, err := backend.Prepare(request); err == nil || err.Error() != "faketerm is not on PATH; launcher faketerm cannot start" {
		t.Fatalf("binary: %v", err)
	}
	posix := declarativeOp(t, OpReadOutput, `{"steps": [{"argv": ["read"]}]}`, func(root map[string]any) {
		object(root, "launchers", "faketerm", "requires")["platform"] = "posix"
	})
	request.Windows = true
	if _, err := posix.Prepare(request); err == nil || err.Error() != "launcher faketerm is not available on native Windows" {
		t.Fatalf("platform: %v", err)
	}
	if !posix.Capabilities().POSIXOnly {
		t.Fatal("posix platform must mark POSIXOnly")
	}
	if calls := fake.Calls(t); len(calls) != 0 {
		t.Fatalf("commands ran before the preconditions held: %q", calls)
	}
	env := func(name string) string {
		if name == "FAKETERM" {
			return "1"
		}
		return ""
	}
	if !backend.AutoDetect(env) || backend.AutoDetect(noEnv) {
		t.Fatal("auto detection follows requires.env")
	}
}

func TestDeclarativeErrorClassification(t *testing.T) {
	resetLanguage(t)
	ctx := context.Background()
	backend := fixtureBackend(t, noEnv)
	// The gone detail is the step detail: trimmed stderr, or the exit status.
	for name, tc := range map[string]struct {
		reply  terminaltest.Reply
		detail string
	}{
		"exit code":   {terminaltest.Reply{Args: []string{"facts"}, Code: 3, Stderr: "vanished"}, "vanished"},
		"stderr":      {terminaltest.Reply{Args: []string{"facts"}, Code: 1, Stderr: "No such pane: p1\n"}, "No such pane: p1"},
		"stdout json": {terminaltest.Reply{Args: []string{"facts"}, Code: 1, Stdout: `{"error":{"code":"pane_not_found"}}`}, "exit 1"},
	} {
		t.Run(name, func(t *testing.T) {
			fake := terminaltest.New(t, tc.reply)
			facts, err := backend.PaneFacts(ctx, fakeConn(fake), "p1")
			if err != nil || !facts.Gone || facts.GoneDetail != tc.detail {
				t.Fatalf("facts=%+v err=%v", facts, err)
			}
			exists, err := backend.ContainerExists(ctx, fakeConn(fake), Address{Container: "w1"})
			if err != nil || !exists {
				t.Fatalf("an unrelated command must not borrow the gone fact: exists=%v err=%v", exists, err)
			}
		})
	}
	fake := terminaltest.New(t, terminaltest.Reply{Args: []string{"exists"}, Code: 3})
	if exists, err := backend.ContainerExists(ctx, fakeConn(fake), Address{Container: "w1"}); err != nil || exists {
		t.Fatalf("container gone: exists=%v err=%v", exists, err)
	}
	fake.SetReplies(t, terminaltest.Reply{Args: []string{"topology"}, Code: 3, Stderr: "no such pane"})
	if _, err := backend.Topology(ctx, fakeConn(fake), Address{Pane: "p1"}); err == nil {
		t.Fatal("gone classification applies only to pane facts and container existence")
	}
	fake.SetReplies(t, terminaltest.Reply{Args: []string{"facts"}, Code: 1, Stderr: "permission denied"})
	if _, err := backend.PaneFacts(ctx, fakeConn(fake), "p1"); err == nil || err.Error() != "permission denied" {
		t.Fatalf("ordinary failure: %v", err)
	}

	timed := declarativeOp(t, OpReadOutput, `{"steps": [{"store": "out", "argv": ["read"], "timeout": "50ms"}], "result": {"text": "{step.out.text}"}}`, nil)
	fake.SetReplies(t, terminaltest.Reply{Args: []string{"read"}, Sleep: 5 * time.Second})
	_, err := timed.ReadOutput(ctx, fakeConn(fake), "p1")
	var commandErr *CommandError
	if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &commandErr) || commandErr.Kind != KindExec {
		t.Fatalf("timeout: %v", err)
	}
}

func TestDeclarativeReverseLookupRows(t *testing.T) {
	resetLanguage(t)
	backend := fixtureBackend(t, noEnv)
	identity := Identity{Reference: "s1", ProcessName: func() (string, error) { return "codex", nil }}
	for name, tc := range map[string]struct {
		stdout  string
		matches int
		invalid bool
	}{
		"none":      {stdout: "w1 p1 s2 codex\n", matches: 0},
		"ambiguous": {stdout: "w1 p1 s1 codex\nw1 p2 s1 codex\n", matches: 2},
		"invalid":   {stdout: "garbage\n", invalid: true},
	} {
		t.Run(name, func(t *testing.T) {
			fake := terminaltest.New(t, reply(tc.stdout, "list"))
			_, err := backend.ReverseLookup(context.Background(), fakeConn(fake), identity)
			var matchErr *MatchError
			if tc.invalid {
				if err == nil || errors.As(err, &matchErr) || err.Error() != "lookup returned invalid output" {
					t.Fatalf("err=%v", err)
				}
				return
			}
			if !errors.As(err, &matchErr) || matchErr.Matches != tc.matches {
				t.Fatalf("err=%v", err)
			}
		})
	}
	fake := terminaltest.New(t, terminaltest.Reply{Args: []string{"list"}, Code: 1, Stderr: "server down"})
	failing := Identity{Reference: "s1", ProcessName: func() (string, error) { return "", errors.New("definition error") }}
	if _, err := backend.ReverseLookup(context.Background(), fakeConn(fake), failing); err == nil || err.Error() != "server down" {
		t.Fatalf("collection failure must precede the process name: %v", err)
	}
}

func TestDeclarativeHooks(t *testing.T) {
	resetLanguage(t)
	var status HookStatus
	var seen HookCall
	RegisterHook("test-report-hook", func(_ context.Context, call HookCall) HookResult {
		seen = call
		return HookResult{Status: status, Note: "socket unset", Err: errors.New("socket refused")}
	})
	RegisterHook("test-focus-hook", func(_ context.Context, call HookCall) HookResult {
		return HookResult{Status: status, Note: "pane focus skipped", Err: errors.New("pane focus refused")}
	})
	data := mutateFixture(t, func(root map[string]any) {
		object(root, "capabilities")["session_report"] = true
		root["hooks"] = map[string]any{"report_session": "test-report-hook", "focus_pane": "test-focus-hook"}
	})
	def, err := DecodeDefinition("faketerm.json", data)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := NewDeclarativeBackend(def, "faketerm", noEnv)
	if err != nil {
		t.Fatal(err)
	}
	fake := terminaltest.New(t, reply(`{"command":"codex","dead":"0","mode":"0"}`, "facts"), reply("s1", "meta"))
	report := SessionReport{Pane: "p1", Agent: "claude", Reference: "s1"}

	status = HookOK
	if err := backend.ReportSession(report); err != nil || seen.Values["reference"] != "s1" || seen.Point != HookPointReportSession {
		t.Fatalf("ok: err=%v call=%+v", err, seen)
	}
	if result := backend.Focus(context.Background(), fakeConn(fake), Address{Container: "w1", Pane: "p1"}); !result.Success || result.ID != "focus.success" {
		t.Fatalf("focus ok=%+v", result)
	}
	status = HookDegraded
	if err := backend.ReportSession(report); !errors.Is(err, ErrNoReportChannel) || !strings.Contains(err.Error(), "socket unset") {
		t.Fatalf("degraded: %v", err)
	}
	if result := backend.Focus(context.Background(), fakeConn(fake), Address{Container: "w1", Pane: "p1"}); !result.Success || result.ID != "focus.tab_only" || result.Args[0] != "pane focus skipped" {
		t.Fatalf("focus degraded=%+v", result)
	}
	status = HookFailed
	if err := backend.ReportSession(report); err == nil || err.Error() != "socket refused" {
		t.Fatalf("failed: %v", err)
	}
	if result := backend.Focus(context.Background(), fakeConn(fake), Address{Container: "w1", Pane: "p1"}); result.Success || result.ID != "focus.switch_failed" {
		t.Fatalf("focus failed=%+v", result)
	}
	status = HookStatus(42)
	if err := backend.ReportSession(report); err == nil || !strings.Contains(err.Error(), "unknown status") {
		t.Fatalf("unknown status: %v", err)
	}
}

func TestDeclarativeFocusClosedAndUnavailable(t *testing.T) {
	resetLanguage(t)
	backend := declarativeOp(t, OpFocus, `{
		"unavailable": [{"when": "field_missing:env.FAKETERM", "message": "outside {pane}"}],
		"closed_when": "field:facts.dead=1",
		"steps": [{"argv": ["focus", "{pane}"]}]
	}`, nil)
	result := backend.Focus(context.Background(), Conn{}, Address{Pane: "p1"})
	if result.Success || result.ID != "terminal.text" || result.Args[0] != "outside p1" {
		t.Fatalf("unavailable=%+v", result)
	}
	withEnv, err := NewDeclarativeBackend(backend.def, "faketerm", func(string) string { return "1" })
	if err != nil {
		t.Fatal(err)
	}
	fake := terminaltest.New(t, reply(`{"command":"codex","dead":"1","mode":"0"}`, "facts"), reply("s1", "meta"))
	if result := withEnv.Focus(context.Background(), fakeConn(fake), Address{Pane: "p1"}); result.ID != "focus.closed" {
		t.Fatalf("closed=%+v", result)
	}
	fake.SetReplies(t, reply(`{"command":"codex","dead":"0","mode":"0"}`, "facts"), reply("s1", "meta"), terminaltest.Reply{Args: []string{"focus"}, Code: 1, Stderr: "no client"})
	if result := withEnv.Focus(context.Background(), fakeConn(fake), Address{Pane: "p1"}); result.ID != "focus.switch_failed" || result.Args[0] != "focus: no client" {
		t.Fatalf("switch failed=%+v", result)
	}
}
