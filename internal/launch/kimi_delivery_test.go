package launch

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
)

// stubKimiDiscovery returns one new session id after the pre-launch snapshot.
func stubKimiDiscovery(t *testing.T, id string) {
	t.Helper()
	previous := enumerateSessionsFn
	calls := 0
	enumerateSessionsFn = func(context.Context, *config.SessionDiscovery, *process.AgentProgram, string, string) ([]string, error) {
		calls++
		if calls == 1 {
			return nil, nil
		}
		return []string{id}, nil
	}
	t.Cleanup(func() { enumerateSessionsFn = previous })
}

func TestKimiStartDeliversPromptToPane(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, bin := setupBoard(t)
	_ = bin
	t.Setenv("KANBAN_TMUX_PANE_OUTPUT", "Never Ask  /repo  context: 0%")
	stubKimiDiscovery(t, "kimi-ses-1")
	var captured []string
	previous := newShellInvocation
	newShellInvocation = func(p process.AgentProgram, args []string, env map[string]string) (process.ProcessInvocation, error) {
		captured = append([]string{}, args...)
		return previous(p, args, env)
	}
	t.Cleanup(func() { newShellInvocation = previous })
	id, _ := makeTodo(t, root, "kimi-pane")
	_, _, err := capture(t, func() error { return commandStart(root, "kimi", "tmux", id) })
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(captured, "\n")
	if strings.Contains(joined, "UTF-8 task file") {
		t.Fatalf("prompt leaked into kimi argv: %q", captured)
	}
	if !strings.Contains(joined, "--auto") {
		t.Fatalf("kimi argv missing --auto: %q", captured)
	}
	sent := mustRead(t, filepath.Join(root, "tmux.log")+".send-keys")
	if !strings.Contains(sent, "UTF-8 task file") || !strings.Contains(sent, "Enter") {
		t.Fatalf("prompt was not delivered to the pane: %q", sent)
	}
	snapshot, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snapshot.Text, "- SESSION: kimi kimi-ses-1\n") {
		t.Fatalf("discovered session was not persisted: %s", snapshot.Text)
	}
}

func TestKimiTrustDialogBlocksDelivery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	stubKimiDiscovery(t, "kimi-ses-blocked")
	t.Setenv("KANBAN_TMUX_BLOCKED_OUTPUT", "Trust this folder?\nTrust this folder\nDon't trust")
	id, _ := makeTodo(t, root, "kimi-trust")
	_, _, err := capture(t, func() error { return commandStart(root, "kimi", "tmux", id) })
	if err == nil {
		t.Fatal("trust dialog must block the launch")
	}
	if !strings.Contains(err.Error(), "trust the launch directory") {
		t.Fatalf("blocked reason missing: %v", err)
	}
	if _, stat := os.Stat(filepath.Join(root, "tmux.log") + ".send-keys"); !os.IsNotExist(stat) {
		t.Fatalf("prompt was sent while the trust dialog was up: %v", stat)
	}
}

func TestKimiStartDeliversPromptThroughHerdr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("herdr fakes are POSIX")
	}
	root, _, bin := setupBoard(t)
	herdrLog := installHerdr(t, root, bin)
	t.Setenv("KANBAN_HERDR_OUTPUT", "Never Ask  /repo  context: 0%")
	stubKimiDiscovery(t, "kimi-ses-2")
	id, _ := makeTodo(t, root, "kimi-herdr")
	_, _, err := capture(t, func() error { return commandStart(root, "kimi", "herdr", id) })
	if err != nil {
		t.Fatal(err)
	}
	prompt := mustRead(t, herdrLog+".prompt")
	if !strings.Contains(prompt, "agent\nprompt") || !strings.Contains(prompt, "UTF-8 task file") {
		t.Fatalf("herdr did not deliver the prompt: %q", prompt)
	}
	snapshot, err := board.ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snapshot.Text, "- SESSION: kimi kimi-ses-2\n") {
		t.Fatalf("discovered session was not persisted: %s", snapshot.Text)
	}
}

func TestKimiRejectsDirectLaunchers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fakes")
	}
	root, _, _ := setupBoard(t)
	stubKimiDiscovery(t, "kimi-ses-direct")
	stdinIsTTY, stdoutIsTTY, stderrIsTTY = func() bool { return true }, func() bool { return true }, func() bool { return true }
	t.Cleanup(func() {
		stdinIsTTY = func() bool { return fileIsTTY(os.Stdin) }
		stdoutIsTTY = func() bool { return fileIsTTY(os.Stdout) }
		stderrIsTTY = func() bool { return fileIsTTY(os.Stderr) }
	})
	id, _ := makeTodo(t, root, "kimi-foreground")
	_, _, err := capture(t, func() error { return commandStart(root, "kimi", "foreground", id) })
	if err == nil || !strings.Contains(err.Error(), "tmux") || !strings.Contains(err.Error(), "herdr") {
		t.Fatalf("foreground: %v", err)
	}
}
