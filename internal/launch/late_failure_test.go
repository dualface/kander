package launch

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
)

// lateDiscoveryFailure makes the session discovery of a launch fail. during runs
// inside the launch, after the agent started and before discovery gives up.
func lateDiscoveryFailure(t *testing.T, during func()) {
	t.Helper()
	expired := false
	stubDiscovery(t, func(call int) []string {
		if call == 2 {
			during()
			expired = true
		}
		return []string{"existing"}
	})
	previous := nowFn
	nowFn = func() time.Time {
		if expired {
			return previous().Add(time.Hour)
		}
		return previous()
	}
	t.Cleanup(func() { nowFn = previous })
}

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
}

func containerClosed(root string) bool {
	_, err := os.Stat(filepath.Join(root, "tmux.log.kill"))
	return err == nil
}

func requireRecord(t *testing.T, root, task, status string) board.ExecutionRecord {
	t.Helper()
	record, _, err := board.CurrentExecution(root, task)
	if err != nil || record.Status != status {
		t.Fatalf("record=%+v err=%v, want %s", record, err, status)
	}
	return record
}

func requireUnresolved(t *testing.T, err error) *UnresolvedError {
	t.Helper()
	var unresolved *UnresolvedError
	if !errors.As(err, &unresolved) {
		t.Fatalf("err=%v", err)
	}
	return unresolved
}

// The sequence of issue #22: the executor publishes progress, then discovery fails.
func TestLateFailureKeepsAnExecutorThatWorked(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "late-evidence")
	var written uint64
	lateDiscoveryFailure(t, func() {
		executorWrite(t, root, task)
		s, err := board.ReadSnapshot(root, task)
		if err != nil {
			t.Fatal(err)
		}
		written = s.Revision
	})
	_, _, err := capture(t, func() error { return commandStart(root, "devin", "tmux", task) })
	unresolved := requireUnresolved(t, err)
	if containerClosed(root) {
		t.Fatal("the launch closed the container of a working executor")
	}
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	if s.Entry.State != "working" || s.Revision != written || !strings.Contains(s.Text, "executor progress") {
		t.Fatalf("state=%s revision=%d want %d", s.Entry.State, s.Revision, written)
	}
	record := requireRecord(t, root, task, board.ExecutionUnresolved)
	if record.Failure == "" || record.Failure != unresolved.Reason || unresolved.Window != board.MetadataFrom(s.Text, board.FieldWindow) || unresolved.Window == "" {
		t.Fatalf("record=%+v unresolved=%+v", record, unresolved)
	}
}

// The judgement must see the container still open. A judgement that runs after
// the close cannot save anything.
func TestStandingIsReadBeforeTheContainerCloses(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "late-order")
	lateDiscoveryFailure(t, func() {})
	reads := 0
	previous := readStandingFn
	t.Cleanup(func() { readStandingFn = previous })
	readStandingFn = func(root string, entry board.Entry) (board.ExecutionStanding, error) {
		reads++
		if containerClosed(root) {
			t.Error("the container closed before the judgement")
		}
		return previous(root, entry)
	}
	if _, _, err := capture(t, func() error { return commandStart(root, "devin", "tmux", task) }); err == nil {
		t.Fatal("the launch succeeded")
	}
	if reads != 1 || !containerClosed(root) {
		t.Fatalf("reads=%d closed=%v", reads, containerClosed(root))
	}
}

// The launcher's WINDOW write succeeds after the executor wrote, and moves the
// cursor past that write. The entrance still reports it.
func TestEvidenceSurvivesASuccessfulWindowWrite(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "late-window")
	lateDiscoveryFailure(t, func() {})
	previous := recordWindowFn
	t.Cleanup(func() { recordWindowFn = previous })
	recordWindowFn = func(root string, entry board.Entry, window, resolved string) (board.ExecutionResult, error) {
		executorWrite(t, root, task)
		return previous(root, entry, window, resolved)
	}
	_, _, err := capture(t, func() error { return commandStart(root, "devin", "tmux", task) })
	requireUnresolved(t, err)
	if containerClosed(root) {
		t.Fatal("the container closed over an earlier executor write")
	}
	requireRecord(t, root, task, board.ExecutionUnresolved)
}

func TestFailedCloseDoesNotRollBack(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "late-close")
	t.Setenv("KANBAN_TMUX_KILL_FAIL", "1")
	lateDiscoveryFailure(t, func() {})
	_, _, err := capture(t, func() error { return commandStart(root, "devin", "tmux", task) })
	unresolved := requireUnresolved(t, err)
	if !strings.Contains(unresolved.Reason, "fake kill failure") {
		t.Fatalf("reason=%q", unresolved.Reason)
	}
	s, err := board.ReadSnapshot(root, task)
	if err != nil || s.Entry.State != "working" {
		t.Fatalf("a card with a possibly running executor left working: %+v %v", s.Entry, err)
	}
	requireRecord(t, root, task, board.ExecutionUnresolved)
}

// A write between the judgement and the rollback loses nothing: the rollback
// refuses and the attempt still ends.
func TestWriteAfterTheJudgementEndsUnresolved(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "late-race")
	lateDiscoveryFailure(t, func() {})
	previous := readStandingFn
	t.Cleanup(func() { readStandingFn = previous })
	readStandingFn = func(root string, entry board.Entry) (board.ExecutionStanding, error) {
		standing, err := previous(root, entry)
		executorWrite(t, root, task)
		return standing, err
	}
	_, _, err := capture(t, func() error { return commandStart(root, "devin", "tmux", task) })
	requireUnresolved(t, err)
	if !containerClosed(root) {
		t.Fatal("the judgement allowed the close")
	}
	s, err := board.ReadSnapshot(root, task)
	if err != nil || s.Entry.State != "working" || !strings.Contains(s.Text, "executor progress") {
		t.Fatalf("state=%s err=%v", s.Entry.State, err)
	}
	requireRecord(t, root, task, board.ExecutionUnresolved)
}

func TestUnreadableStandingKeepsTheContainer(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "late-unread")
	lateDiscoveryFailure(t, func() {})
	previous := readStandingFn
	t.Cleanup(func() { readStandingFn = previous })
	readStandingFn = func(string, board.Entry) (board.ExecutionStanding, error) {
		return board.ExecutionStanding{}, errors.New("read failed")
	}
	_, _, err := capture(t, func() error { return commandStart(root, "devin", "tmux", task) })
	requireUnresolved(t, err)
	if containerClosed(root) {
		t.Fatal("an unknown standing closed the container")
	}
}

func takeoverFixture(t *testing.T, root, task string) {
	t.Helper()
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = board.BindExecutionCursor(root, s.Entry); err != nil {
		t.Fatal(err)
	}
	if _, err = board.BeginTakeover(root, s.Entry, board.TakeoverInput{Owner: "claude", Session: "claude taken", Decision: "user"}); err != nil {
		t.Fatal(err)
	}
}

func adoptFixture(t *testing.T, root, task string) {
	t.Helper()
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = board.BindExecutionCursor(root, s.Entry); err != nil {
		t.Fatal(err)
	}
	if _, err = board.AdoptExecution(root, s, "user"); err != nil {
		t.Fatal(err)
	}
}

func TestSupersededStartExitsNormally(t *testing.T) {
	skipOnWindows(t)
	for name, overtake := range map[string]func(*testing.T, string, string){"handoff": takeoverFixture, "adoption": adoptFixture} {
		t.Run(name+"/failed", func(t *testing.T) {
			root, _, _ := setupBoard(t)
			task, _ := makeTodo(t, root, "superseded-late")
			lateDiscoveryFailure(t, func() { overtake(t, root, task) })
			out, _, err := capture(t, func() error { return commandStart(root, "devin", "tmux", task) })
			if err != nil || !strings.Contains(out, "superseded: "+task) {
				t.Fatalf("out=%q err=%v", out, err)
			}
			if containerClosed(root) {
				t.Fatal("a superseded launcher closed the container")
			}
		})
		t.Run(name+"/confirm", func(t *testing.T) {
			root, _, _ := setupBoard(t)
			task, _ := makeTodo(t, root, "superseded-confirm")
			previous := recordWindowFn
			t.Cleanup(func() { recordWindowFn = previous })
			recordWindowFn = func(root string, entry board.Entry, window, resolved string) (board.ExecutionResult, error) {
				result, err := previous(root, entry, window, resolved)
				overtake(t, root, task)
				return result, err
			}
			var result StartResult
			_, _, err := capture(t, func() error { var e error; result, e = Start(root, "claude", "tmux", task); return e })
			if err != nil || !result.Superseded {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if containerClosed(root) {
				t.Fatal("a superseded launcher closed the container")
			}
		})
	}
}

// foregroundAgent replaces the fake devin with a process that stays alive.
func foregroundAgent(t *testing.T, bin string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(bin, "devin"), []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	stdinIsTTY, stdoutIsTTY, stderrIsTTY = func() bool { return true }, func() bool { return true }, func() bool { return true }
	t.Cleanup(func() {
		stdinIsTTY = func() bool { return fileIsTTY(os.Stdin) }
		stdoutIsTTY = func() bool { return fileIsTTY(os.Stdout) }
		stderrIsTTY = func() bool { return fileIsTTY(os.Stderr) }
	})
}

// A discovery failure leaves the tmux pane without a session marker, so the output
// names only the takeover, after stopping the old executor by hand.
func TestUnresolvedOutputAndExitStatus(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "late-output")
	lateDiscoveryFailure(t, func() { executorWrite(t, root, task) })
	_, _, err := capture(t, func() error { return commandStart(root, "devin", "tmux", task) })
	if unresolved := requireUnresolved(t, err); !unresolved.Unmarked {
		t.Fatalf("unresolved=%+v", unresolved)
	}
	code := 0
	out, _, _ := capture(t, func() error { code = exitLaunch(err); return nil })
	if code != exitUnresolved || code == 1 {
		t.Fatalf("code=%d", code)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if !strings.HasPrefix(lines[0], "started-unresolved: "+task+" window=tmux:") || !strings.Contains(lines[0], " reason=") {
		t.Fatalf("out=%q", out)
	}
	steps := 0
	for _, line := range lines {
		if strings.Contains(line, "resume "+task) {
			steps++
			if !strings.Contains(line, "--decision <reference>") || strings.Contains(line, "--adopt") {
				t.Fatalf("next step: %q", line)
			}
		}
	}
	window := strings.TrimPrefix(strings.Fields(lines[0])[2], "window=")
	if steps != 1 || !strings.Contains(out, config.Text("launch.unmarked_executor_stop_first", window)) {
		t.Fatalf("out=%q", out)
	}
	code = 0
	_, _, _ = capture(t, func() error { code = exitLaunch(errors.New("plain")); return nil })
	if code != 1 {
		t.Fatalf("plain failure code=%d", code)
	}
}

func TestResumeRefusesMixedAdoptOptions(t *testing.T) {
	setupBoard(t)
	for _, args := range [][]string{
		{"--adopt", "task"},
		{"--adopt", "--decision", "user", "--agent", "claude", "task"},
		{"--adopt", "--decision", "user", "--message", "go", "task"},
		{"--adopt", "--decision", "user", "--launcher", "tmux", "task"},
		{"--adopt", "--decision", "user", "--timeout", "90", "task"},
		{"--adopt", "--decision", "user", "--kind", "sync", "task"},
	} {
		code := 0
		_, _, _ = capture(t, func() error { code = RunResumeRemote(args); return nil })
		if code != 2 {
			t.Errorf("%v: code=%d", args, code)
		}
	}
}
