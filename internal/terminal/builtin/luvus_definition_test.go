package builtin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

func luvusBackendForTest(t *testing.T) terminal.Backend {
	t.Helper()
	backend, err := DefinitionBackend("luvus", Luvus, envOf(map[string]string{"LUVUS_ENV": "1", "LUVUS_PANE_ID": "3"}))
	if err != nil {
		t.Fatal(err)
	}
	return backend
}

// luvusReply answers the luvus CLI for a split that opens pane 7, whose
// agent get reports the working directory cwd.
func luvusReply(cwd string) func(args []string) probe.Result {
	return func(args []string) probe.Result {
		switch strings.Join(args[:2], " ") {
		case "pane split":
			return probe.Result{Stdout: `{"id":"1","result":{"type":"pane","pane":"7","workspace":"0","tab":"1"}}`}
		case "agent get":
			return probe.Result{Stdout: `{"id":"1","result":{"type":"agent","pane":"7","agent":"zsh","status":"idle","session":null,"cwd":"` + cwd + `"}}`}
		}
		return probe.Result{Stdout: `{"id":"1","result":{}}`}
	}
}

func luvusCreateCalls(cwd string) [][]string {
	return [][]string{
		{"pane", "split", "3", "--no-focus", "--cwd", cwd},
		{"agent", "get", "7"},
		{"pane", "move", "7", "--new-tab"},
		{"pane", "name", "--pane", "7", "task name"},
		{"pane", "focus", "3"},
	}
}

func TestLuvusDefinitionArgvAndParsing(t *testing.T) {
	backend := luvusBackendForTest(t)
	ctx := context.Background()
	address := terminal.Address{Container: "7", Pane: "7"}
	cases := []struct {
		name  string
		run   func(terminal.Conn) error
		reply string
		want  [][]string
	}{
		{"ready", func(c terminal.Conn) error { return backend.WaitReady(c, "7") },
			`{"id":"1","result":{"type":"pane_read","pane":"7","text":"\n$ "}}`, [][]string{{"pane", "read", "7"}}},
		{"run", func(c terminal.Conn) error { return backend.RunCommand(c, "7", "agent 'task file'", true) },
			`{"id":"1","result":{}}`, [][]string{{"pane", "run", "7", "agent 'task file'"}}},
		{"facts", func(c terminal.Conn) error {
			got, e := backend.PaneFacts(ctx, c, "7")
			if e == nil && got != (terminal.PaneFacts{Agent: "codex", AgentStatus: "blocked", AgentSession: "s1", Container: "7"}) {
				t.Fatalf("facts=%+v", got)
			}
			return e
		}, `{"id":"1","result":{"type":"agent","pane":"7","agent":"codex","status":"blocked","session":"s1","cwd":"/p"}}`, [][]string{{"agent", "get", "7"}}},
		{"read", func(c terminal.Conn) error {
			got, e := backend.ReadOutput(ctx, c, "7")
			if e == nil && got != "line one\nline two" {
				t.Fatalf("output=%q", got)
			}
			return e
		}, `{"id":"1","result":{"type":"pane_read","pane":"7","text":"line one\nline two"}}`, [][]string{{"pane", "read", "7"}}},
		{"deliver", func(c terminal.Conn) error {
			return backend.DeliverText(ctx, c, "7", "--flag # kander-notify: /tmp/task file")
		}, `{"id":"1","result":{}}`, [][]string{{"agent", "prompt", "7", "--", "--flag # kander-notify: /tmp/task file"}}},
		{"topology", func(c terminal.Conn) error {
			got, e := backend.Topology(ctx, c, address)
			if e == nil && (got.Container != "7" || len(got.Panes) != 1 || got.Panes[0] != "7") {
				t.Fatalf("topology=%+v", got)
			}
			return e
		}, `{"id":"1","result":{"type":"pane_list","panes":[{"pane":"3"},{"pane":"7"}]}}`, [][]string{{"pane", "list"}}},
		{"reverse lookup", func(c terminal.Conn) error {
			got, e := backend.ReverseLookup(ctx, c, terminal.Identity{Agent: "codex", Reference: "s1"})
			if e == nil && got != address {
				t.Fatalf("address=%+v", got)
			}
			return e
		}, `{"id":"1","result":{"type":"agent_list","agents":[{"pane":"3","agent":"claude","session":"s1"},{"pane":"5","agent":"codex","session":"s2"},{"pane":"7","agent":"codex","session":"s1"}]}}`, [][]string{{"agent", "list"}}},
		{"close", func(c terminal.Conn) error { return backend.CloseContainer(ctx, c, address) },
			`{"id":"1","result":{}}`, [][]string{{"pane", "close", "7"}}},
		{"exists", func(c terminal.Conn) error {
			got, e := backend.ContainerExists(ctx, c, address)
			if e == nil && !got {
				t.Fatal("live pane reported missing")
			}
			return e
		}, `{"id":"1","result":{"type":"pane_status","pane":"7"}}`, [][]string{{"pane", "status", "7"}}},
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

func TestLuvusDefinitionCreateVerifiesCwd(t *testing.T) {
	backend := luvusBackendForTest(t)
	cwd := t.TempDir()
	r := &recorder{reply: luvusReply(cwd)}
	got, err := backend.CreateContainer(r.conn(), terminal.Target{}, cwd, "task name")
	if err != nil {
		t.Fatal(err)
	}
	if got != (terminal.Address{Container: "7", Pane: "7"}) {
		t.Fatalf("address=%+v", got)
	}
	assertCalls(t, r.calls, luvusCreateCalls(cwd))
	if opaque := backend.OpaqueAddress(got); opaque != "7:7" {
		t.Fatalf("address=%q", opaque)
	}
	if parsed, address, ok := terminal.ParseWindow("luvus:7:7"); !ok || parsed.Name() != Luvus || address != got {
		t.Fatalf("WINDOW luvus:7:7 parsed as %+v ok=%v", address, ok)
	}
}

// luvus may report the directory with symbolic links resolved or without the
// trailing slash of the requested path; neither is a mismatch.
func TestLuvusDefinitionCreateAcceptsEquivalentCwd(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	canonical, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, requested, reported string }{
		{"symbolic link", link, canonical},
		{"trailing slash", canonical + string(filepath.Separator), canonical},
		{"literal", link, link},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &recorder{reply: luvusReply(tc.reported)}
			if _, err := luvusBackendForTest(t).CreateContainer(r.conn(), terminal.Target{}, tc.requested, "task name"); err != nil {
				t.Fatal(err)
			}
			assertCalls(t, r.calls, luvusCreateCalls(tc.requested))
		})
	}
}

// A pane whose directory is not reported yet is read again until it is.
func TestLuvusDefinitionCreatePollsForCwd(t *testing.T) {
	cwd := t.TempDir()
	reads := 0
	reply := luvusReply(cwd)
	r := &recorder{reply: func(args []string) probe.Result {
		if args[0] == "agent" && args[1] == "get" {
			reads++
			if reads < 3 {
				return probe.Result{Stdout: `{"id":"1","result":{"type":"agent","pane":"7","cwd":""}}`}
			}
		}
		return reply(args)
	}}
	if _, err := luvusBackendForTest(t).CreateContainer(r.conn(), terminal.Target{}, cwd, "task name"); err != nil {
		t.Fatal(err)
	}
	if reads != 3 {
		t.Fatalf("agent get ran %d times", reads)
	}
}

// An older luvus CLI ignores --cwd, and an older server ignores the cwd a
// newer CLI sends: both open the pane in the anchor's directory. The
// definition closes that pane and fails with the upgrade hint before the
// pane is moved, named or handed a command.
func TestLuvusDefinitionCreateRejectsIgnoredCwd(t *testing.T) {
	config.ApplyLanguageArgument([]string{"kander", "--lang", "en"})
	cwd := t.TempDir()
	anchor := t.TempDir()
	for _, closeFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "closed", true: "close failed"}[closeFails], func(t *testing.T) {
			reply := luvusReply(anchor)
			r := &recorder{reply: func(args []string) probe.Result {
				if closeFails && args[0] == "pane" && args[1] == "close" {
					return probe.Result{Code: 1, Stdout: `{"id":"1","error":{"code":"internal","message":"cannot close"}}`}
				}
				return reply(args)
			}}
			_, err := luvusBackendForTest(t).CreateContainer(r.conn(), terminal.Target{}, cwd, "task name")
			if err == nil {
				t.Fatal("a pane in the wrong directory was accepted")
			}
			assertCalls(t, r.calls, [][]string{
				{"pane", "split", "3", "--no-focus", "--cwd", cwd},
				{"agent", "get", "7"},
				{"pane", "close", "7"},
			})
			want := config.Text("luvus.cwd_mismatch", "7", anchor, cwd)
			if closeFails {
				want = "cannot close"
			}
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error=%v; want %q", err, want)
			}
			if !closeFails && !strings.Contains(err.Error(), "--cwd") {
				t.Fatalf("error lacks the upgrade hint: %v", err)
			}
		})
	}
}

func TestLuvusDefinitionCreateClosesPaneWithUnreadableCwd(t *testing.T) {
	config.ApplyLanguageArgument([]string{"kander", "--lang", "en"})
	cwd := t.TempDir()
	reply := luvusReply(cwd)
	r := &recorder{reply: func(args []string) probe.Result {
		if args[0] == "agent" && args[1] == "get" {
			return probe.Result{Code: 1, Stderr: "server gone"}
		}
		return reply(args)
	}}
	_, err := luvusBackendForTest(t).CreateContainer(r.conn(), terminal.Target{}, cwd, "task name")
	if err == nil || !strings.Contains(err.Error(), config.Text("luvus.cwd_unreadable", "7", cwd, "server gone")) {
		t.Fatalf("error=%v", err)
	}
	assertCalls(t, r.calls, [][]string{
		{"pane", "split", "3", "--no-focus", "--cwd", cwd},
		{"agent", "get", "7"},
		{"pane", "close", "7"},
	})
}

func TestLuvusDefinitionCreateRejectsSplitWithoutPane(t *testing.T) {
	r := &recorder{reply: func([]string) probe.Result { return probe.Result{Stdout: `{"id":"1","result":{"type":"pane"}}`} }}
	if _, err := luvusBackendForTest(t).CreateContainer(r.conn(), terminal.Target{}, t.TempDir(), "task"); err == nil {
		t.Fatal("split without a pane id accepted")
	}
	if len(r.calls) != 1 {
		t.Fatalf("calls=%q", r.calls)
	}
}

func TestLuvusDefinitionNotFoundIsGone(t *testing.T) {
	backend := luvusBackendForTest(t)
	address := terminal.Address{Container: "7", Pane: "7"}
	notFound := probe.Result{Code: 1, Stdout: `{"id":"1","error":{"code":"not_found","message":"no pane 7"}}`}
	r := &recorder{reply: func([]string) probe.Result { return notFound }}
	facts, err := backend.PaneFacts(context.Background(), r.conn(), "7")
	if err != nil || !facts.Gone {
		t.Fatalf("facts=%+v error=%v", facts, err)
	}
	exists, err := backend.ContainerExists(context.Background(), r.conn(), address)
	if err != nil || exists {
		t.Fatalf("exists=%v error=%v", exists, err)
	}
	other := probe.Result{Code: 1, Stdout: `{"id":"1","error":{"code":"internal","message":"boom"}}`}
	r = &recorder{reply: func([]string) probe.Result { return other }}
	if _, err := backend.ContainerExists(context.Background(), r.conn(), address); err == nil {
		t.Fatal("an ordinary failure was reported as gone")
	}
	r = &recorder{reply: func([]string) probe.Result {
		return probe.Result{Stdout: `{"id":"1","result":{"type":"agent","pane":"8"}}`}
	}}
	if _, err := backend.PaneFacts(context.Background(), r.conn(), "7"); err == nil {
		t.Fatal("facts of another pane accepted")
	}
}

func TestLuvusDefinitionReverseLookupOutcomes(t *testing.T) {
	backend := luvusBackendForTest(t)
	lookup := func(stdout string) error {
		r := &recorder{reply: func([]string) probe.Result { return probe.Result{Stdout: stdout} }}
		_, err := backend.ReverseLookup(context.Background(), r.conn(), terminal.Identity{Agent: "codex", Reference: "s1"})
		return err
	}
	var match *terminal.MatchError
	if err := lookup(`{"result":{"agents":[{"pane":"7","agent":"codex","session":"s2"}]}}`); !errors.As(err, &match) || match.Matches != 0 {
		t.Fatalf("no match: %v", err)
	}
	if err := lookup(`{"result":{"agents":[{"pane":"5","agent":"codex","session":"s1"},{"pane":"7","agent":"codex","session":"s1"}]}}`); !errors.As(err, &match) || match.Matches != 2 {
		t.Fatalf("ambiguous: %v", err)
	}
	// An agent pane without a reported session cannot be decided.
	var incomplete *terminal.IncompleteLookupError
	if err := lookup(`{"result":{"agents":[{"pane":"7","agent":"codex","session":null}]}}`); !errors.As(err, &incomplete) {
		t.Fatalf("unbound session: %v", err)
	}
	if err := lookup(`{"result":{"agents":[{"agent":"codex","session":"s1"}]}}`); err == nil {
		t.Fatal("a row without a pane id was accepted")
	}
}

func TestLuvusDefinitionFocus(t *testing.T) {
	backend := luvusBackendForTest(t)
	r := &recorder{reply: func(args []string) probe.Result {
		if args[0] == "agent" {
			return probe.Result{Stdout: `{"id":"1","result":{"type":"agent","pane":"7","agent":"codex","status":"idle"}}`}
		}
		return probe.Result{Stdout: `{"id":"1","result":{}}`}
	}}
	result := backend.Focus(context.Background(), r.conn(), terminal.Address{Container: "7", Pane: "7"})
	if !result.Success {
		t.Fatalf("focus=%+v", result)
	}
	assertCalls(t, r.calls, [][]string{{"agent", "get", "7"}, {"pane", "focus", "7"}})

	outside, err := DefinitionBackend("luvus", Luvus, envOf(nil))
	if err != nil {
		t.Fatal(err)
	}
	r = &recorder{}
	if result := outside.Focus(context.Background(), r.conn(), terminal.Address{Container: "7", Pane: "7"}); result.Success {
		t.Fatalf("focus outside luvus=%+v", result)
	}
	if len(r.calls) != 0 {
		t.Fatalf("calls=%q", r.calls)
	}
}

func TestLuvusAutoPriority(t *testing.T) {
	// Only the embedded definitions take part, never a user's share directory.
	saved := terminal.DefinitionDirs
	terminal.DefinitionDirs = func() []terminal.DefinitionDir { return nil }
	terminal.ReloadDefinitions()
	t.Cleanup(func() {
		terminal.DefinitionDirs = saved
		terminal.ReloadDefinitions()
	})
	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{"luvus", map[string]string{"LUVUS_ENV": "1"}, Luvus},
		{"herdr wins", map[string]string{"LUVUS_ENV": "1", "HERDR_ENV": "1"}, Herdr},
		{"luvus over tmux", map[string]string{"LUVUS_ENV": "1", "TMUX": "/tmp/tmux-1000/default,1,0"}, Luvus},
		{"tmux only", map[string]string{"TMUX": "/tmp/tmux-1000/default,1,0"}, Tmux},
		{"luvus env not 1", map[string]string{"LUVUS_ENV": "0", "TMUX": "/tmp/tmux-1000/default,1,0"}, Tmux},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend, ok := terminal.ResolveAuto(false, envOf(tc.env))
			if !ok || backend.Name() != tc.want {
				name := ""
				if ok {
					name = backend.Name()
				}
				t.Fatalf("auto=%q ok=%v; want %q", name, ok, tc.want)
			}
		})
	}
	if backend, ok := terminal.ResolveAuto(true, envOf(map[string]string{"LUVUS_ENV": "1"})); ok {
		t.Fatalf("native Windows resolved auto to %q", backend.Name())
	}
}
