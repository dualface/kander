//go:build !windows

package liveness

import (
	"strconv"
	"testing"
)

func TestSubscriptionDoesNotOverlapProbeBatches(t *testing.T) {
	root, opts, id := factsMember(t)
	moveRuntimeWorking(t, root, id)
	setLocation(t, currentEntry(t, root, id).Document, "codex wanted", "herdr:w1:t1:w1:p0")
	for n := 1; n < 8; n++ {
		task, path := makeWorking(t, "runtime-batch-"+strconv.Itoa(n), "Worker")
		setTaskGroup(t, path, opts.Group)
		setLocation(t, path, "codex wanted", "herdr:w1:t1:w1:p"+strconv.Itoa(n))
		opts.Members = append(opts.Members, task)
	}
	dir := installBatchFake(t, false)
	events := make(runtimeEvents, 64)
	cancel, done := runRuntimeSubscription(t, root, opts, events)
	// Several heartbeat deadlines pass while the first four workers stay slow.
	heartbeats := 0
	nextRuntimeEvent(t, events, func(e groupEvent) bool {
		if e.Event == "heartbeat" {
			heartbeats++
		}
		return heartbeats == 4
	})
	cancel()
	<-done
	assertBatchPeakAndCleanup(t, dir, DefaultBatchConcurrency, DefaultBatchConcurrency)
}
