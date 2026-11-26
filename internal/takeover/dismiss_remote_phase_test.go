package takeover

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
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

// writeStatusHC installs an hc whose terminal t8 runs claude with session
// session-1, as the remote dismiss fixture records it. The reported OSC 7501
// state is read from hc.status on each call, and the terminal is gone once
// hc.exited exists; /exit creates it.
func writeStatusHC(t *testing.T, root, state string) (setState func(string), log string) {
	t.Helper()
	log = filepath.Join(root, "hc.log")
	status := filepath.Join(root, "hc.status")
	exited := filepath.Join(root, "hc.exited")
	setState = func(state string) {
		if err := os.WriteFile(status, []byte(state), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	setState(state)
	script := `#!/bin/sh
printf '%s\n' "$*" >> ` + log + `
gone() { printf '%s\n' '{"error":{"kind":"not_found","message":"no terminal"}}' >&2; exit 3; }
state=$(cat ` + status + `)
case "$1" in
get) [ -f ` + exited + ` ] && gone; printf '%s\n' "{\"address\":\"t8\",\"labels\":{\"kander.session\":\"session-1\"},\"state\":\"live\",\"status\":{\"app\":\"claude-code\",\"state\":\"$state\"}}" ;;
ls) if [ -f ` + exited + ` ]; then printf '%s\n' '{"terminals":[]}'; else printf '%s\n' '{"terminals":[{"address":"t8","state":"live"}]}'; fi ;;
send) [ "$4" = /exit ] && : > ` + exited + `; printf '{}\n' ;;
kill|rm) : > ` + exited + `; gone ;;
*) printf '{}\n' ;;
esac
`
	if err := os.WriteFile(filepath.Join(root, "fake-bin", "hc"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return setState, log
}

// The same receipt order on herdr-core, the launcher of the reported case:
// a blocked terminal gets nothing and keeps the receipt prepared; the rerun
// sends /exit once the terminal is idle, or completes once it is gone.
func TestRemoteDismissOnHerdrCoreKeepsThePreparedReceiptWhileBlocked(t *testing.T) {
	for _, recovery := range []string{"idle", "gone"} {
		t.Run(recovery, func(t *testing.T) {
			d := newRemoteDismissAt(t, "hc-blocked-"+recovery, "herdr-core:t8")
			useEnglish(t)
			d.retain(t, nil)
			bindDismissProcess(t, "", "")
			setState, log := writeStatusHC(t, d.root, "blocked")
			code, _, stderr := d.run(t)
			if code == 0 || !strings.Contains(stderr, "pane status does not accept delivery") || !strings.Contains(stderr, "blocked") {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			if phase := d.receiptPhase(t); phase != "prepared" {
				t.Fatalf("phase after a failed check %q", phase)
			}
			if calls := mustReadFile(t, log); strings.Contains(calls, "send") || strings.Contains(calls, "kill") {
				t.Fatalf("a terminal operation ran although a check failed:\n%s", calls)
			}
			want := t_("remote.execution_dismissed", d.task)
			if recovery == "idle" {
				setState("idle")
			} else {
				if err := os.WriteFile(filepath.Join(d.root, "hc.exited"), nil, 0o644); err != nil {
					t.Fatal(err)
				}
				want = t_("remote.execution_target_gone", d.task, "herdr-core:t8")
			}
			if code, out, stderr := d.run(t); code != 0 || !strings.Contains(out, want) {
				t.Fatalf("rerun exit %d: %s %s", code, out, stderr)
			}
			if phase := d.receiptPhase(t); phase != "complete" {
				t.Fatalf("phase after the rerun %q", phase)
			}
			sent := strings.Contains(mustReadFile(t, log), "send --json t8 /exit")
			if sent != (recovery == "idle") {
				t.Fatalf("exit command sent=%v on %s:\n%s", sent, recovery, mustReadFile(t, log))
			}
		})
	}
}
