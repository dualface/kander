package takeover

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

// receiptPhase returns the phase of the single dismiss receipt.
func (d *remoteDismiss) receiptPhase(t *testing.T) string {
	t.Helper()
	files := d.receipts(t)
	if len(files) != 1 {
		t.Fatalf("receipts %v", files)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		Kind  string `json:"kind"`
		Phase string `json:"phase"`
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatalf("receipt %s: %v", data, err)
	}
	if receipt.Kind != "dismiss" {
		t.Fatalf("receipt kind %q", receipt.Kind)
	}
	return receipt.Phase
}

// useEnglish renders the command output in English.
func useEnglish(t *testing.T) {
	t.Helper()
	t.Setenv(config.EnvLang, "en")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "en"})
	t.Cleanup(func() { config.ApplyLanguageArgument([]string{"kander", "--lang", "cn"}) })
}

// blockedDismiss runs a dismiss against a pane that does not accept delivery
// and checks that it sent nothing and left the receipt prepared.
func blockedDismiss(t *testing.T, slug string) *remoteDismiss {
	t.Helper()
	d := newRemoteDismiss(t, slug)
	useEnglish(t)
	d.retain(t, nil)
	bindDismissProcess(t, "", "")
	t.Setenv("KANBAN_HERDR_STATUS", "blocked")
	code, _, stderr := d.run(t)
	if code == 0 || !strings.Contains(stderr, "pane status does not accept delivery") || !strings.Contains(stderr, "blocked") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if phase := d.receiptPhase(t); phase != "prepared" {
		t.Fatalf("phase after a failed check %q", phase)
	}
	if d.terminalLog("prompt") || d.terminalLog("close") {
		t.Fatal("a terminal operation ran although a check failed")
	}
	return d
}

func TestRemoteDismissKeepsThePreparedReceiptWhenTheAgentPaneIsBlocked(t *testing.T) {
	d := blockedDismiss(t, "blocked-then-idle")
	t.Setenv("KANBAN_HERDR_STATUS", "idle")
	if code, out, stderr := d.run(t); code != 0 || !strings.Contains(out, t_("remote.execution_dismissed", d.task)) {
		t.Fatalf("rerun exit %d: %s %s", code, out, stderr)
	}
	if !d.terminalLog("prompt") || !d.terminalLog("close") {
		t.Fatal("the rerun did not send the exit command and close the container")
	}
	if phase := d.receiptPhase(t); phase != "complete" {
		t.Fatalf("phase after the rerun %q", phase)
	}
}

func TestRemoteDismissCompletesAPreparedReceiptOnceThePaneIsGone(t *testing.T) {
	d := blockedDismiss(t, "blocked-then-gone")
	t.Setenv("KANBAN_HERDR_STALE_PANE", "w1:p8")
	if code, out, stderr := d.run(t); code != 0 || !strings.Contains(out, t_("remote.execution_target_gone", d.task, "herdr:w1:t8:w1:p8")) {
		t.Fatalf("rerun exit %d: %s %s", code, out, stderr)
	}
	if phase := d.receiptPhase(t); phase != "complete" {
		t.Fatalf("phase after the rerun %q", phase)
	}
	if d.terminalLog("prompt") || d.terminalLog("close") {
		t.Fatal("a terminal operation ran for an absent terminal")
	}
}

func TestRemoteDismissKeepsThePreparedReceiptWhenTheProcessPaneIsRefused(t *testing.T) {
	d := newRemoteDismissAt(t, "process-refused", "tmux:$88:@8:%8")
	d.retain(t, nil)
	bindDismissProcess(t, "", "")
	// The pane proves the execution, but its window holds another pane: the
	// container check before the first send refuses it.
	t.Setenv("KANBAN_TMUX_WINDOW_PANES", "2")
	code, _, stderr := d.run(t)
	if code == 0 || !strings.Contains(stderr, t_("takeover.the_tmux_window_does_not_contain_only_the_target", "@8", "2")) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if phase := d.receiptPhase(t); phase != "prepared" {
		t.Fatalf("phase after a failed check %q", phase)
	}
	for _, name := range []string{"tmux.log.instruction", "tmux.log.kill"} {
		if _, err := os.Stat(filepath.Join(d.root, name)); err == nil {
			t.Fatalf("%s was written although a check failed", name)
		}
	}
}

func TestRemoteDismissKeepsExitingAfterTheSendFailed(t *testing.T) {
	d := newRemoteDismiss(t, "send-failed")
	d.retain(t, nil)
	bindDismissProcess(t, "", "")
	t.Setenv("KANBAN_HERDR_PROMPT_FAIL", "prompt refused")
	if code, _, stderr := d.run(t); code == 0 || !strings.Contains(stderr, "prompt refused") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if phase := d.receiptPhase(t); phase != "exiting" {
		t.Fatalf("phase after a failed send %q", phase)
	}
	// The pane may have received the exit command: while it exists, a rerun
	// asks for reconciliation instead of sending again.
	if err := os.Unsetenv("KANBAN_HERDR_PROMPT_FAIL"); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := d.run(t)
	if code == 0 || !strings.Contains(stderr, t_("remote.launch_reconciliation_required", d.receipts(t)[0])) {
		t.Fatalf("rerun exit %d: %s", code, stderr)
	}
	if d.terminalLog("prompt") || d.terminalLog("close") {
		t.Fatal("the rerun sent to a pane that may have received the exit command")
	}
	if phase := d.receiptPhase(t); phase != "exiting" {
		t.Fatalf("phase after the rerun %q", phase)
	}
}
