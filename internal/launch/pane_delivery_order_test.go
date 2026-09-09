package launch

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestTmuxPaneDeliveryWaitsBeforeSessionDiscovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX terminal fakes")
	}
	root, _, bin := setupBoard(t)
	cfg := usePaneAgent(t, root, bin, "tmux")
	t.Setenv("KANBAN_TMUX_READY_AFTER", "2")
	t.Setenv("KANBAN_TMUX_PANE_OUTPUT", "TUI_READY")
	log := filepath.Join(root, "tmux.log")
	freezeClock(t)
	advance := sleepFn
	waited := false
	sleepFn = func(d time.Duration) {
		waited = true
		if _, err := os.Stat(log + ".send-keys"); !os.IsNotExist(err) {
			t.Fatalf("prompt sent before ready: %v", err)
		}
		advance(d)
	}
	plan, err := prepareLaunch("tmux", filepath.Dir(root), "start")
	if err != nil {
		t.Fatal(err)
	}
	if err := applyAgentDelivery(&plan, cfg, "panecli"); err != nil {
		t.Fatal(err)
	}
	program, err := requireAgentProgram("panecli", cfg)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := launchInvocation(plan, *program, attachPrompt(&plan, []string{"--interactive"}, "task prompt"))
	if err != nil {
		t.Fatal(err)
	}
	discovered := false
	callback := func() (AgentSession, error) {
		discovered = true
		order := mustRead(t, log+".order")
		if !strings.Contains(order, "respawn-pane\ncapture-pane\ncapture-pane\nsend-keys\nsend-keys\n") {
			t.Fatalf("launch, readiness and delivery out of order: %s", order)
		}
		sent := mustRead(t, log+".send-keys")
		if sent != "send-keys\n-t\n%9\n-l\ntask prompt\nsend-keys\n-t\n%9\nEnter\n" {
			t.Fatalf("prompt and Enter must be separate calls before session discovery: %q", sent)
		}
		return AgentSession{Agent: "panecli", Reference: "discovered-session"}, nil
	}
	if _, err := launchAgent(plan, root, "pane-order", inv, nil, callback, nil); err != nil {
		t.Fatal(err)
	}
	if !waited || !discovered {
		t.Fatalf("waited=%v discovered=%v", waited, discovered)
	}
	if got := strings.TrimSpace(mustRead(t, log+".pane-session")); got != "discovered-session" {
		t.Fatalf("session marker=%q", got)
	}
}
