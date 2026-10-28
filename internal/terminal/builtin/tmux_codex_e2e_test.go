package builtin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

// realCodexEnv opts into TestTmuxDeliverTextSubmitsRealCodexTurn. The test
// spends model quota and needs a logged-in Codex, so CI never sets it.
const realCodexEnv = "KANDER_REAL_CODEX_E2E"

// TestTmuxDeliverTextSubmitsRealCodexTurn delivers a notify-style line to a
// real Codex TUI through the built-in tmux definition and requires the
// acknowledgement marker that only the agent can print. The delivered line
// names a file instead of carrying the marker, so an unsubmitted composer
// cannot satisfy the test.
//
// Optional settings: KANDER_REAL_CODEX_DIR is the working directory, which
// Codex must already trust (default: the module root); KANDER_REAL_CODEX_MODEL
// selects the model.
func TestTmuxDeliverTextSubmitsRealCodexTurn(t *testing.T) {
	if os.Getenv(realCodexEnv) != "1" {
		t.Skipf("set %s=1 to run against a real Codex CLI and tmux", realCodexEnv)
	}
	for _, tool := range []string{"tmux", "codex"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found: %v", tool, err)
		}
	}
	dir := os.Getenv("KANDER_REAL_CODEX_DIR")
	if dir == "" {
		root, err := filepath.Abs(filepath.Join("..", "..", ".."))
		if err != nil {
			t.Fatal(err)
		}
		dir = root
	}
	codex := []string{"codex", "--sandbox", "read-only"}
	if model := os.Getenv("KANDER_REAL_CODEX_MODEL"); model != "" {
		codex = append(codex, "--model", model)
	}

	// A private tmux server keeps the test away from the user's sessions.
	socket := "kander-e2e-" + randomHex(t, 4)
	conn := terminal.Conn{Program: "tmux", Run: func(ctx context.Context, program string, args []string) (probe.Result, error) {
		return terminal.SpawnRunner(ctx, program, append([]string{"-L", socket}, args...))
	}}
	tmux := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		result, err := conn.Run(ctx, "tmux", args)
		if err != nil || result.Code != 0 {
			t.Fatalf("tmux %q: err=%v code=%d stderr=%s", args, err, result.Code, result.Stderr)
		}
		return result.Stdout
	}
	tmux(append([]string{"new-session", "-d", "-s", "e2e", "-x", "200", "-y", "50", "-c", dir}, strings.Join(codex, " "))...)
	t.Cleanup(func() { _, _ = conn.Run(context.Background(), "tmux", []string{"kill-server"}) })
	pane := strings.TrimSpace(tmux("display-message", "-p", "-t", "e2e", "#{pane_id}"))
	backend := tmuxBackend(t, Tmux, os.Getenv)

	// Wait for the composer. A hooks review dialog is skipped once with Esc,
	// which continues without trusting any hook.
	skipped := false
	waitPane(t, conn, backend, pane, 90*time.Second, func(text string) bool {
		if !skipped && strings.Contains(text, "Continue without trusting") {
			tmux("send-keys", "-t", pane, "Escape")
			skipped = true
			return false
		}
		return strings.Contains(text, "? for shortcuts")
	})

	marker := "KANDER-E2E-ACK:" + randomHex(t, 16)
	message := filepath.Join(t.TempDir(), "message.md")
	if err := os.WriteFile(message, []byte("Acknowledge this notification: before anything else, output this line exactly:\n"+marker+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	line := "# kander-notify: read " + message + " and follow it; do not run any other command."
	if err := backend.DeliverText(context.Background(), conn, pane, line); err != nil {
		t.Fatal(err)
	}
	waitPane(t, conn, backend, pane, 120*time.Second, func(text string) bool { return strings.Contains(text, marker) })
}

func waitPane(t *testing.T, conn terminal.Conn, backend terminal.Backend, pane string, timeout time.Duration, done func(string) bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	text := ""
	for time.Now().Before(deadline) {
		var err error
		if text, err = backend.ReadOutput(context.Background(), conn, pane); err != nil {
			t.Fatal(err)
		}
		if done(text) {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("pane did not reach the expected state within %s:\n%s", timeout, text)
}

func randomHex(t *testing.T, size int) string {
	t.Helper()
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(data)
}
