package launch

// Directed reproductions of the known dead ends that sit inside a launcher, out
// of reach of the board model in internal/board/model_test.go. Each test asserts
// that the dead end exists. A repair turns its test red; the repair card then
// deletes the test and the matching row of modelDeadEnds. See docs/model-test.md.

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
)

func deadEndExecutorWrite(t *testing.T, root, taskID string) {
	t.Helper()
	s, err := board.ReadSnapshot(root, taskID)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.TrimRight(s.Text, "\n") + "\n\n- executor progress\n"
	if err := board.UpdateDocument(root, taskID, board.UpdateOptions{Document: "spec.md", Text: text, ExpectedRevision: s.Revision}); err != nil {
		t.Fatalf("executor write: %v", err)
	}
}

// assertStranded checks the shape every launcher dead end leaves behind: the
// launch failed, its container closed, and the executor's record stayed on a
// card no later start accepts.
func assertStranded(t *testing.T, root, taskID string, launchErr error, wantState string) {
	t.Helper()
	if launchErr == nil {
		t.Fatal("launch succeeded: the dead end is gone; remove this test and its modelDeadEnds row")
	}
	t.Logf("launch error: %v", launchErr)
	s, err := board.ReadSnapshot(root, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if s.Entry.State != wantState || !strings.Contains(s.Text, "executor progress") {
		t.Fatalf("state=%s, executor record kept=%v", s.Entry.State, strings.Contains(s.Text, "executor progress"))
	}
	if _, err := os.Stat(filepath.Join(root, "tmux.log.kill")); err != nil {
		t.Fatalf("the failed launch kept its container: %v", err)
	}
	if _, _, err := capture(t, func() error { return commandStart(root, "devin", "tmux", taskID) }); err == nil {
		t.Fatal("a later start accepted the stranded card")
	}
}

// stubDiscovery lets poll run on each session enumeration of a launch.
func stubDiscovery(t *testing.T, poll func(call int) []string) {
	t.Helper()
	calls := 0
	previous := enumerateSessionsFn
	enumerateSessionsFn = func(context.Context, *config.SessionDiscovery, *process.AgentProgram, string, string) ([]string, error) {
		calls++
		return poll(calls), nil
	}
	t.Cleanup(func() { enumerateSessionsFn = previous })
}

// DE-01 and DE-02: the launcher closes the container before it judges whether
// the executor already worked, and its cursor then loses to the executor's write.
func TestDeadEndStartClosesContainerOverExecutorWork(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	for name, discovered := range map[string][]string{"discovery-fails": {"existing"}, "confirm-fails": {"existing", "devin-new-session"}} {
		t.Run(name, func(t *testing.T) {
			root, _, _ := setupBoard(t)
			taskID, _ := makeTodo(t, root, "dead-end-start")
			expired := false
			stubDiscovery(t, func(call int) []string {
				if call == 1 {
					return []string{"existing"}
				}
				if call == 2 {
					deadEndExecutorWrite(t, root, taskID)
					expired = true
				}
				return discovered
			})
			previousNow := nowFn
			nowFn = func() time.Time {
				if expired {
					return previousNow().Add(time.Hour)
				}
				return previousNow()
			}
			t.Cleanup(func() { nowFn = previousNow })
			_, _, err := capture(t, func() error { return commandStart(root, "devin", "tmux", taskID) })
			assertStranded(t, root, taskID, err, "working")
		})
	}
}

// DE-16: with pane delivery the executor runs before WINDOW is written, and that
// write uses the launcher's exact revision.
func TestDeadEndPaneWindowWriteLosesToExecutor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	t.Setenv("KANBAN_TMUX_PANE_OUTPUT", "Never Ask  /repo  context: 0%")
	stubKimiDiscovery(t, "kimi-dead-end")
	taskID, _ := makeTodo(t, root, "dead-end-window")
	delivered := filepath.Join(root, "tmux.log.send-keys")
	wrote := false
	previous := readDocumentFn
	readDocumentFn = func(e board.Entry) (string, error) {
		if _, err := os.Stat(delivered); err == nil && !wrote {
			wrote = true
			deadEndExecutorWrite(t, root, taskID)
		}
		return previous(e)
	}
	t.Cleanup(func() { readDocumentFn = previous })
	_, _, err := capture(t, func() error { return commandStart(root, "kimi", "tmux", taskID) })
	if !wrote {
		t.Fatal("the prompt was not delivered before WINDOW")
	}
	assertStranded(t, root, taskID, err, "working")
}

// DE-17: the takeover writes the discovered session through the cursor it held
// before the new executor ran.
func TestDeadEndTakeoverDiscoveryUsesStaleCursor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	taskID, path := makeTodo(t, root, "dead-end-takeover")
	startThenReview(t, root, "claude", taskID, path)
	stubDiscovery(t, func(call int) []string {
		if call == 1 {
			return []string{"existing"}
		}
		if call == 2 {
			deadEndExecutorWrite(t, root, taskID)
		}
		return []string{"existing", "devin-new-session"}
	})
	agent := "devin"
	_, _, err := capture(t, func() error { return commandResume(root, &agent, "tmux", taskID, "take over", "", true, 61) })
	assertStranded(t, root, taskID, err, "review")
}
