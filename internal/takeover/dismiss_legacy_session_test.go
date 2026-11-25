package takeover

import (
	"os"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/remote"
)

// legacyCodexDismiss is a done card of a Codex execution that a remote start
// recorded before it confirmed the discovered session to the server: the card
// and the execution record hold only the agent, while the retained execution
// holds the full session, in the same terminal.
func legacyCodexDismiss(t *testing.T, slug string) *remoteDismiss {
	t.Helper()
	d := newRemoteDismiss(t, slug)
	text := strings.Replace(d.card.Text, "- OWNER: claude", "- OWNER: codex", 1)
	text = strings.Replace(text, "- SESSION: claude session-1", "- SESSION: codex", 1)
	if board.MetadataFrom(text, board.FieldOwner) != "codex" || board.MetadataFrom(text, board.FieldSession) != "codex" {
		t.Fatalf("the fixture card did not take the legacy Codex session:\n%s", text)
	}
	d.card.Text = text
	d.record.Owner, d.record.Session = "codex", "codex"
	d.retain(t, func(c *remote.Credential) { c.Session = "codex session-1" })
	t.Setenv("KANBAN_HERDR_AGENT", "codex")
	bindDismissProcess(t, "", "")
	return d
}

func TestRemoteDismissOfALegacyCodexExecutionWithItsTerminalGone(t *testing.T) {
	d := legacyCodexDismiss(t, "legacy-gone")
	t.Setenv("KANBAN_HERDR_STALE_PANE", "w1:p8")
	code, out, stderr := d.run(t)
	window := board.MetadataFrom(d.card.Text, board.FieldWindow)
	if code != 0 || !strings.Contains(out, t_("remote.execution_target_gone", d.task, window)) {
		t.Fatalf("exit %d: %s %s", code, out, stderr)
	}
	files := d.receipts(t)
	if len(files) != 1 {
		t.Fatalf("receipts %v", files)
	}
	if data, err := os.ReadFile(files[0]); err != nil || !strings.Contains(string(data), `"phase":"complete"`) {
		t.Fatalf("receipt is not complete: %s %v", data, err)
	}
	if d.terminalLog("prompt") || d.terminalLog("close") {
		t.Fatal("a terminal operation ran for an absent terminal")
	}
}

// The live pane proves the retained session, so the agent gets its exit
// command and the container is closed.
func TestRemoteDismissOfALegacyCodexExecutionInItsProvenPane(t *testing.T) {
	d := legacyCodexDismiss(t, "legacy-live")
	code, out, stderr := d.run(t)
	if code != 0 || !strings.Contains(out, t_("remote.execution_dismissed", d.task)) {
		t.Fatalf("exit %d: %s %s", code, out, stderr)
	}
	prompt, err := os.ReadFile(d.root + "/herdr.log.prompt")
	if err != nil || !strings.Contains(string(prompt), "/exit") || !d.terminalLog("close") {
		t.Fatalf("the agent was not told to exit and closed: %q %v", prompt, err)
	}
}

// A pane that carries another session does not prove the retained one, so
// nothing is sent to it.
func TestRemoteDismissOfALegacyCodexExecutionRefusesAnotherSession(t *testing.T) {
	d := legacyCodexDismiss(t, "legacy-other")
	t.Setenv("KANBAN_HERDR_SESSION", "session-2")
	code, _, stderr := d.run(t)
	if code == 0 || !strings.Contains(stderr, "pane no longer proves the lost execution session") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if d.terminalLog("prompt") || d.terminalLog("close") {
		t.Fatal("a terminal operation ran against a pane of another session")
	}
}

// Only the agent of the retained session relaxes the comparison: a card that
// names another agent is still a scene mismatch.
func TestRemoteDismissRefusesACardOfAnotherAgentThanTheRetainedSession(t *testing.T) {
	d := newRemoteDismiss(t, "legacy-other-agent")
	d.card.Text = strings.Replace(d.card.Text, "- SESSION: claude session-1", "- SESSION: claude", 1)
	d.record.Session = "claude"
	d.retain(t, func(c *remote.Credential) { c.Session = "codex session-1" })
	bindDismissProcess(t, "", "")
	window := board.MetadataFrom(d.card.Text, board.FieldWindow)
	d.requireRefused(t, t_("remote.local_execution.scene_mismatch", "codex session-1", window, "claude", window))
}
