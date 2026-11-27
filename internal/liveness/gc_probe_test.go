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
