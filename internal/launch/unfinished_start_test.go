package launch

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/liveness"
)

// unfinishedStart claims a card through a start attempt no launcher ended, and
// leaves it at status with a resumable session on a live tmux address.
func unfinishedStart(t *testing.T, root, slug, status string) (string, board.Entry) {
	t.Helper()
	task, _ := makeTodo(t, root, slug)
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := board.BeginTaskStart(s.Entry, root, board.TaskStartInput{Owner: "claude", Session: "claude 2f1b7c7e-6c8a-4e0e-9d55-0a6f2c1d3b4e", Window: "tmux:$1:@1:%1"})
	if err != nil {
		t.Fatal(err)
	}
	if status == board.ExecutionUnresolved {
		if _, err = board.UnresolveExecution(root, moved, "", "late failure"); err != nil {
			t.Fatal(err)
		}
	}
	return task, moved
}

func runResume(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	code := 0
	out, errOut, _ := capture(t, func() error {
		code = RunResume(args)
		return nil
	})
	return code, out, errOut
}

// A plain resume beside an unfinished start would run a second process and move
// WINDOW away from the executor that adoption and takeover must find.
func TestPlainResumeRefusesAnUnfinishedStart(t *testing.T) {
	skipOnWindows(t)
	for _, status := range []string{board.ExecutionPending, board.ExecutionUnresolved} {
		for name, extra := range map[string][]string{"message": nil, "dispatch": {"--dispatch-id", "live-guard", "--kind", "sync"}} {
			t.Run(status+"/"+name, func(t *testing.T) {
				root, _, _ := setupBoard(t)
				task, _ := unfinishedStart(t, root, "resume-unfinished", status)
				before, err := board.ReadSnapshot(root, task)
				if err != nil {
					t.Fatal(err)
				}
				record := requireRecord(t, root, task, status)

				args := append([]string{"--message", "x", "--timeout", "61"}, extra...)
				code, out, errOut := runResume(t, append(args, task)...)

				if code == 0 || strings.Contains(out, "live-guard") {
					t.Fatalf("code=%d out=%s", code, out)
				}
				for _, way := range []string{"resume " + task + " --adopt --decision", "resume " + task + " --agent <agent> --decision"} {
					if !strings.Contains(errOut, way) {
						t.Fatalf("no %q in %s", way, errOut)
					}
				}
				if _, err = os.Stat(filepath.Join(root, "tmux.log.command")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("a process started: %v", err)
				}
				after, err := board.ReadSnapshot(root, task)
				if err != nil {
					t.Fatal(err)
				}
				if after.Revision != before.Revision || after.Text != before.Text {
					t.Fatalf("card changed: %d to %d", before.Revision, after.Revision)
				}
				if current := requireRecord(t, root, task, status); current != record {
					t.Fatalf("record changed: %+v to %+v", record, current)
				}
				if _, err = board.ReadDispatch(root, task, "live-guard"); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("a dispatch bound: %v", err)
				}
			})
		}
	}
}

func TestPlainResumeKeepsARunningExecution(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "resume-running")
	if _, _, err := capture(t, func() error { return commandStart(root, "claude", "", task) }); err != nil {
		t.Fatal(err)
	}
	session := liveness.ParseTaskSession(snapshotText(t, root, task))
	code, _, errOut := runResume(t, "--message", "x", "--timeout", "61", task)
	if code != 0 {
		t.Fatalf("code=%d %s", code, errOut)
	}
	if cmd := lastCommand(t, root); !strings.Contains(cmd, "--resume "+session.Reference) {
		t.Fatalf("command=%s", cmd)
	}
	requireRecord(t, root, task, board.ExecutionRunning)
}

// The check is about a card that can still be adopted or taken over.
func TestResumableIgnoresACardOutsideTheExecutionColumns(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, entry := unfinishedStart(t, root, "resume-left", board.ExecutionUnresolved)
	if err := RequireResumable(root, entry); err == nil {
		t.Fatal("the working card passed")
	}
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	trashed, err := board.MoveWithOptions(s.Entry, root, "trash", board.MoveOptions{Result: "trashed", Reason: "fixture", Decision: "user decision"})
	if err != nil {
		t.Fatal(err)
	}
	requireRecord(t, root, task, board.ExecutionUnresolved)
	if err = RequireResumable(root, trashed); err != nil {
		t.Fatal(err)
	}
}

func snapshotText(t *testing.T, root, task string) string {
	t.Helper()
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	return s.Text
}
