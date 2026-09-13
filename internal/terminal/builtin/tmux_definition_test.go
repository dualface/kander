package builtin

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
	"github.com/dualface/kander/internal/terminal/terminaltest"
)

// recorder is a fake tmux: it records every argv and answers by a reply
// function, so a test pins the exact command lines the definition produces.
type recorder struct {
	calls [][]string
	reply func(args []string) probe.Result
}

func (r *recorder) conn() terminal.Conn {
	return terminal.Conn{Program: "tmux", Run: func(_ context.Context, program string, args []string) (probe.Result, error) {
		r.calls = append(r.calls, append([]string{}, args...))
		if r.reply == nil {
			return probe.Result{}, nil
		}
		return r.reply(args), nil
	}}
}

func envOf(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func assertCalls(t *testing.T, got, want [][]string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv mismatch\n got=%q\nwant=%q", got, want)
	}
}

// The argv below is copied from the Go tmux backend this definition replaced
// (internal/terminal/tmux at 39247fa); TAB and #{...} must match byte for byte.
func TestTmuxDefinitionArgvMatchesGoBackend(t *testing.T) {
	resetLang(t)
	ctx := context.Background()
	address := terminal.Address{Session: "$1", Container: "@1", Pane: "%1"}
	backend := tmuxBackend(t, Tmux, envOf(map[string]string{"TMUX": "/tmp/tmux,1,0"}))
	cases := []struct {
		name  string
		reply func(args []string) probe.Result
		run   func(terminal.Conn) error
		want  [][]string
	}{
		{
			name:  "create_container",
			reply: func([]string) probe.Result { return probe.Result{Stdout: "@9\t%9\n"} },
			run: func(conn terminal.Conn) error {
				_, err := backend.CreateContainer(conn, terminal.Target{Session: "$42"}, "/work dir", "task label")
				return err
			},
			want: [][]string{{"new-window", "-d", "-P", "-F", "#{window_id}\t#{pane_id}", "-t", "$42:", "-c", "/work dir", "-n", "task label"}},
		},
		{
			name: "run_command posix",
			run:  func(conn terminal.Conn) error { return backend.RunCommand(conn, "%9", "codex 'a b'", true) },
			want: [][]string{{"respawn-pane", "-k", "-t", "%9", "exec codex 'a b'"}},
		},
		{
			name: "run_command without exec",
			run:  func(conn terminal.Conn) error { return backend.RunCommand(conn, "%9", "codex", false) },
			want: [][]string{{"respawn-pane", "-k", "-t", "%9", "codex"}},
		},
		{
			name: "set_session_marker",
			run:  func(conn terminal.Conn) error { return backend.SetSessionMarker(conn, "%9", "session-1") },
			want: [][]string{{"set-option", "-p", "-t", "%9", "@kander_session", "session-1"}},
		},
		{
			// An empty value cannot be an argv element of a definition; the
			// marker is unset instead, which every reader treats as empty.
			name: "set_session_marker without a reference",
			run:  func(conn terminal.Conn) error { return backend.SetSessionMarker(conn, "%9", "") },
			want: [][]string{{"set-option", "-p", "-u", "-t", "%9", "@kander_session"}},
		},
		{
			name: "pane_facts",
			reply: func(args []string) probe.Result {
				if args[0] == "display-message" {
					return probe.Result{Stdout: "codex\t0\t0\n"}
				}
				return probe.Result{Code: 1, Stderr: "invalid option: " + args[len(args)-1]}
			},
			run: func(conn terminal.Conn) error { _, err := backend.PaneFacts(ctx, conn, "%9"); return err },
			want: [][]string{
				{"display-message", "-p", "-t", "%9", "#{pane_current_command}\t#{pane_in_mode}\t#{pane_dead}"},
				{"show-options", "-p", "-v", "-t", "%9", "@kander_session"},
				{"show-options", "-p", "-v", "-t", "%9", "@onevoke_session"},
			},
		},
		{
			name: "read_output",
			run:  func(conn terminal.Conn) error { _, err := backend.ReadOutput(ctx, conn, "%9"); return err },
			want: [][]string{{"capture-pane", "-p", "-t", "%9"}},
		},
		{
			name: "deliver_text",
			run: func(conn terminal.Conn) error {
				return backend.DeliverText(ctx, conn, "%9", "# kander-notify: /tmp/x {marker}")
			},
			want: [][]string{{"send-keys", "-t", "%9", "-l", "# kander-notify: /tmp/x {marker}"}, {"send-keys", "-t", "%9", "Enter"}},
		},
		{
			name:  "topology",
			reply: func([]string) probe.Result { return probe.Result{Stdout: "$1\tname\t@1\t1\n"} },
			run:   func(conn terminal.Conn) error { _, err := backend.Topology(ctx, conn, address); return err },
			want:  [][]string{{"display-message", "-p", "-t", "%1", "#{session_id}\t#{session_name}\t#{window_id}\t#{window_panes}"}},
		},
		{
			name:  "container_exists",
			reply: func([]string) probe.Result { return probe.Result{Stdout: "@1\n"} },
			run:   func(conn terminal.Conn) error { _, err := backend.ContainerExists(ctx, conn, address); return err },
			want:  [][]string{{"display-message", "-p", "-t", "@1", "#{window_id}"}},
		},
		{
			name:  "reverse_lookup",
			reply: func([]string) probe.Result { return probe.Result{Stdout: "%1\t$1\tname\t@1\tcodex\t0\tref\t\n"} },
			run: func(conn terminal.Conn) error {
				_, err := backend.ReverseLookup(ctx, conn, terminal.Identity{Reference: "ref", ProcessName: func() (string, error) { return "codex", nil }})
				return err
			},
			want: [][]string{{"list-panes", "-a", "-F", "#{pane_id}\t#{session_id}\t#{session_name}\t#{window_id}\t#{pane_current_command}\t#{pane_dead}\t#{@kander_session}\t#{@onevoke_session}"}},
		},
		{
			name: "focus",
			reply: func(args []string) probe.Result {
				if args[0] == "display-message" {
					return probe.Result{Stdout: "codex\t0\t0\n"}
				}
				return probe.Result{Stdout: "session\n"}
			},
			run: func(conn terminal.Conn) error {
				if result := backend.Focus(ctx, conn, address); !result.Success {
					return errors.New(result.ID)
				}
				return nil
			},
			want: [][]string{
				{"display-message", "-p", "-t", "%1", "#{pane_current_command}\t#{pane_in_mode}\t#{pane_dead}"},
				{"show-options", "-p", "-v", "-t", "%1", "@kander_session"},
				{"select-window", "-t", "$1:@1"},
				{"select-pane", "-t", "%1"},
				{"switch-client", "-t", "$1"},
			},
		},
		{
			name: "close_container",
			run:  func(conn terminal.Conn) error { return backend.CloseContainer(ctx, conn, address) },
			want: [][]string{{"kill-window", "-t", "@1"}},
		},
		{
			name: "close_container without window",
			run:  func(conn terminal.Conn) error { return backend.CloseContainer(ctx, conn, terminal.Address{}) },
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{reply: tc.reply}
			if err := tc.run(rec.conn()); err != nil {
				t.Fatal(err)
			}
			assertCalls(t, rec.calls, tc.want)
		})
	}
	if got := backend.VersionArgs(); !reflect.DeepEqual(got, []string{"-V"}) {
		t.Fatalf("version args=%q", got)
	}
}

func TestMain(m *testing.M) {
	terminaltest.Main()
	os.Exit(m.Run())
}

func prepareRequest(fake *terminaltest.Fake, project string, env map[string]string) terminal.PrepareRequest {
	return terminal.PrepareRequest{
		Project:  project,
		LookPath: func(string) (string, error) { return fake.Program, nil },
		Getenv:   envOf(env),
	}
}

func TestTmuxDefinitionPrepareReadsCurrentSession(t *testing.T) {
	resetLang(t)
	backend := tmuxBackend(t, Tmux, envOf(nil))
	inside := map[string]string{"TMUX": "/tmp/tmux,1,0", "TMUX_PANE": "%7"}
	fake := terminaltest.New(t, terminaltest.Reply{Args: []string{"display-message"}, Stdout: "$42\n"})
	target, err := backend.Prepare(prepareRequest(fake, "/p", inside))
	if err != nil || target.Session != "$42" || target.Program != fake.Program {
		t.Fatalf("target=%+v err=%v", target, err)
	}
	assertCalls(t, fake.Calls(t), [][]string{{"display-message", "-p", "-t", "%7", "#{session_id}"}})

	fake.SetReplies(t, terminaltest.Reply{Args: []string{"display-message"}, Stdout: "not-a-session\n"})
	if _, err := backend.Prepare(prepareRequest(fake, "/p", inside)); err == nil || err.Error() != config.Text("launch.failed_to_read_tmux_session", config.Text("launch.cannot_determine_the_current_session")) {
		t.Fatalf("err=%v", err)
	}
	fake.SetReplies(t, terminaltest.Reply{Args: []string{"display-message"}, Code: 1, Stderr: "no server running\n"})
	if _, err := backend.Prepare(prepareRequest(fake, "/p", inside)); err == nil || err.Error() != config.Text("launch.failed_to_read_tmux_session", "no server running") {
		t.Fatalf("err=%v", err)
	}
	if _, err := backend.Prepare(prepareRequest(fake, "/p", map[string]string{"TMUX": "/tmp/tmux,1,0"})); err == nil || err.Error() != config.Text("launch.not_currently_in_a_tmux_session_run", TmuxSessionHint) {
		t.Fatalf("err=%v", err)
	}
	missing := prepareRequest(fake, "/p", inside)
	missing.LookPath = func(string) (string, error) { return "", errors.New("missing") }
	if _, err := backend.Prepare(missing); err == nil || err.Error() != config.Text("launch.tmux_is_not_in_path_run_kander_welcome_to") {
		t.Fatalf("err=%v", err)
	}
}

// Session reuse follows the owner marker: a session of another project moves
// to the next numbered candidate, the project's own session is reused without
// creating a session, and the primary marker hides the legacy one.
func TestTmuxDefinitionProjectSessionCandidates(t *testing.T) {
	resetLang(t)
	project := "/work/demo"
	base := "kb-" + terminal.ProjectKey(project)
	backend := tmuxBackend(t, TmuxSession, envOf(nil))
	fake := terminaltest.New(t,
		terminaltest.Reply{Args: []string{"has-session", "-t", "=" + base}},
		terminaltest.Reply{Args: []string{"has-session", "-t", "=" + base + "-2"}},
		terminaltest.Reply{Args: []string{"has-session"}, Code: 1},
		terminaltest.Reply{Args: []string{"show-options", "-v", "-t", base, "@kander_project"}, Stdout: "/elsewhere\n"},
		terminaltest.Reply{Args: []string{"show-options", "-v", "-t", base + "-2", "@kander_project"}, Stdout: project + "\n"},
	)
	target, err := backend.Prepare(prepareRequest(fake, project, nil))
	if err != nil || target.Session != base+"-2" || !target.SessionExists {
		t.Fatalf("target=%+v err=%v", target, err)
	}
	assertCalls(t, fake.Calls(t), [][]string{
		{"has-session", "-t", "=" + base},
		{"show-options", "-v", "-t", base, "@kander_project"},
		{"has-session", "-t", "=" + base + "-2"},
		{"show-options", "-v", "-t", base + "-2", "@kander_project"},
	})

	rec := &recorder{reply: func(args []string) probe.Result {
		if args[0] == "new-window" {
			return probe.Result{Stdout: "@3\t%3\n"}
		}
		t.Fatalf("unexpected command while reusing a session: %q", args)
		return probe.Result{}
	}}
	address, err := backend.CreateContainer(rec.conn(), target, project, "label")
	if err != nil || address != (terminal.Address{Session: base + "-2", Container: "@3", Pane: "%3"}) {
		t.Fatalf("address=%+v err=%v", address, err)
	}

	fake.SetReplies(t,
		terminaltest.Reply{Args: []string{"has-session"}},
		terminaltest.Reply{Args: []string{"show-options"}, Stdout: "/elsewhere\n"},
	)
	if _, err := backend.Prepare(prepareRequest(fake, project, nil)); err == nil || err.Error() != config.Text("launch.no_project_session_name_is_available_and_its_numbered", base) {
		t.Fatalf("err=%v", err)
	}
	if calls := fake.Calls(t); len(calls) != 18 || calls[17][3] != base+"-9" {
		t.Fatalf("calls=%q", calls)
	}

	fake.SetReplies(t, terminaltest.Reply{Args: []string{"has-session"}, Code: 1})
	target, err = backend.Prepare(prepareRequest(fake, project, nil))
	if err != nil || target.Session != base || target.SessionExists {
		t.Fatalf("new session target=%+v err=%v", target, err)
	}
}

func TestTmuxDefinitionProjectSessionCreateAndRetry(t *testing.T) {
	resetLang(t)
	backend := tmuxBackend(t, TmuxSession, envOf(nil))
	target := terminal.Target{Session: "kb-demo", Project: "/work/demo"}
	rec := &recorder{reply: func(args []string) probe.Result {
		if args[0] == "new-session" {
			return probe.Result{Stdout: "@1\t%1\n"}
		}
		return probe.Result{}
	}}
	address, err := backend.CreateContainer(rec.conn(), target, "/work/demo", "label")
	if err != nil || address.Container != "@1" || address.Pane != "%1" {
		t.Fatalf("address=%+v err=%v", address, err)
	}
	assertCalls(t, rec.calls, [][]string{
		{"new-session", "-d", "-P", "-F", "#{window_id}\t#{pane_id}", "-s", "kb-demo", "-c", "/work/demo", "-n", "label"},
		{"set-option", "-t", "kb-demo", "@kander_project", "/work/demo"},
	})

	// A concurrent start created the session: fall back to a new window once.
	rec = &recorder{reply: func(args []string) probe.Result {
		switch args[0] {
		case "new-session":
			return probe.Result{Code: 1, Stderr: "duplicate session: kb-demo"}
		case "show-options":
			if args[4] == "@kander_project" {
				return probe.Result{Code: 1}
			}
			return probe.Result{Stdout: "/work/demo\n"}
		case "new-window":
			return probe.Result{Stdout: "@2\t%2\n"}
		}
		return probe.Result{}
	}}
	address, err = backend.CreateContainer(rec.conn(), target, "/work/demo", "label")
	if err != nil || address.Container != "@2" {
		t.Fatalf("address=%+v err=%v", address, err)
	}
	assertCalls(t, rec.calls, [][]string{
		{"new-session", "-d", "-P", "-F", "#{window_id}\t#{pane_id}", "-s", "kb-demo", "-c", "/work/demo", "-n", "label"},
		{"has-session", "-t", "=kb-demo"},
		{"show-options", "-v", "-t", "kb-demo", "@kander_project"},
		{"show-options", "-v", "-t", "kb-demo", "@onevoke_project"},
		{"new-window", "-d", "-P", "-F", "#{window_id}\t#{pane_id}", "-t", "kb-demo:", "-c", "/work/demo", "-n", "label"},
	})

	// The session belongs to another project: report the new-session failure.
	rec = &recorder{reply: func(args []string) probe.Result {
		switch args[0] {
		case "new-session":
			return probe.Result{Code: 1, Stderr: "duplicate session: kb-demo"}
		case "show-options":
			return probe.Result{Stdout: "/elsewhere\n"}
		}
		return probe.Result{}
	}}
	_, err = backend.CreateContainer(rec.conn(), target, "/work/demo", "label")
	if err == nil || err.Error() != config.Text("launch.tmux_failed", "new-session", "duplicate session: kb-demo") {
		t.Fatalf("err=%v", err)
	}
}

// An empty answer is gone, while a non-empty answer that does not have the
// expected shape stays an invalid response with its original diagnostic.
func TestTmuxDefinitionEmptyAnswerIsGone(t *testing.T) {
	resetLang(t)
	backend := tmuxBackend(t, Tmux, envOf(nil))
	ctx := context.Background()
	for stdout, gone := range map[string]bool{"\t\t\n": true, "\n": true, "codex\n": false} {
		rec := &recorder{reply: func([]string) probe.Result { return probe.Result{Stdout: stdout} }}
		facts, err := backend.PaneFacts(ctx, rec.conn(), "%9")
		if gone {
			if err != nil || !facts.Gone || facts.GoneDetail != config.Text("terminal.target_answered_empty") || len(rec.calls) != 1 {
				t.Fatalf("stdout %q: facts=%+v err=%v calls=%q", stdout, facts, err, rec.calls)
			}
			continue
		}
		if err == nil || facts.Gone || err.Error() != config.Text("launch.tmux_pane_probe_returned_an_invalid_response") {
			t.Fatalf("stdout %q: facts=%+v err=%v", stdout, facts, err)
		}
	}
	address := terminal.Address{Container: "@9"}
	rec := &recorder{reply: func([]string) probe.Result { return probe.Result{Stdout: "\n"} }}
	if exists, err := backend.ContainerExists(ctx, rec.conn(), address); err != nil || exists {
		t.Fatalf("empty window answer: exists=%v err=%v", exists, err)
	}
	rec = &recorder{reply: func([]string) probe.Result { return probe.Result{Stdout: "@8\n"} }}
	if _, err := backend.ContainerExists(ctx, rec.conn(), address); err == nil || err.Error() != config.Text("takeover.tmux_window_probe_returned_an_invalid_response", "@8") {
		t.Fatalf("other window answer: %v", err)
	}
	rec = &recorder{reply: func([]string) probe.Result { return probe.Result{Code: 1, Stderr: "permission denied"} }}
	if _, err := backend.PaneFacts(ctx, rec.conn(), "%9"); err == nil || err.Error() != config.Text("launch.tmux_pane_does_not_exist", "%9", "permission denied") {
		t.Fatalf("exit failure: %v", err)
	}
}

func TestTmuxDefinitionStartedLinesAndAddress(t *testing.T) {
	resetLang(t)
	target := terminal.Target{Session: "kb-demo"}
	address := terminal.Address{Session: "kb-demo", Container: "@3", Pane: "%3"}
	plain := tmuxBackend(t, Tmux, envOf(nil))
	if got := plain.StartedLines("head", target, address, envOf(nil)); !reflect.DeepEqual(got, []string{config.Text("launch.launcher_tmux_window", "head", "@3")}) {
		t.Fatalf("lines=%q", got)
	}
	session := tmuxBackend(t, TmuxSession, envOf(nil))
	for tmuxEnv, hint := range map[string]string{"": "tmux attach -t kb-demo", "/tmp/tmux,1,0": "tmux switch-client -t kb-demo"} {
		want := []string{config.Text("launch.session_window", "head", "kb-demo", "@3"), config.Text("launch.view", hint)}
		if got := session.StartedLines("head", target, address, envOf(map[string]string{"TMUX": tmuxEnv})); !reflect.DeepEqual(got, want) {
			t.Fatalf("lines=%q want %q", got, want)
		}
	}
	if got := terminal.FormatAddress(session, address); got != "tmux-session:kb-demo:@3:%3" {
		t.Fatalf("window=%q", got)
	}
	for value, ok := range map[string]bool{"tmux-session:kb-demo:@3:%3": true, "tmux:kb-demo:@3:%3": false, "tmux-session:kb demo:@3:%3": false} {
		if _, got := session.ParseAddress(value); got != ok {
			t.Fatalf("ParseAddress(%q)=%v", value, got)
		}
	}
	caps := session.Capabilities()
	if !caps.Container || !caps.Focus || !caps.PaneMetadata || !caps.ForegroundProcess || !caps.POSIXOnly || caps.AgentIdentity || caps.WaitOutput {
		t.Fatalf("capabilities=%+v", caps)
	}
	// Like the removed Go backend, auto looks only at TMUX; Prepare still
	// requires TMUX_PANE and names it.
	if !plain.AutoDetect(envOf(map[string]string{"TMUX": "x"})) || session.AutoDetect(envOf(map[string]string{"TMUX": "x", "TMUX_PANE": "%1"})) {
		t.Fatal("only plain tmux takes part in auto resolution")
	}
}
