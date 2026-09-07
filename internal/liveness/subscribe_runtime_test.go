package liveness

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
)

type runtimeEvents chan groupEvent

func (w runtimeEvents) Write(p []byte) (int, error) { return w.WriteContext(context.Background(), p) }
func (w runtimeEvents) WriteContext(ctx context.Context, p []byte) (int, error) {
	var event groupEvent
	if err := json.Unmarshal(p, &event); err != nil {
		return 0, err
	}
	select {
	case w <- event:
		return len(p), nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func nextRuntimeEvent(t *testing.T, events runtimeEvents, match func(groupEvent) bool) groupEvent {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-events:
			if match(event) {
				return event
			}
		case <-timer.C:
			t.Fatal("subscription event deadline")
			return groupEvent{}
		}
	}
}

func runRuntimeSubscription(t *testing.T, root string, opts subscribeOptions, w io.Writer) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	joined := make(chan struct{})
	go func() { done <- SubscribeContext(ctx, root, opts, w); close(joined) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-joined:
		case <-time.After(3 * time.Second):
			t.Error("subscription resources did not join")
		}
	})
	return cancel, done
}

// Reverse the historical audit: a real slow probe cannot hold the scan or stop.
func TestAuditProbeBlocksScanAndCancellation(t *testing.T) {
	root, opts, id := factsMember(t)
	if _, err := board.MoveEntry(currentEntry(t, root, id), root, "working"); err != nil {
		t.Fatal(err)
	}
	setLocation(t, currentEntry(t, root, id).Document, "codex wanted", "herdr:w1:t1:w1:p0")
	dir := installBatchFake(t, false)
	events := make(runtimeEvents, 32)
	cancel, done := runRuntimeSubscription(t, root, opts, events)
	nextRuntimeEvent(t, events, func(e groupEvent) bool { return e.Event == "snapshot" })
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "w1:p0")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("probe never started")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := board.MoveEntry(currentEntry(t, root, id), root, "review"); err != nil {
		t.Fatal(err)
	}
	event := nextRuntimeEvent(t, events, func(e groupEvent) bool { return e.Event == "state-change" })
	if event.Tasks[id] != "review" {
		t.Fatalf("state=%+v", event)
	}
	started := time.Now()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	if time.Since(started) > 400*time.Millisecond {
		t.Fatal("cancellation waited for slow probe")
	}
	assertBatchPeakAndCleanup(t, dir, 1, 1)
}

func TestSubscribeChangesStillObserveAgentDeath(t *testing.T) {
	root, opts, id := factsMember(t)
	deadID, path := makeWorking(t, "runtime-death", "Agent")
	setTaskGroup(t, path, opts.Group)
	setLocation(t, path, "codex wanted", "herdr:w1:t1:w1:p0")
	opts.Members = append(opts.Members, deadID)
	opts.Heartbeat = .06
	installPOSIXFakes(t, true)
	marker := filepath.Join(t.TempDir(), "dead")
	t.Setenv("RUNTIME_DEAD", marker)
	script := `#!/bin/sh
if [ -f "$RUNTIME_DEAD" ]; then
 if [ "$2" = list ]; then echo '{"result":{"panes":[]}}'; else echo '{"id":"cli:pane:get","error":{"code":"pane_not_found","message":"gone"}}' >&2; exit 1; fi
else
 echo '{"result":{"pane":{"pane_id":"w1:p0","agent":"codex","agent_status":"working","agent_session":{"value":"wanted"}}}}'
fi
`
	if err := os.WriteFile(filepath.Join(os.Getenv("PATH"), "herdr"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	events := make(runtimeEvents, 64)
	cancel, done := runRuntimeSubscription(t, root, opts, events)
	alive := nextRuntimeEvent(t, events, func(e groupEvent) bool { return e.Event == "heartbeat" && e.Liveness[deadID].Status == Alive })
	if alive.Liveness[deadID].ObservedAt == nil || alive.Liveness[deadID].RuntimeState != "working" {
		t.Fatalf("observation=%+v", alive)
	}
	if err := os.WriteFile(marker, []byte("dead"), 0600); err != nil {
		t.Fatal(err)
	}
	// Continue changing one card while the other agent disappears.
	updates := make(chan struct{})
	updateErr := make(chan error, 1)
	go func() {
		defer close(updates)
		timer := time.NewTicker(8 * time.Millisecond)
		defer timer.Stop()
		for {
			select {
			case <-done:
				return
			case <-timer.C:
				if err := factsUpdate(root, id, func(s string) string { return s + "\nChanged.\n" }); err != nil {
					updateErr <- err
					return
				}
			}
		}
	}()
	stopped := nextRuntimeEvent(t, events, func(e groupEvent) bool { return e.Event == "heartbeat" && e.Liveness[deadID].Status == Stopped })
	if stopped.Liveness[deadID].ObservedAt == nil || !stopped.Liveness[deadID].ObservedAt.After(*alive.Liveness[deadID].ObservedAt) {
		t.Fatalf("no new death observation: %+v", stopped)
	}
	cancel()
	<-updates
	select {
	case err := <-updateErr:
		t.Fatal(err)
	default:
	}
}

func TestSubscriptionRejectsSupersededObservations(t *testing.T) {
	root, opts, id := factsMember(t)
	if _, err := board.MoveEntry(currentEntry(t, root, id), root, "working"); err != nil {
		t.Fatal(err)
	}
	s, err := newSubscription(opts)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := s.readContext(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	text, _ := facts.scanned.Document(id)
	p := newSubscriptionProbes(context.Background())
	defer p.finish()
	report := Report{TaskID: id, Agent: "codex", Status: Alive, RuntimeState: "working", ObservationValid: true, ObservedAt: time.Now(), Identity: identityFrom(facts.scanned.Entries[id], text)}
	cache := func() { p.cached[id] = subscriptionObservation{revision: facts.revisions[id], report: report} }
	cache()
	if live := p.liveness(facts, time.Second)[id]; !live.ObservationValid || live.ObservedAt == nil {
		t.Fatalf("valid report lost: %+v", live)
	}
	report.ObservedAt = time.Now().Add(-time.Minute)
	cache()
	if live := p.liveness(facts, time.Second)[id]; !live.Stale || live.Status != Unknown || live.ObservationValid {
		t.Fatalf("expired alive report reused: %+v", live)
	}
	report.ObservedAt = time.Now()
	cache()
	if err := factsUpdate(root, id, func(text string) string { return text + "\nUpdated.\n" }); err != nil {
		t.Fatal(err)
	}
	current, err := s.readContext(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if live := p.liveness(current, time.Second)[id]; live.Status != Unknown || live.ObservedAt != nil {
		t.Fatalf("old revision reused: %+v", live)
	}
	facts = current
	cache()
	report.Identity.Session = "old-session"
	cache()
	if live := p.liveness(current, time.Second)[id]; live.Status != Unknown || live.ObservedAt != nil {
		t.Fatalf("old session reused: %+v", live)
	}
}

type waitingWriter struct {
	started  chan struct{}
	returned chan struct{}
}

func (w *waitingWriter) Write([]byte) (int, error) { panic("uncancellable Write called") }
func (w *waitingWriter) WriteContext(ctx context.Context, p []byte) (int, error) {
	close(w.started)
	defer close(w.returned)
	<-ctx.Done()
	return 0, ctx.Err()
}

type unsupportedWriter struct{ called bool }

func (w *unsupportedWriter) Write(p []byte) (int, error) { w.called = true; return len(p), nil }

func TestSubscriptionOutputBounds(t *testing.T) {
	t.Run("queue-full", func(t *testing.T) {
		writer := &waitingWriter{started: make(chan struct{}), returned: make(chan struct{})}
		q := newSubscriptionOutput(context.Background(), writer)
		if _, err := q.Write([]byte("first\n")); err != nil {
			t.Fatal(err)
		}
		<-writer.started
		for n := 0; n < subscriptionQueueSize; n++ {
			if _, err := q.Write([]byte("next\n")); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := q.Write([]byte("overflow\n")); err == nil || !strings.Contains(err.Error(), "16") {
			t.Fatalf("overflow=%v", err)
		}
		if err := q.finish(); !errors.Is(err, context.Canceled) {
			t.Fatalf("join=%v", err)
		}
		select {
		case <-writer.returned:
		default:
			t.Fatal("writer survived exit")
		}
	})
	t.Run("line-limit", func(t *testing.T) {
		q := newSubscriptionOutput(context.Background(), contextWriteFunc(func(ctx context.Context, p []byte) (int, error) { return len(p), nil }))
		if _, err := q.Write(make([]byte, subscriptionMaxLine+1)); err == nil {
			t.Fatal("unbounded line accepted")
		}
		if err := q.finish(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("unsupported", func(t *testing.T) {
		root, opts, _ := factsMember(t)
		writer := &unsupportedWriter{}
		if err := SubscribeContext(context.Background(), root, opts, writer); err == nil || writer.called {
			t.Fatalf("unsupported writer invoked: %v", err)
		}
	})
	t.Run("deadline-includes-queue", func(t *testing.T) {
		writer := &waitingWriter{started: make(chan struct{}), returned: make(chan struct{})}
		q := newSubscriptionOutput(context.Background(), writer)
		q.lines <- queuedLine{data: []byte("expired"), deadline: time.Now().Add(-time.Second)}
		if err := q.finish(); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("deadline=%v", err)
		}
		select {
		case <-writer.returned:
		default:
			t.Fatal("writer survived deadline")
		}
	})
}

func moveRuntimeWorking(t *testing.T, root, id string) {
	t.Helper()
	if _, err := board.MoveEntry(currentEntry(t, root, id), root, "working"); err != nil {
		t.Fatal(err)
	}
}

func TestSubscribeQueueOverflowIsNotSuccessfulCancellation(t *testing.T) {
	root, opts, _ := factsMember(t)
	opts.Heartbeat = .001
	writer := &waitingWriter{started: make(chan struct{}), returned: make(chan struct{})}
	started := time.Now()
	err := Subscribe(root, opts, writer, make(chan struct{}))
	if err == nil || !strings.Contains(err.Error(), "16") {
		t.Fatalf("queue overflow masked as successful stop: %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("queue overflow failed to cancel output")
	}
	select {
	case <-writer.returned:
	default:
		t.Fatal("writer survived overflow")
	}
}
