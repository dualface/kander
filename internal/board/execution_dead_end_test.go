package board

import (
	"strings"
	"testing"
)

// Directed reproductions of the launcher half of DE-03, DE-04 and DE-15. The
// takeover handoff gives the board a way out, so the model no longer reaches
// them. Card 20261030-gh-dualface-kander-22-task turns these tests red; it then
// deletes them and the matching rows of modelDeadEnds. See docs/model-test.md.

// DE-03 and DE-15: nothing stops a dispatch from binding a start that has no result.
func TestDeadEndDispatchBindsAPendingStart(t *testing.T) {
	root := tempBoard(t)
	s := coordinatorTodo(t, root, "pending-dispatch")
	if _, err := BeginTaskStart(s.Entry, root, TaskStartInput{Owner: "codex", Session: "codex"}); err != nil {
		t.Fatal(err)
	}
	in := DispatchInput{ID: "pending-dispatch", TaskID: s.Entry.TaskID, Kind: "sync", Message: "同步", Base: strings.Repeat("a", 40)}
	if _, err := PrepareDispatch(root, in); err != nil {
		t.Fatalf("the dead end is gone; remove this test and its modelDeadEnds rows: %v", err)
	}
}

// DE-04: a takeover already ended the attempt, and the launcher reads that as a
// failure instead of stepping aside.
func TestDeadEndConfirmAfterHandoffIsAnError(t *testing.T) {
	root := tempBoard(t)
	s := coordinatorTodo(t, root, "raced-start")
	moved, err := BeginTaskStart(s.Entry, root, TaskStartInput{Owner: "codex", Session: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	current := transactionSnapshot(t, root, s.Entry.TaskID)
	if _, err = BindExecutionCursor(root, current.Entry); err != nil {
		t.Fatal(err)
	}
	if _, err = BeginTakeover(root, current.Entry, TakeoverInput{Owner: "claude", Session: "claude next"}); err != nil {
		t.Fatal(err)
	}
	if err = ConfirmTaskStart(root, moved); err == nil {
		t.Fatal("the dead end is gone; remove this test and its modelDeadEnds row")
	}
}
