package worker

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/remote"
)

// serveUntilHeld starts a worker for home and waits until it holds worker.lock.
func serveUntilHeld(t *testing.T, ctx context.Context, home string) <-chan error {
	t.Helper()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, Options{Home: home, Tick: 10 * time.Millisecond, IdleExit: time.Hour, Logger: quiet})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		held, err := remote.WorkerLockHeld(home)
		if err != nil {
			t.Fatal(err)
		}
		if held {
			return done
		}
		if time.Now().After(deadline) {
			t.Fatal("the worker never took the lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWorkerExitsOnAStopRequest(t *testing.T) {
	home := t.TempDir()
	done := serveUntilHeld(t, t.Context(), home)
	if err := remote.WriteWorkerStopRequest(home, time.Now()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the worker did not exit normally: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the worker did not exit on the stop request")
	}
	if held, err := remote.WorkerLockHeld(home); err != nil || held {
		t.Fatalf("the worker kept its lock: held=%t err=%v", held, err)
	}
	if _, found, err := remote.ReadWorkerStopRequest(home); err != nil || found {
		t.Fatalf("the stop request stays: found=%t err=%v", found, err)
	}
}

func TestWorkerRemovesAnOlderStopRequestAndRuns(t *testing.T) {
	home := t.TempDir()
	if err := remote.WriteWorkerStopRequest(home, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := serveUntilHeld(t, ctx, home)
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, found, err := remote.ReadWorkerStopRequest(home)
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the older stop request was not removed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if held, err := remote.WorkerLockHeld(home); err != nil || !held {
		t.Fatalf("the worker stopped on an older request: held=%t err=%v", held, err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
