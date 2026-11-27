package liveness

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/remote"
)

func TestObserveExecutorsKeepsUnobservableExecutorsUnknown(t *testing.T) {
	statuses := ObserveExecutors(t.Context(), []remote.RetainedExecution{{TaskID: "20261101-gc-task", Session: "claude s-1"}, {TaskID: "20261101-gc-task", Window: "herdr:pane-1"}})
	if len(statuses) != 2 || statuses[0] != Unknown || statuses[1] != Unknown {
		t.Fatalf("an execution without a session or a window is unknown: %v", statuses)
	}
}

// A stale recorded pane is no proof the executor stopped: the session that
// moved to another pane is found by reverse lookup, and its worktree stays.
func TestObserveExecutorsFindsAnExecutorThatMovedPanes(t *testing.T) {
	resetLang(t)
	installPOSIXFakes(t, true)
	file := writePiSessionFile(t, "wanted")
	t.Setenv("KANBAN_HERDR_STALE_PANE", "w1:p1")
	t.Setenv("KANBAN_HERDR_SESSION_KIND", "path")
	t.Setenv("KANBAN_HERDR_AGENT", "pi")
	t.Setenv("KANBAN_HERDR_SESSION", file)
	t.Setenv("KANBAN_HERDR_LIST_JSON", listJSON(herdrRow("w9:t9", "w9:p9", "pi", `{"kind":"path","value":"`+file+`"}`)))
	statuses := ObserveExecutors(t.Context(), []remote.RetainedExecution{{TaskID: "20261101-gc-task", Session: "pi wanted", Window: "herdr:w1:t1:w1:p1"}})
	if len(statuses) != 1 || statuses[0] != Drifted {
		t.Fatalf("an executor in another pane is found there, not taken for stopped: %v", statuses)
	}
}

// A stale address of a session without a reference was never searched for:
// the card classification calls it stopped, the gc keeps it unknown.
func TestObserveExecutorsKeepsAnUnsearchedStaleSessionUnknown(t *testing.T) {
	resetLang(t)
	installPOSIXFakes(t, true)
	t.Setenv("KANBAN_HERDR_STALE_PANE", "w1:p1")
	t.Setenv("KANBAN_HERDR_AGENT", "pi")
	if report := ClassifyTask(board.Entry{TaskID: "20261101-gc-task"}, classifyText("pi", "herdr:w1:t1:w1:p1")); report.Status != Stopped {
		t.Fatalf("the premise: a card classification calls it stopped: %+v", report)
	}
	statuses := ObserveExecutors(t.Context(), []remote.RetainedExecution{{TaskID: "20261101-gc-task", Session: "pi", Window: "herdr:w1:t1:w1:p1"}})
	if len(statuses) != 1 || statuses[0] != Unknown {
		t.Fatalf("a stale session without a reference is unknown: %v", statuses)
	}
}

// An executor without a terminal container is never classified through a
// local board: from a checkout that still holds a board, the gc reads it
// unknown and no file of that board, its locks and control directory
// included, is created or changed.
func TestObserveExecutorsNeverReadsALocalBoard(t *testing.T) {
	resetLang(t)
	dir := tempBoard(t)
	id := todayID("gc-probe")
	if _, err := board.NewTask(dir, "chore", "gc-probe", "gc-probe", "en", false); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	before := treeState(t, dir)
	for _, window := range []string{"foreground", "console"} {
		statuses := ObserveExecutors(t.Context(), []remote.RetainedExecution{{TaskID: id, Session: "claude s-1", Window: window}})
		if len(statuses) != 1 || statuses[0] != Unknown {
			t.Fatalf("%s: %v", window, statuses)
		}
	}
	if after := treeState(t, dir); after != before {
		t.Fatalf("the board changed:\n%s\n%s", before, after)
	}
}

// treeState lists every path below dir with the size of each file.
func treeState(t *testing.T, dir string) string {
	t.Helper()
	var out strings.Builder
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(&out, "%s %d\n", path, info.Size())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out.String()
}
