package liveness

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// contentionStep scripts one read: a lock wait timeout that first advances the
// fake clock by advance, or a real read when timeout is false.
type contentionStep struct {
	timeout bool
	advance time.Duration
}

func readOK() contentionStep { return contentionStep{} }

func lockTimeout(advance time.Duration) contentionStep {
	return contentionStep{timeout: true, advance: advance}
}

type contentionScript struct {
	mu     sync.Mutex
	clock  time.Time
	steps  []contentionStep
	rest   contentionStep
	calls  int
	real   []time.Time
	onCall func(call int)
}

func (c *contentionScript) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.clock
}

func (c *contentionScript) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// installContention replaces the clock and the read seam for one test.
func installContention(t *testing.T, script *contentionScript) {
	t.Helper()
	script.clock = time.Now()
	oldNow, oldRead := nowFn, readSubscriptionFacts
	nowFn = script.now
	readSubscriptionFacts = func(s *subscription, ctx context.Context, root string) (subscriptionFacts, error) {
		script.mu.Lock()
		script.calls++
		call := script.calls
		script.real = append(script.real, time.Now())
		step := script.rest
		if call <= len(script.steps) {
			step = script.steps[call-1]
		}
		if step.timeout {
			script.clock = script.clock.Add(step.advance)
		}
		hook := script.onCall
		script.mu.Unlock()
		if hook != nil {
			hook(call)
		}
		if step.timeout {
			return subscriptionFacts{observedAt: script.now().UTC()}, fmt.Errorf("board lock: %w", context.DeadlineExceeded)
		}
		return oldRead(s, ctx, root)
	}
	t.Cleanup(func() { nowFn, readSubscriptionFacts = oldNow, oldRead })
}

func noReadError(t *testing.T, events runtimeEvents) {
	t.Helper()
	for {
		select {
		case event := <-events:
			if event.Event == "read-error" || event.Event == "membership-unknown" {
				t.Fatalf("terminal event during short contention: %+v", event)
			}
		default:
			return
		}
	}
}

func TestSubscribeShortContentionKeepsSnapshot(t *testing.T) {
	root, opts, id := factsMember(t)
	updated := make(chan error, 1)
	script := &contentionScript{steps: []contentionStep{readOK(), lockTimeout(10 * time.Second), lockTimeout(10 * time.Second), lockTimeout(10 * time.Second)}}
	script.onCall = func(call int) {
		if call == 2 {
			updated <- factsUpdate(root, id, func(text string) string { return text + "\nchanged during contention\n" })
		}
	}
	installContention(t, script)
	events := make(runtimeEvents, 64)
	cancel, done := runRuntimeSubscription(t, root, opts, events)
	snapshot := nextRuntimeEvent(t, events, func(e groupEvent) bool { return true })
	if snapshot.Event != "snapshot" {
		t.Fatalf("first event: %+v", snapshot)
	}
	if err := <-updated; err != nil {
		t.Fatal(err)
	}
	update := nextRuntimeEvent(t, events, func(e groupEvent) bool { return true })
	if update.Event != "task-update" || update.Seq != 2 || len(update.Updated) != 1 || update.Updated[0] != id || update.TaskRevisions[id] == snapshot.TaskRevisions[id] {
		t.Fatalf("change after contention compared with the wrong snapshot: %+v", update)
	}
	if calls := script.callCount(); calls < 5 {
		t.Fatalf("reads=%d", calls)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	noReadError(t, events)
}

func TestSubscribeContentionLimitTerminates(t *testing.T) {
	root, opts, id := factsMember(t)
	script := &contentionScript{steps: []contentionStep{readOK(), lockTimeout(15 * time.Second), lockTimeout(15 * time.Second), lockTimeout(15 * time.Second)}, rest: readOK()}
	installContention(t, script)
	events := make(runtimeEvents, 64)
	_, done := runRuntimeSubscription(t, root, opts, events)
	snapshot := nextRuntimeEvent(t, events, func(e groupEvent) bool { return true })
	failure := nextRuntimeEvent(t, events, func(e groupEvent) bool { return true })
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("exit=%v", err)
	}
	if failure.Event != "read-error" || failure.Seq != 2 || failure.ReadStatus != "maintenance" || failure.MembershipComplete || !failure.ReconciliationRequired || failure.Tasks[id] != snapshot.Tasks[id] || !strings.Contains(failure.Detail, context.DeadlineExceeded.Error()) {
		t.Fatalf("terminal event: %+v", failure)
	}
	if calls := script.callCount(); calls != 4 {
		t.Fatalf("reads=%d; want the third timeout, 30s after the first, to terminate", calls)
	}
}

func TestSubscribeSuccessfulReadClearsContentionStart(t *testing.T) {
	root, opts, _ := factsMember(t)
	reached := make(chan struct{})
	script := &contentionScript{steps: []contentionStep{
		readOK(), lockTimeout(20 * time.Second), lockTimeout(20 * time.Second), readOK(),
		lockTimeout(20 * time.Second), lockTimeout(20 * time.Second), readOK(),
	}}
	script.onCall = func(call int) {
		if call == 8 {
			close(reached)
		}
	}
	installContention(t, script)
	events := make(runtimeEvents, 64)
	cancel, done := runRuntimeSubscription(t, root, opts, events)
	select {
	case <-reached:
	case err := <-done:
		t.Fatalf("subscription ended: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("reads stalled")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	noReadError(t, events)
}

func TestSubscribeInitialSnapshotContention(t *testing.T) {
	t.Run("retry", func(t *testing.T) {
		root, opts, id := factsMember(t)
		installContention(t, &contentionScript{steps: []contentionStep{lockTimeout(10 * time.Second), lockTimeout(10 * time.Second)}})
		events := make(runtimeEvents, 64)
		cancel, done := runRuntimeSubscription(t, root, opts, events)
		snapshot := nextRuntimeEvent(t, events, func(e groupEvent) bool { return true })
		if snapshot.Event != "snapshot" || snapshot.Seq != 1 || snapshot.Tasks[id] != "review" {
			t.Fatalf("first event: %+v", snapshot)
		}
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel=%v", err)
		}
	})
	t.Run("limit", func(t *testing.T) {
		root, opts, _ := factsMember(t)
		installContention(t, &contentionScript{rest: lockTimeout(15 * time.Second)})
		events := make(runtimeEvents, 64)
		_, done := runRuntimeSubscription(t, root, opts, events)
		failure := nextRuntimeEvent(t, events, func(e groupEvent) bool { return true })
		if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("exit=%v", err)
		}
		if failure.Event != "read-error" || failure.Seq != 1 || failure.ReadStatus != "maintenance" || len(failure.Tasks) != 0 {
			t.Fatalf("terminal event: %+v", failure)
		}
	})
}

func TestSubscribeContentionRetryWaitsRefresh(t *testing.T) {
	root, opts, _ := factsMember(t)
	opts.Refresh, opts.Heartbeat = .15, .001
	script := &contentionScript{steps: []contentionStep{readOK(), lockTimeout(time.Second), lockTimeout(time.Second), lockTimeout(time.Second)}}
	installContention(t, script)
	events := make(runtimeEvents, 64)
	cancel, done := runRuntimeSubscription(t, root, opts, events)
	snapshot := nextRuntimeEvent(t, events, func(e groupEvent) bool { return true })
	next := nextRuntimeEvent(t, events, func(e groupEvent) bool { return true })
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	// The heartbeat was due during the retries; it follows the next good read.
	if next.Event != "heartbeat" || next.Seq != 2 || next.ObservedAt.Sub(snapshot.ObservedAt) < 3*time.Second {
		t.Fatalf("event during contention: %+v", next)
	}
	script.mu.Lock()
	defer script.mu.Unlock()
	for call := 2; call < 5; call++ {
		if gap := script.real[call].Sub(script.real[call-1]); gap < 150*time.Millisecond {
			t.Fatalf("retry %d after %s; want the full refresh", call, gap)
		}
	}
}

func TestSubscribeCancelDuringContentionRetry(t *testing.T) {
	root, opts, _ := factsMember(t)
	opts.Refresh = 3600
	retrying := make(chan struct{})
	script := &contentionScript{steps: []contentionStep{readOK()}, rest: lockTimeout(time.Second)}
	script.onCall = func(call int) {
		if call == 2 {
			close(retrying)
		}
	}
	installContention(t, script)
	events := make(runtimeEvents, 64)
	cancel, done := runRuntimeSubscription(t, root, opts, events)
	nextRuntimeEvent(t, events, func(e groupEvent) bool { return e.Event == "snapshot" })
	<-retrying
	// The scripted timeout returns at once; give it time to enter the wait.
	time.Sleep(50 * time.Millisecond)
	started := time.Now()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	if time.Since(started) > 400*time.Millisecond {
		t.Fatal("cancellation waited for the retry interval")
	}
	if calls := script.callCount(); calls != 2 {
		t.Fatalf("reads=%d", calls)
	}
	select {
	case event := <-events:
		t.Fatalf("event after cancellation: %+v", event)
	default:
	}
}
