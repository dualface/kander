package notify

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var (
	ackMarkerPattern   = regexp.MustCompile(`KANDER-NOTIFY-ACK:[0-9a-f]{32}`)
	messagePathPattern = regexp.MustCompile(`/[^ ,]*/message\.txt`)
)

// assertAckOnlyInMessageFile checks that the delivered instruction names a message file which
// carries the acknowledgement request, while the instruction itself never carries the marker.
// It returns the marker.
func assertAckOnlyInMessageFile(t *testing.T, instruction string) string {
	t.Helper()
	if strings.Contains(instruction, "KANDER-NOTIFY-ACK:") {
		t.Fatalf("instruction carries the acknowledgement marker: %s", instruction)
	}
	matches := messagePathPattern.FindAllString(instruction, -1)
	if len(matches) == 0 {
		t.Fatalf("instruction names no message file: %s", instruction)
	}
	data, err := os.ReadFile(matches[len(matches)-1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "Acknowledge this notification: before anything else, output this line exactly:\n") {
		t.Fatalf("message file lacks the acknowledgement request: %s", data)
	}
	marker := ackMarkerPattern.FindString(string(data))
	if marker == "" {
		t.Fatalf("message file lacks the marker: %s", data)
	}
	return marker
}

func tmuxNotifyFixture(t *testing.T, slug string) (string, string) {
	t.Helper()
	root, _ := setupBoard(t)
	taskID, path := makeReview(t, root, slug)
	setWindow(t, path, "tmux:$1:@1:%1")
	t.Setenv("KANBAN_TMUX_CURRENT_COMMAND", "claude")
	t.Setenv("KANBAN_TMUX_PANE_SESSION", "session-1")
	return root, taskID
}

// Regression for issue #18: a pane that only echoes the delivered instruction, as an
// unsubmitted composer does, must not confirm the notification.
func TestTmuxNotifyEchoedInstructionStaysUnconfirmed(t *testing.T) {
	root, taskID := tmuxNotifyFixture(t, "notify-tmux-echo")
	oldNow, oldSleep := nowFn, sleepFn
	t.Cleanup(func() { nowFn, sleepFn = oldNow, oldSleep })
	base := time.Unix(0, 0)
	nowFn = func() time.Time { return base }
	sleepFn = func(d time.Duration) { base = base.Add(d) }
	out, errOut, err := capture(t, func() error {
		return commandNotify(root, taskID, "x", "", "", true, 61)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "回执=未确认") {
		t.Fatalf("echoed instruction confirmed the notification: %s", out)
	}
	if !strings.Contains(errOut, "警告: 已投递, 未在超时内确认") {
		t.Fatalf("missing unconfirmed warning: %s", errOut)
	}
	instruction, _ := os.ReadFile(filepath.Join(root, "tmux.log.instruction"))
	assertAckOnlyInMessageFile(t, string(instruction))
}

func TestTmuxNotifyAgentMarkerConfirms(t *testing.T) {
	root, taskID := tmuxNotifyFixture(t, "notify-tmux-ack")
	t.Setenv("KANBAN_TMUX_AGENT_ACK", "1")
	out, _, err := capture(t, func() error {
		return commandNotify(root, taskID, "x", "", "", true, 61)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "回执=已确认") {
		t.Fatalf("agent marker did not confirm: %s", out)
	}
}

func TestHerdrNotifyWaitsForMessageFileMarker(t *testing.T) {
	root, _ := setupBoard(t)
	taskID, _ := makeReview(t, root, "notify-herdr-marker")
	out, _, err := capture(t, func() error {
		return commandNotify(root, taskID, "x", "", "", true, 61)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "回执=已确认") {
		t.Fatalf("out=%s", out)
	}
	prompt, _ := os.ReadFile(filepath.Join(root, "herdr.log.prompt"))
	marker := assertAckOnlyInMessageFile(t, string(prompt))
	wait, _ := os.ReadFile(filepath.Join(root, "herdr.log.wait"))
	if !strings.Contains("\n"+string(wait), "\n--match\n"+marker+"\n") {
		t.Fatalf("wait-output does not match the message file marker %s: %s", marker, wait)
	}
}
