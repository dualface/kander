package launch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/liveness"
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

func TestLateFailureWithoutEvidenceClosesAndRollsBack(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "late-clean")
	lateDiscoveryFailure(t, func() {})
	_, _, err := capture(t, func() error { return commandStart(root, "devin", "tmux", task) })
	if err == nil || isUnresolved(err) {
		t.Fatalf("err=%v", err)
	}
	if !containerClosed(root) {
		t.Fatal("the failed launch kept its container")
	}
	s, err := board.ReadSnapshot(root, task)
	if err != nil || s.Entry.State != "todo" {
		t.Fatalf("snapshot %+v err=%v", s.Entry, err)
	}
	requireRecord(t, root, task, board.ExecutionRolledBack)
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
	if _, err = board.AdoptExecution(root, s.Entry, "user"); err != nil {
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

// Without a container there is nothing to keep, but the rule stays: evidence
// means no rollback, and the executor is not killed.
func TestLateFailureWithoutAContainer(t *testing.T) {
	skipOnWindows(t)
	for name, evidence := range map[string]bool{"evidence": true, "clean": false} {
		t.Run(name, func(t *testing.T) {
			root, _, bin := setupBoard(t)
			foregroundAgent(t, bin)
			task, _ := makeTodo(t, root, "late-process")
			lateDiscoveryFailure(t, func() {
				if evidence {
					executorWrite(t, root, task)
				}
			})
			// capture would wait for the pipe the live agent inherits.
			result, err := Start(root, "devin", "foreground", task)
			s, readErr := board.ReadSnapshot(root, task)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !evidence {
				if err == nil || isUnresolved(err) || s.Entry.State != "todo" {
					t.Fatalf("state=%s err=%v", s.Entry.State, err)
				}
				requireRecord(t, root, task, board.ExecutionRolledBack)
				return
			}
			unresolved := requireUnresolved(t, err)
			defer func() { _ = result.Outcome.Process.Kill(); _, _ = result.Outcome.Wait() }()
			if s.Entry.State != "working" || unresolved.Window != "foreground" || unresolved.hold == nil {
				t.Fatalf("state=%s unresolved=%+v", s.Entry.State, unresolved)
			}
			if result.Outcome.Poll() != nil {
				t.Fatal("the launch killed the executor")
			}
			requireRecord(t, root, task, board.ExecutionUnresolved)
		})
	}
}

func TestUnresolvedOutputAndExitStatus(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "late-output")
	lateDiscoveryFailure(t, func() { executorWrite(t, root, task) })
	_, _, err := capture(t, func() error { return commandStart(root, "devin", "tmux", task) })
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
			if !strings.Contains(line, "--decision <reference>") {
				t.Fatalf("next step without --decision: %q", line)
			}
		}
	}
	if steps != 2 {
		t.Fatalf("out=%q", out)
	}
	code = 0
	_, _, _ = capture(t, func() error { code = exitLaunch(errors.New("plain")); return nil })
	if code != 1 {
		t.Fatalf("plain failure code=%d", code)
	}
}

// The same rules hold for the launcher of a takeover, and it exits the same way.
func TestTakeoverLateFailureEndsUnresolved(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, path := makeTodo(t, root, "takeover-late")
	startThenReview(t, root, "claude", task, path)
	lateDiscoveryFailure(t, func() { executorWrite(t, root, task) })
	agent := "devin"
	_, _, err := capture(t, func() error { return commandResume(root, &agent, "tmux", task, "take over", "", true, 61) })
	requireUnresolved(t, err)
	if containerClosed(root) {
		t.Fatal("the takeover closed the container of a working executor")
	}
	record := requireRecord(t, root, task, board.ExecutionUnresolved)
	if record.Kind != board.ExecutionTakeover {
		t.Fatalf("record %+v", record)
	}
}

func TestTakeoverDecisionRequirement(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "takeover-decision")
	lateDiscoveryFailure(t, func() { executorWrite(t, root, task) })
	_, _, err := capture(t, func() error { return commandStart(root, "devin", "tmux", task) })
	requireUnresolved(t, err)
	agent := "claude"
	_, _, err = capture(t, func() error { return commandResume(root, &agent, "tmux", task, "take over", "", true, 61) })
	if err == nil || !strings.Contains(err.Error(), "--decision") {
		t.Fatalf("err=%v", err)
	}
	requireRecord(t, root, task, board.ExecutionUnresolved)
	_, _, err = capture(t, func() error {
		return commandResume(root, &agent, "tmux", task, "take over", "", true, 61, DispatchOptions{Decision: "user approved"})
	})
	if err != nil {
		t.Fatal(err)
	}
	record := requireRecord(t, root, task, board.ExecutionRunning)
	if record.Kind != board.ExecutionTakeover {
		t.Fatalf("record %+v", record)
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
		{"--decision", "user", "--message", "go", "task"},
	} {
		code := 0
		_, _, _ = capture(t, func() error { code = RunResume(args); return nil })
		if code != 2 {
			t.Errorf("%v: code=%d", args, code)
		}
	}
}

func stubAdoptLiveness(t *testing.T, status string) {
	t.Helper()
	previous := adoptLivenessFn
	t.Cleanup(func() { adoptLivenessFn = previous })
	adoptLivenessFn = func(board.Snapshot) liveness.Report { return liveness.Report{Status: status, Detail: "probe detail"} }
}

func TestAdoptKeepsTheExecutor(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "adopt-keep")
	lateDiscoveryFailure(t, func() { executorWrite(t, root, task) })
	_, _, err := capture(t, func() error { return commandStart(root, "devin", "tmux", task) })
	requireUnresolved(t, err)
	created, _ := os.ReadFile(filepath.Join(root, "tmux.log"))
	for _, status := range []string{liveness.Unknown, liveness.Stopped, liveness.Drifted} {
		stubAdoptLiveness(t, status)
		_, _, err = capture(t, func() error { return commandAdopt(root, task, "user") })
		if err == nil || !strings.Contains(err.Error(), status) {
			t.Fatalf("%s: err=%v", status, err)
		}
		requireRecord(t, root, task, board.ExecutionUnresolved)
	}
	stubAdoptLiveness(t, liveness.Alive)
	out, _, err := capture(t, func() error { return commandAdopt(root, task, "user") })
	if err != nil || !strings.Contains(out, "adopted: "+task) {
		t.Fatalf("out=%q err=%v", out, err)
	}
	requireRecord(t, root, task, board.ExecutionRunning)
	if after, _ := os.ReadFile(filepath.Join(root, "tmux.log")); string(after) != string(created) {
		t.Fatal("adoption started a new executor")
	}
	if _, _, err = capture(t, func() error { return commandAdopt(root, task, "user") }); err == nil {
		t.Fatal("a running record was adopted")
	}
}

// foreground and console have no probeable address until they get a process
// identity, so the real observation refuses them.
func TestAdoptRefusesAnAddressWithoutLiveness(t *testing.T) {
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "adopt-foreground")
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = board.BeginTaskStart(s.Entry, root, board.TaskStartInput{Owner: "devin", Session: "devin", Window: "foreground"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = capture(t, func() error { return commandAdopt(root, task, "user") }); err == nil || !strings.Contains(err.Error(), liveness.Unknown) {
		t.Fatalf("err=%v", err)
	}
	requireRecord(t, root, task, board.ExecutionPending)
}

func TestSessionRepairWaitsForAnUnfinishedStart(t *testing.T) {
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "repair-unfinished")
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := board.BeginTaskStart(s.Entry, root, board.TaskStartInput{Owner: "codex", Session: "codex", Window: "tmux:$1:@1:%1"})
	if err != nil {
		t.Fatal(err)
	}
	current, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	refused := func() {
		t.Helper()
		_, err := RepairMissingSession(context.Background(), root, current, "")
		if err == nil || !strings.Contains(err.Error(), "--adopt") || !strings.Contains(err.Error(), "--agent") {
			t.Fatalf("err=%v", err)
		}
	}
	refused()
	if _, err = board.UnresolveExecution(root, moved, "", "late failure"); err != nil {
		t.Fatal(err)
	}
	refused()
}

// groupedUnresolved runs the sequence of issue #22 on a member of a checkpointed
// group, and leaves the member in unresolved with its executor kept.
func groupedUnresolved(t *testing.T, slug string) (root, task string, c board.CoordinatorCheckpoint) {
	t.Helper()
	root, _, _ = setupBoard(t)
	task, _ = makeTodo(t, root, slug)
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	const group = "20261031-late-failure-group"
	text := strings.Replace(s.Text, "- TASK_GROUP:", "- TASK_GROUP: "+group, 1)
	if err = writeCardFixture(root, s.Entry, text); err != nil {
		t.Fatal(err)
	}
	c, err = board.ClaimCoordinator(context.Background(), root, board.CoordinatorClaim{GroupID: group, Owner: "coordinator", Token: "one-session", Basis: "test", Members: []string{task}})
	if err != nil {
		t.Fatal(err)
	}
	lateDiscoveryFailure(t, func() { executorWrite(t, root, task) })
	_, _, err = capture(t, func() error { return commandStart(root, "devin", "tmux", task) })
	requireUnresolved(t, err)
	if c, err = ReconcileCoordinator(context.Background(), root, launchCoordinatorRequest(t, root, task, c, board.Dispatch{})); err != nil {
		t.Fatalf("reconcile of an unresolved member: %v", err)
	}
	if !c.Members[task].AwaitingStart {
		t.Fatalf("mirror %+v", c.Members[task])
	}
	return root, task, c
}

// finishMember takes a member with a running execution through review, wrap-up
// and done, reconciling after each step a coordinator sees.
func finishMember(t *testing.T, root, task string, c board.CoordinatorCheckpoint) {
	t.Helper()
	c, err := ReconcileCoordinator(context.Background(), root, launchCoordinatorRequest(t, root, task, c, board.Dispatch{}))
	if err != nil {
		t.Fatalf("reconcile after the exit: %v", err)
	}
	if c.Members[task].AwaitingStart {
		t.Fatalf("mirror %+v", c.Members[task])
	}
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(s.Text, "- TASK_BRANCH:", "- TASK_BRANCH: late-failure", 1)
	if err = board.UpdateDocument(root, task, board.UpdateOptions{Document: "spec.md", Text: text, ExpectedRevision: s.Revision}); err != nil {
		t.Fatal(err)
	}
	if s, err = board.ReadSnapshot(root, task); err != nil {
		t.Fatal(err)
	}
	if _, err = board.MoveEntry(s.Entry, root, "review"); err != nil {
		t.Fatal(err)
	}
	in := closedWrapUp(t, root, task)
	d, err := PrepareBoundDispatch(root, in)
	if err != nil {
		t.Fatal(err)
	}
	if c, err = ReconcileCoordinator(context.Background(), root, launchCoordinatorRequest(t, root, task, c, d)); err != nil {
		t.Fatalf("reconcile of the wrap-up: %v", err)
	}
	if s, err = board.ReadSnapshot(root, task); err != nil {
		t.Fatal(err)
	}
	if _, err = board.MoveWithOptions(s.Entry, root, "working", board.MoveOptions{Authorization: d.Authorization}); err != nil {
		t.Fatal(err)
	}
	if s, err = board.ReadSnapshot(root, task); err != nil {
		t.Fatal(err)
	}
	text = strings.ReplaceAll(s.Text, "<FILL_IN>", "done")
	text = strings.Replace(text, "## SUMMARY\n", "## SUMMARY\n\ndone\n", 1)
	if err = board.UpdateDocument(root, task, board.UpdateOptions{Document: "spec.md", Text: text, ExpectedRevision: s.Revision, Authorization: d.Authorization}); err != nil {
		t.Fatal(err)
	}
	if s, err = board.ReadSnapshot(root, task); err != nil {
		t.Fatal(err)
	}
	if _, err = board.MoveWithOptions(s.Entry, root, "done", board.MoveOptions{Authorization: d.Authorization, DeliveryCommit: in.Base, Result: "completed"}); err != nil {
		t.Fatal(err)
	}
	if d, err = board.ReadDispatch(root, task, d.Input.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = ReconcileCoordinator(context.Background(), root, launchCoordinatorRequest(t, root, task, c, d)); err != nil {
		t.Fatalf("reconcile of the done member: %v", err)
	}
}

func TestGroupMemberLeavesUnresolvedByAdoption(t *testing.T) {
	skipOnWindows(t)
	root, task, c := groupedUnresolved(t, "group-adopt")
	stubAdoptLiveness(t, liveness.Alive)
	if _, _, err := capture(t, func() error { return commandAdopt(root, task, "user") }); err != nil {
		t.Fatal(err)
	}
	finishMember(t, root, task, c)
}

func TestGroupMemberLeavesUnresolvedByTakeover(t *testing.T) {
	skipOnWindows(t)
	root, task, c := groupedUnresolved(t, "group-takeover")
	agent := "claude"
	_, _, err := capture(t, func() error {
		return commandResume(root, &agent, "tmux", task, "take over", "", true, 61, DispatchOptions{Decision: "user approved"})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireRecord(t, root, task, board.ExecutionRunning)
	finishMember(t, root, task, c)
}
