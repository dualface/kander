package notify

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
)

func TestBusyDispatchCannotBorrowReplacementAuthorization(t *testing.T) {
	root, task, first := durableNotifyFixture(t, 10*time.Second)
	// Hold the first busy probe until the old intent has been replaced. The
	// following probe is ready at another tab, which would require a WINDOW write.
	script := `#!/bin/sh
log="$KANBAN_HERDR_LOG"
if [ "$1" = pane ] && [ "$2" = get ]; then
  status=idle
  if [ ! -f "$log.paused" ]; then
    status=working
    touch "$log.paused"
    while [ ! -f "$log.release" ]; do sleep 0.01; done
  fi
  printf '%s\n' "{\"result\":{\"pane\":{\"pane_id\":\"w1:p9\",\"tab_id\":\"w1:t10\",\"agent\":\"claude\",\"agent_status\":\"$status\",\"agent_session\":{\"value\":\"session-1\"}}}}"
  exit 0
fi
exit 1
`
	if err := os.WriteFile(filepath.Join(root, "fake-bin", "herdr"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	type replacement struct {
		snapshot board.Snapshot
		err      error
	}
	replaced := make(chan replacement, 1)
	go func() {
		var r replacement
		defer func() {
			if err := os.WriteFile(filepath.Join(root, "herdr.log.release"), nil, 0600); r.err == nil {
				r.err = err
			}
			replaced <- r
		}()
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(filepath.Join(root, "herdr.log.paused")); err == nil {
				break
			}
			if time.Now().After(deadline) {
				r.err = fmt.Errorf("busy probe did not pause")
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		if r.err = board.EndDispatch(root, task, first.Input.ID, first.Revision, board.DispatchCancelled, "replace old intent"); r.err != nil {
			return
		}
		input := first.Input
		input.ID = "replacement-intent"
		if _, r.err = board.PrepareDispatch(root, input); r.err != nil {
			return
		}
		r.snapshot, r.err = board.ReadSnapshot(root, task)
	}()
	_, _, err := capture(t, func() error { return deliverDispatch(root, task, first.Input.ID, "w1:p9") })
	r := <-replaced
	if r.err != nil {
		t.Fatal(r.err)
	}
	if err == nil {
		t.Fatal("superseded delivery succeeded")
	}
	after, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != r.snapshot.Revision || after.Text != r.snapshot.Text {
		t.Fatalf("old dispatch changed replacement WINDOW/revision: %d -> %d, %s -> %s", r.snapshot.Revision, after.Revision, board.MetadataFrom(r.snapshot.Text, "WINDOW"), board.MetadataFrom(after.Text, "WINDOW"))
	}
}
