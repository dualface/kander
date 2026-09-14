package builtin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

// isolatedTmux starts a private tmux server on its own socket and returns a
// connection whose commands address only that server.
func isolatedTmux(t *testing.T) (terminal.Conn, func(args ...string) string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("tmux is POSIX only")
	}
	program, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is not installed")
	}
	// A tmux socket path must stay short, so the directory is not t.TempDir.
	dir, err := os.MkdirTemp("", "kt")
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "s")
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.Command(program, append([]string{"-S", socket, "-f", "/dev/null"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("tmux %q: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	t.Cleanup(func() {
		_ = exec.Command(program, "-S", socket, "kill-server").Run()
		_ = os.RemoveAll(dir)
	})
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	run("new-session", "-d", "-s", "seed", "/bin/sh")
	conn := terminal.Conn{Program: program, Run: func(ctx context.Context, name string, args []string) (probe.Result, error) {
		return probe.CaptureContext(ctx, name, append([]string{"-S", socket}, args...))
	}}
	return conn, run
}

// tmux 3.6 answers display-message for a closed pane or window with exit 0
// and empty fields; both facts and container existence must report gone.
func TestTmuxDefinitionRealClosedTargetsAreGone(t *testing.T) {
	resetLang(t)
	conn, run := isolatedTmux(t)
	backend := tmuxBackend(t, Tmux, os.Getenv)
	ctx := context.Background()
	pane := run("new-window", "-d", "-P", "-F", "#{pane_id}", "-t", "seed:", "/bin/sh")
	window := run("display-message", "-p", "-t", pane, "#{window_id}")
	run("set-option", "-p", "-t", pane, "@kander_session", "session-1")
	address := terminal.Address{Session: "seed", Container: window, Pane: pane}

	facts, err := backend.PaneFacts(ctx, conn, pane)
	if err != nil || facts.Gone || facts.Command == "" || facts.Dead != "0" || facts.InMode != "0" || facts.SessionMarker != "session-1" {
		t.Fatalf("live pane facts=%+v err=%v", facts, err)
	}
	if exists, err := backend.ContainerExists(ctx, conn, address); err != nil || !exists {
		t.Fatalf("live window exists=%v err=%v", exists, err)
	}

	if err := backend.CloseContainer(ctx, conn, address); err != nil {
		t.Fatal(err)
	}
	facts, err = backend.PaneFacts(ctx, conn, pane)
	if err != nil || !facts.Gone || facts.GoneDetail != config.Text("terminal.target_answered_empty") {
		t.Fatalf("closed pane facts=%+v err=%v", facts, err)
	}
	if exists, err := backend.ContainerExists(ctx, conn, address); err != nil || exists {
		t.Fatalf("closed window exists=%v err=%v", exists, err)
	}
}
