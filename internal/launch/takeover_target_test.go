package launch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

type cleanupCall struct {
	window    string
	session   AgentSession
	newWindow string
}

// driftedWorkingCard is a started card whose recorded pane stopped running the
// agent. The respawn of a takeover writes the current command again, so a probe
// after the launch would find the recorded pane alive.
func driftedWorkingCard(t *testing.T, slug string) (root, task string) {
	t.Helper()
	skipOnWindows(t)
	root, _, _ = setupBoard(t)
	task, _ = makeTodo(t, root, slug)
	if _, _, err := capture(t, func() error { return commandStart(root, "claude", "tmux", task) }); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tmux.log.current"), []byte("bash\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(root, "fake-bin", "tmux-real")
	writeFakeTmux(t, real, filepath.Join(root, "tmux.log"))
	script := `#!/bin/sh
if [ "$1" = "list-panes" ]; then
  printf '%s\n' "$1" >> "$KANBAN_TMUX_LOG.order"
  printf '%s\n' "$KANBAN_TMUX_LIST_PANES"
  exit 0
fi
exec "` + real + `" "$@"
`
	if err := os.WriteFile(filepath.Join(root, "fake-bin", "tmux"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return root, task
}

func recordCleanup(t *testing.T) *[]cleanupCall {
	t.Helper()
	saved := CleanupTakeover
	t.Cleanup(func() { CleanupTakeover = saved })
	calls := &[]cleanupCall{}
	CleanupTakeover = func(window string, session AgentSession, newWindow string, _ float64) CleanupResult {
		*calls = append(*calls, cleanupCall{window, session, newWindow})
		return CleanupResult{Cleaned: true, OldWindow: window, Channel: "tmux", Container: "@5"}
	}
	return calls
}

func takeOver(t *testing.T, root, task, agent string) string {
	t.Helper()
	out, _, err := capture(t, func() error {
		return commandResume(root, &agent, "tmux", task, "take over", "", true, 61, DispatchOptions{Decision: "user approved"})
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTakeoverLocatesTheDriftedExecutorBeforeTheNewOneStarts(t *testing.T) {
	root, task := driftedWorkingCard(t, "takeover-drift")
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	reference := strings.Fields(board.MetadataFrom(s.Text, board.FieldSession))[1]
	t.Setenv("KANBAN_TMUX_LIST_PANES", paneRow("%5", reference))
	calls := recordCleanup(t)
	takeOver(t, root, task, "claude")
	if len(*calls) != 1 || (*calls)[0].window != "tmux:$9:@9:%5" {
		t.Fatalf("calls=%+v", *calls)
	}
	if (*calls)[0].newWindow == (*calls)[0].window {
		t.Fatalf("the new executor is the target: %+v", *calls)
	}
	order, err := os.ReadFile(filepath.Join(root, "tmux.log.order"))
	if err != nil {
		t.Fatal(err)
	}
	located, started := strings.LastIndex(string(order), "list-panes"), strings.LastIndex(string(order), "respawn-pane")
	if located < 0 || located > started {
		t.Fatalf("order:\n%s", order)
	}
}

// The new Codex rollout is the newest of the task, so resolving after the launch
// would search for the new executor.
func TestTakeoverResolvesTheOldCodexSessionBeforeItsOwnRolloutExists(t *testing.T) {
	root, task := driftedWorkingCard(t, "takeover-codex")
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	if err = executionFixture(root, s.Entry, "codex", "codex", board.MetadataFrom(s.Text, board.FieldWindow)); err != nil {
		t.Fatal(err)
	}
	codexRolloutAt(t, task, "old", "old-rollout-id", "", nowFn())
	t.Setenv("KANBAN_TMUX_LIST_PANES", "%5\t$9\tmoved\t@9\tcodex\t0\told-rollout-id\t")
	calls := recordCleanup(t)
	takeOver(t, root, task, "codex")
	if len(*calls) != 1 || (*calls)[0].window != "tmux:$9:@9:%5" || (*calls)[0].session.Reference != "old-rollout-id" {
		t.Fatalf("calls=%+v", *calls)
	}
}

func TestTakeoverKeepsTheRecordedAddressWithNoDrift(t *testing.T) {
	root, task := driftedWorkingCard(t, "takeover-stopped")
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANBAN_TMUX_LIST_PANES", paneRow("%5", "another-session"))
	calls := recordCleanup(t)
	takeOver(t, root, task, "claude")
	if len(*calls) != 1 || (*calls)[0].window != board.MetadataFrom(s.Text, board.FieldWindow) {
		t.Fatalf("calls=%+v", *calls)
	}
}

func TestTakeoverDoesNotReportCleanedWhenTheSearchFails(t *testing.T) {
	root, task := driftedWorkingCard(t, "takeover-ambiguous")
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	reference := strings.Fields(board.MetadataFrom(s.Text, board.FieldSession))[1]
	t.Setenv("KANBAN_TMUX_LIST_PANES", paneRow("%5", reference)+"\n"+paneRow("%6", reference))
	calls := recordCleanup(t)
	out := takeOver(t, root, task, "claude")
	if len(*calls) != 0 {
		t.Fatalf("calls=%+v", *calls)
	}
	if strings.Contains(out, "已清理原容器") || !strings.Contains(out, "原容器保留") {
		t.Fatalf("out=%s", out)
	}
}
