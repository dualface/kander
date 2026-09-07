package liveness

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
)

func TestSubscriptionPendingExecutorExits(t *testing.T) {
	root, opts, id := factsMember(t)
	setLocation(t, currentEntry(t, root, id).Document, "codex wanted", "herdr:w1:t1:w1:p0")
	installPOSIXFakes(t, true)
	marker := filepath.Join(t.TempDir(), "exited")
	t.Setenv("SUBSCRIPTION_DISPATCH_EXITED", marker)
	script := `#!/bin/sh
if [ -f "$SUBSCRIPTION_DISPATCH_EXITED" ]; then
 if [ "$2" = list ]; then echo '{"result":{"panes":[]}}'; else echo '{"id":"cli:pane:get","error":{"code":"pane_not_found","message":"gone"}}' >&2; exit 1; fi
else
 echo '{"result":{"pane":{"pane_id":"w1:p0","agent":"codex","agent_status":"working","agent_session":{"value":"wanted"}}}}'
fi
`
	if err := os.WriteFile(filepath.Join(os.Getenv("PATH"), "herdr"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	dispatch := prepareSubscriptionDispatch(t, root, id, "sync", 250*time.Millisecond)
	opts.Refresh, opts.Heartbeat = .005, .03
	events := make(runtimeEvents, 64)
	cancel, done := runRuntimeSubscription(t, root, opts, events)
	alive := nextRuntimeEvent(t, events, func(e groupEvent) bool { return e.Event == "heartbeat" && e.Liveness[id].Status == Alive })
	if err := os.WriteFile(marker, []byte("exited"), 0600); err != nil {
		t.Fatal(err)
	}
	attention := nextRuntimeEvent(t, events, func(e groupEvent) bool { return e.Event == "dispatch-attention" && e.Liveness[id].Status == Stopped })
	if attention.Tasks[id] != "review" || attention.Dispatches[id].Accepted != nil || !attention.Dispatches[id].ConfirmationOverdue || !attention.Liveness[id].ObservedAt.After(*alive.Liveness[id].ObservedAt) {
		t.Fatalf("exit confused with acceptance: %+v", attention)
	}
	cancel()
	<-done
	actual, err := board.ReadDispatch(root, id, dispatch.Input.ID)
	if err != nil || actual.State != board.DispatchPrepared || actual.Attempts != 0 {
		t.Fatalf("executor automatically recovered: %+v %v", actual, err)
	}
}

func TestSubscriptionDispatchDeadlineDoesNotWaitForProbe(t *testing.T) {
	root, opts, id := factsMember(t)
	setLocation(t, currentEntry(t, root, id).Document, "codex wanted", "herdr:w1:t1:w1:p0")
	dir := installBatchFake(t, false)
	prepareSubscriptionDispatch(t, root, id, "sync", 100*time.Millisecond)
	opts.Refresh, opts.Heartbeat = 3600, 3600
	events := make(runtimeEvents, 16)
	cancel, done := runRuntimeSubscription(t, root, opts, events)
	attention := nextRuntimeEvent(t, events, func(e groupEvent) bool { return e.Event == "dispatch-attention" })
	if !attention.Liveness[id].Collecting || attention.Liveness[id].Status != Unknown || !attention.Dispatches[id].ConfirmationOverdue {
		t.Fatalf("slow probe hid deadline: %+v", attention)
	}
	cancel()
	<-done
	assertBatchPeakAndCleanup(t, dir, 1, 1)
}

func TestSubscriptionAcceptanceEndsConfirmationDeadline(t *testing.T) {
	root, opts, id := factsMember(t)
	dispatch := prepareSubscriptionDispatch(t, root, id, "sync", time.Minute)
	if _, err := board.MoveWithOptions(currentEntry(t, root, id), root, "working", board.MoveOptions{Authorization: dispatch.Authorization}); err != nil {
		t.Fatal(err)
	}
	// Move only the observation clock beyond the immutable deadline; acceptance
	// already persisted and must not become a synthetic completion timeout.
	oldNow := nowFn
	nowFn = func() time.Time { return time.Now().Add(2 * time.Minute) }
	t.Cleanup(func() { nowFn = oldNow })
	events, err := factsEvents(t, root, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		fact := event.Dispatches[id]
		if fact.State != board.DispatchAccepted || fact.Accepted == nil || fact.Completed != nil || fact.ConfirmationPending || fact.ConfirmationOverdue || event.Event == "dispatch-attention" {
			t.Fatalf("accepted work assigned a completion deadline: %+v", event)
		}
	}
}
