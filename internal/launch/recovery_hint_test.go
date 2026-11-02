package launch

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/liveness"
)

// requireTakeoverOnly checks a recovery hint that must not list adoption as a
// next step and must name the takeover.
func requireTakeoverOnly(t *testing.T, task string, err error, want ...string) {
	t.Helper()
	if err == nil || strings.Contains(err.Error(), "resume "+task+" --adopt") || !strings.Contains(err.Error(), "resume "+task+" --agent <agent> --decision") {
		t.Fatalf("err=%v", err)
	}
	for _, text := range want {
		if !strings.Contains(err.Error(), text) {
			t.Fatalf("err=%v, want %q", err, text)
		}
	}
}

func readSnapshot(t *testing.T, root, task string) board.Snapshot {
	t.Helper()
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The sequence of issue #22 leaves a tmux pane without a session marker. The real
// probe of every recovery path reads it unknown, so none of them offers adoption.
func TestUnmarkedStartOffersOnlyTheTakeover(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "unmarked-hints")
	lateDiscoveryFailure(t, func() { executorWrite(t, root, task) })
	_, _, err := capture(t, func() error { return commandStart(root, "devin", "tmux", task) })
	unresolved := requireUnresolved(t, err)
	if !unresolved.Unmarked || unresolved.Window == "" {
		t.Fatalf("unresolved=%+v", unresolved)
	}
	stop := config.Text("launch.unmarked_executor_stop_first", unresolved.Window)
	_, _, err = capture(t, func() error { return commandAdopt(root, task, "user") })
	requireTakeoverOnly(t, task, err, stop)
	requireRecord(t, root, task, board.ExecutionUnresolved)
	s := readSnapshot(t, root, task)
	cannot := config.Text("launch.unmarked_pane_cannot_adopt", unresolved.Window)
	requireTakeoverOnly(t, task, RequireResumable(root, s.Entry), cannot)
	_, err = RepairMissingSession(context.Background(), root, s, "")
	requireTakeoverOnly(t, task, err, cannot)
}

// A start that wrote the session marker and failed afterwards keeps adoption: the
// pane can prove its executor.
func TestMarkedStartKeepsAdoption(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "marked-hints")
	previous := confirmStartFn
	t.Cleanup(func() { confirmStartFn = previous })
	confirmStartFn = func(string, board.Entry) error {
		executorWrite(t, root, task)
		return errors.New("confirm failed")
	}
	_, _, err := capture(t, func() error { return commandStart(root, "claude", "tmux", task) })
	unresolved := requireUnresolved(t, err)
	if unresolved.Unmarked {
		t.Fatalf("unresolved=%+v", unresolved)
	}
	if steps := unresolved.NextSteps(); len(steps) != 2 || !strings.Contains(steps[0], "resume "+task+" --adopt --decision <reference>") {
		t.Fatalf("steps=%q", steps)
	}
	err = RequireResumable(root, readSnapshot(t, root, task).Entry)
	if err == nil || err.Error() != launchError("board.dispatch_start_unfinished", task, board.ExecutionUnresolved).Error() {
		t.Fatalf("err=%v", err)
	}
}

// oldTakenOverCard is a card 1.0.0 started and then took over: its schema 1 start
// attempt stays pending, while OWNER, SESSION and WINDOW name the new executor.
func oldTakenOverCard(t *testing.T, root, slug string) string {
	t.Helper()
	task, _ := makeTodo(t, root, slug)
	s := readSnapshot(t, root, task)
	text, err := board.NewClaimMetadata(s.Text)
	if err != nil {
		t.Fatal(err)
	}
	if err = board.WithTransaction(root, board.LockScope{Tasks: []string{task}, ExclusiveBoard: true}, func(tx *board.Transaction) error {
		if e := tx.Put(task, "spec.md", text); e != nil {
			return e
		}
		return tx.Relocate(task, "working")
	}); err != nil {
		t.Fatal(err)
	}
	setCardField(t, root, task, "OWNER", "claude")
	setCardField(t, root, task, "SESSION", oldReclaimSession)
	s = readSnapshot(t, root, task)
	attempt := map[string]any{"id": "old-attempt", "task_id": task, "cycle": board.ExecutionCycle(s.Entry, s.Text), "owner": "claude", "session": oldReclaimSession, "revision": s.Revision, "status": "pending"}
	dir := filepath.Join(root, ".kander", "groups", "00000000-start-group", task)
	if err = os.MkdirAll(filepath.Join(dir, "old-attempt"), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(attempt)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(dir, "old-attempt", "pending.json"), filepath.Join(dir, "current.json")} {
		if err = os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	setCardField(t, root, task, "OWNER", "codex")
	setCardField(t, root, task, "SESSION", "codex 1.0.0-takeover")
	setCardField(t, root, task, "WINDOW", "tmux:$1:@1:%1")
	return task
}

// The record of an old card shape names an executor the card no longer shows, so
// adoption refuses before it probes, and every hint names only the takeover.
func TestOldTakenOverCardOffersOnlyTheTakeover(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task := oldTakenOverCard(t, root, "old-takeover")
	requireRecord(t, root, task, board.ExecutionPending)
	probed := false
	previous := adoptLivenessFn
	t.Cleanup(func() { adoptLivenessFn = previous })
	adoptLivenessFn = func(board.Snapshot) liveness.Report {
		probed = true
		return liveness.Report{Status: liveness.Alive}
	}
	_, _, err := capture(t, func() error { return commandAdopt(root, task, "user") })
	var e *board.Error
	if !errors.As(err, &e) || e.Code != "board.adopt_start_identity_changed" || probed {
		t.Fatalf("err=%v probed=%v", err, probed)
	}
	requireTakeoverOnly(t, task, err)
	requireRecord(t, root, task, board.ExecutionPending)
	s := readSnapshot(t, root, task)
	changed := config.Text("launch.start_identity_changed")
	requireTakeoverOnly(t, task, RequireResumable(root, s.Entry), changed)
	_, err = RepairMissingSession(context.Background(), root, s, "")
	requireTakeoverOnly(t, task, err, changed)
	_, err = RecoverCoordinator(context.Background(), root, board.CoordinatorRecover{Member: task})
	requireTakeoverOnly(t, task, err, changed)
	if probed {
		t.Fatal("a hint probed a card whose identity changed")
	}
}
