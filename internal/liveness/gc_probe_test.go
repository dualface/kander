package liveness

import (
	"testing"

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
