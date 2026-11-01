//go:build !windows

package launch

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/terminal"
)

// realTmux is one tmux server on a private socket. Every Kander call goes
// through wrapper, so nothing reaches a server this test did not start.
type realTmux struct {
	t       *testing.T
	binary  string
	socket  string
	wrapper string
	session string
	// before runs in the wrapper ahead of respawn-pane, after the launch
	// validated the startup directory and created the container.
	before string
}

const realTmuxSession = "kander-startup"

// startRealTmux starts the server in serverDir with the given environment.
func startRealTmux(t *testing.T, serverDir string, env ...string) *realTmux {
	t.Helper()
	binary, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is not installed; real tmux regression not run")
	}
	// A test temp directory embeds the test name, and a socket path has a limit
	// of about 100 bytes, which a long TMPDIR would pass.
	root, err := os.MkdirTemp("", "ktmux")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	server := &realTmux{t: t, binary: binary, socket: filepath.Join(root, "socket"), wrapper: filepath.Join(root, "tmux"), session: realTmuxSession}
	t.Setenv("HOME", root)
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := exec.CommandContext(ctx, binary, "-S", server.socket, "-f", "/dev/null", "new-session", "-d", "-s", realTmuxSession, "-n", "keep", "/bin/sh")
	start.Dir = serverDir
	start.Env = append(os.Environ(), env...)
	if out, err := start.CombinedOutput(); err != nil {
		t.Fatalf("start isolated tmux: %v %s", err, out)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, binary, "-S", server.socket, "kill-server").Run()
	})
	server.writeWrapper()
	return server
}

func (s *realTmux) writeWrapper() {
	s.t.Helper()
	script := "#!/bin/sh\nif [ \"$1\" = respawn-pane ]; then\n" + s.before + "\n:\nfi\nexec " + posixQuote(s.binary) + " -S " + posixQuote(s.socket) + " \"$@\"\n"
	if err := os.WriteFile(s.wrapper, []byte(script), 0o700); err != nil {
		s.t.Fatal(err)
	}
}

// removeBeforeRespawn deletes dir between the validation and the cd.
func (s *realTmux) removeBeforeRespawn(dir string) {
	s.before = "rm -rf -- " + posixQuote(dir)
	s.writeWrapper()
}

func (s *realTmux) run(args ...string) string {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, s.binary, append([]string{"-S", s.socket}, args...)...).CombinedOutput()
	if err != nil {
		s.t.Fatalf("isolated tmux %q: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (s *realTmux) plan() LaunchPlan {
	return LaunchPlan{Launcher: "tmux", processName: "sleep", Target: terminal.Target{Program: s.wrapper, Session: s.session}}
}

func (s *realTmux) windowNames() []string {
	s.t.Helper()
	return strings.Fields(s.run("list-windows", "-t", s.session, "-F", "#{window_name}"))
}

// cwdAgent records its working directory, its PWD, and then stays alive so the
// launch sees a running process.
func cwdAgent(t *testing.T) (process.ProcessInvocation, string) {
	t.Helper()
	dir := t.TempDir()
	report := filepath.Join(dir, "report")
	agent := filepath.Join(dir, "agent")
	script := "#!/bin/sh\n{ pwd -P; printf '%s\\n' \"$PWD\"; } > " + posixQuote(report+".tmp") + "\nmv " + posixQuote(report+".tmp") + " " + posixQuote(report) + "\nexec sleep 30\n"
	if err := os.WriteFile(agent, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return process.ProcessInvocation{Argv: []string{agent, "--model", "m", "--approval", "never"}}, report
}

func wantAgentCwd(t *testing.T, report, startupDir string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var data []byte
	for {
		var err error
		if data, err = os.ReadFile(report); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("agent never reported: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	want, err := filepath.EvalSymlinks(startupDir)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 2 || lines[0] != want {
		t.Fatalf("agent cwd report %q, want %q", lines, want)
	}
	if got, err := filepath.EvalSymlinks(lines[1]); err != nil || got != want {
		t.Fatalf("agent PWD %q (%q, %v), want %q", lines[1], got, err, want)
	}
}

func TestRealTmuxAgentRunsInStartupDirectory(t *testing.T) {
	resetLang(t)
	for _, tc := range []struct {
		name       string
		subdir     string
		env        []string
		deleteHome bool
		// directoryOnly drops the process name, so only pane_directory can
		// prove the entry.
		directoryOnly bool
	}{
		{name: "server directory deleted", subdir: "project", deleteHome: true},
		{name: "plain directory", subdir: "project"},
		{name: "space and apostrophe", subdir: "pro ject's dir"},
		{name: "stale PWD", subdir: "project", env: []string{"PWD=/kander/stale/pwd"}},
		{name: "trailing space proven by the directory", subdir: "project ", directoryOnly: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serverDir := t.TempDir()
			server := startRealTmux(t, serverDir, tc.env...)
			if tc.deleteHome {
				if err := os.RemoveAll(serverDir); err != nil {
					t.Fatal(err)
				}
			}
			if len(tc.env) > 0 {
				server.run("set-environment", "-g", "PWD", "/kander/stale/pwd")
			}
			startupDir := filepath.Join(t.TempDir(), tc.subdir)
			if err := os.Mkdir(startupDir, 0o700); err != nil {
				t.Fatal(err)
			}
			invocation, report := cwdAgent(t)
			plan := server.plan()
			if tc.directoryOnly {
				plan.processName = ""
			}
			if _, err := launchAgent(plan, filepath.Join(startupDir, "kanban"), "task", invocation, nil, nil, nil); err != nil {
				t.Fatal(err)
			}
			wantAgentCwd(t, report, startupDir)
		})
	}
}

func TestRealTmuxDirectoryRemovedAfterValidationFailsTheLaunch(t *testing.T) {
	resetLang(t)
	for _, tc := range []struct {
		name         string
		remainOnExit bool
		durable      bool
	}{
		{name: "pane closes"},
		{name: "remain-on-exit keeps the dead pane", remainOnExit: true},
		{name: "durable dispatch", durable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := startRealTmux(t, t.TempDir())
			if tc.remainOnExit {
				server.run("set-option", "-g", "remain-on-exit", "on")
			}
			server.run("new-window", "-d", "-t", server.session+":", "-n", "other", "/bin/sh")
			startupDir := filepath.Join(t.TempDir(), "project")
			if err := os.Mkdir(startupDir, 0o700); err != nil {
				t.Fatal(err)
			}
			server.removeBeforeRespawn(startupDir)
			invocation, report := cwdAgent(t)
			_, err := launchAgent(server.plan(), filepath.Join(startupDir, "kanban"), "task", invocation, nil, nil, nil, tc.durable)
			var failure *LaunchFailure
			if !errors.As(err, &failure) || failure.DeliveryUnknown || failure.CloseError != "" {
				t.Fatalf("err=%#v", err)
			}
			if !strings.Contains(err.Error(), startupDir) {
				t.Fatalf("failure names no directory: %v", err)
			}
			if _, statErr := os.Stat(report); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("agent ran: %v", statErr)
			}
			if got := strings.Join(server.windowNames(), " "); got != "keep other" {
				t.Fatalf("windows after the failed launch: %q", got)
			}
		})
	}
}

// A shell that needs longer than the first polls to reach cd is still decided by
// the cd: an open pane alone never counts as started.
func TestRealTmuxSlowShellThatCannotEnterFailsTheLaunch(t *testing.T) {
	resetLang(t)
	server := startRealTmux(t, t.TempDir())
	slowShell := filepath.Join(filepath.Dir(server.wrapper), "slow-sh")
	if err := os.WriteFile(slowShell, []byte("#!/bin/sh\nsleep 3\nexec /bin/sh \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	server.run("set-option", "-g", "default-shell", slowShell)
	startupDir := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(startupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	server.removeBeforeRespawn(startupDir)
	invocation, report := cwdAgent(t)
	_, err := launchAgent(server.plan(), filepath.Join(startupDir, "kanban"), "task", invocation, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), startupDir) {
		t.Fatalf("err=%v", err)
	}
	if _, statErr := os.Stat(report); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("agent ran: %v", statErr)
	}
	if got := strings.Join(server.windowNames(), " "); got != "keep" {
		t.Fatalf("windows after the failed launch: %q", got)
	}
}

// The fork of a respawned pane reports the server's directory for a moment, so
// a server started in the project must not read as an entered pane.
func TestRealTmuxServerInTheStartupDirectoryIsNotEntry(t *testing.T) {
	resetLang(t)
	startupDir := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(startupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 20; attempt++ {
		server := startRealTmux(t, startupDir)
		server.removeBeforeRespawn(startupDir)
		invocation, report := cwdAgent(t)
		_, err := launchAgent(server.plan(), filepath.Join(startupDir, "kanban"), "task", invocation, nil, nil, nil)
		if err == nil {
			t.Fatal("launch succeeded without entering the directory")
		}
		if _, statErr := os.Stat(report); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("agent ran: %v", statErr)
		}
		server.run("kill-server")
		if err := os.Mkdir(startupDir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

// StartChat passes no paneSession, so nothing after run_command would notice
// the missing agent without the pane watch.
func TestRealTmuxChatFailsWhenTheDirectoryIsRemovedAfterValidation(t *testing.T) {
	resetLang(t)
	// setupBoard puts a fake tmux on PATH, so the real one is resolved first.
	server := startRealTmux(t, t.TempDir())
	_, _, bin := setupBoard(t)
	useChatConfig(t, "claude", "tmux")
	ran := filepath.Join(filepath.Dir(server.wrapper), "agent-ran")
	agent := "#!/bin/sh\n: > " + posixQuote(ran) + "\nexec sleep 30\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(agent), 0o700); err != nil {
		t.Fatal(err)
	}
	startupDir := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(startupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	server.removeBeforeRespawn(startupDir)
	t.Setenv("PATH", filepath.Dir(server.wrapper)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", server.socket+",1,0")
	t.Setenv("TMUX_PANE", server.run("list-panes", "-t", server.session, "-F", "#{pane_id}"))

	_, err := StartChat(ChatRequest{Root: filepath.Join(startupDir, "kanban"), Message: "hello"})
	if err == nil || !strings.Contains(err.Error(), startupDir) {
		t.Fatalf("err=%v", err)
	}
	if _, statErr := os.Stat(ran); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("agent ran: %v", statErr)
	}
	if got := strings.Join(server.windowNames(), " "); got != "keep" {
		t.Fatalf("windows after the failed chat: %q", got)
	}
}
