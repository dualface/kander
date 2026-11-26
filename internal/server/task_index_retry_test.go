package server

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/api"
)

func TestTaskIndexWriteWakesWaitingRebuild(t *testing.T) {
	s, h := openTest(t, t.TempDir(), nil)
	t.Cleanup(func() { h.Close(); _ = s.Close() })
	p, id := prepareTodo(t, h.URL)
	backingOff := make(chan int, 1)
	s.indexRetryDelay = func(attempt int) time.Duration {
		backingOff <- attempt
		return time.Hour
	}
	reached, release := make(chan struct{}), make(chan struct{})
	releaseRebuild := sync.OnceFunc(func() { close(release) })
	defer releaseRebuild()
	var attempts, active, maxActive atomic.Int32
	var failed atomic.Bool
	s.indexFault = func(_ *project, phase string) error {
		switch phase {
		case "update":
			if !failed.Swap(true) {
				return errors.New("injected refresh failure")
			}
		case "rebuild":
			n := active.Add(1)
			defer active.Add(-1)
			for old := maxActive.Load(); old < n && !maxActive.CompareAndSwap(old, n); old = maxActive.Load() {
			}
			switch attempts.Add(1) {
			case 1:
				return errors.New("injected rebuild failure")
			case 2:
				close(reached)
				<-release
			}
		}
		return nil
	}
	assign := func(name string) {
		t.Helper()
		if _, err := p.Assign(t.Context(), "", id, api.Assign{Assignee: name}); err != nil {
			t.Fatal(err)
		}
	}
	assign("alice")
	if attempt := waitFor(t, backingOff, "rebuild backoff"); attempt != 1 {
		t.Fatalf("backoff attempt = %d, want 1", attempt)
	}
	assign("bob")
	waitFor(t, reached, "write-triggered retry before one-hour timer")
	assign("carol")
	releaseRebuild()
	s.indexWorkers.Wait()
	if got := maxActive.Load(); got != 1 {
		t.Fatalf("max concurrent rebuild attempts = %d, want 1", got)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("rebuild attempts = %d, want 2", got)
	}
}

func TestServerCloseCancelsIndexRebuildBackoff(t *testing.T) {
	s, h := openTest(t, t.TempDir(), nil)
	t.Cleanup(func() { h.Close(); _ = s.Close() })
	p, id := prepareTodo(t, h.URL)
	backingOff := make(chan struct{})
	s.indexRetryDelay = func(int) time.Duration {
		close(backingOff)
		return time.Hour
	}
	s.indexFault = func(_ *project, phase string) error {
		if phase == "update" || phase == "rebuild" {
			return errors.New("injected index failure")
		}
		return nil
	}
	if _, err := p.Assign(t.Context(), "", id, api.Assign{Assignee: "alice"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, backingOff, "rebuild backoff")
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	if err := waitFor(t, closed, "Close during rebuild backoff"); err != nil {
		t.Fatal(err)
	}
}
