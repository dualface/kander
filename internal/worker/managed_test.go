package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/process"
	"github.com/dualface/kander/v2/internal/remote"
)

func TestManagedOptionsExitOnHandoverOutsideWindows(t *testing.T) {
	for _, c := range []struct {
		managed      bool
		goos         string
		exitHandover bool
	}{{true, "linux", true}, {true, "darwin", true}, {true, "windows", false}, {false, "linux", false}} {
		o := managedOptions(Options{}, c.managed, c.goos)
		if o.Managed != c.managed || o.ExitOnHandover != c.exitHandover {
			t.Fatalf("%+v: %+v", c, o)
		}
	}
}

// A worker without claims exits after the idle time unless it is managed: a service worker
// stays for the starts directed at its host.
func TestOnlyAnUnmanagedWorkerExitsWhenIdle(t *testing.T) {
	r := newRig(t)
	idle := r.options()
	idle.IdleExit = time.Millisecond
	idle.Now = time.Now
	done := make(chan error, 1)
	go func() { done <- Serve(t.Context(), idle) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("an unmanaged worker without claims did not exit")
	}

	managed := r.options()
	managed.IdleExit, managed.Now, managed.Managed = time.Millisecond, time.Now, true
	ctx, cancel := context.WithCancel(t.Context())
	done = make(chan error, 1)
	go func() { done <- Serve(ctx, managed) }()
	r.ticks(20)
	select {
	case err := <-done:
		t.Fatalf("a managed worker exited without claims: %v", err)
	default:
	}
	if !readRecord(t, r.home).Managed {
		t.Fatal("worker.json does not say the worker is managed")
	}
	finish(t, done, cancel)
}

// A managed worker on Linux and macOS leaves the start of the replaced binary to its service
// manager: it releases the lock and exits without starting a successor.
func TestManagedWorkerExitsOnHandoverWithoutStartingASuccessor(t *testing.T) {
	r := newRig(t)
	r.claimed("managed-handover")
	opts := r.options()
	opts.Managed, opts.ExitOnHandover = true, true
	done := make(chan error, 1)
	go func() { done <- Serve(t.Context(), opts) }()
	waitFor(t, "the worker start", func() bool { return r.sink.find("worker started") != nil })
	r.writeExecutable("v2")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the exit for a replaced binary ends Serve with nil: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the managed worker did not exit for its replaced binary")
	}
	if r.spawns.Load() != 0 || r.probes.Load() != 1 {
		t.Fatalf("probes=%d spawns=%d, want 1 and 0", r.probes.Load(), r.spawns.Load())
	}
	if held, err := remote.WorkerHeld(r.home); err != nil || held {
		t.Fatalf("worker.lock must be free after the exit: %v %v", held, err)
	}
	if r.sink.find("worker exits for its service manager to start the replaced binary") == nil {
		t.Fatal("the exit is not logged")
	}
}

// The successor of a managed worker on Windows stays managed; any other detached worker never
// inherits KANDER_MANAGED.
func TestSuccessorOfAManagedWorkerStaysManaged(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(process.EnvManaged, "1")
	for _, managed := range []bool{true, false} {
		home := t.TempDir()
		out := filepath.Join(t.TempDir(), "env")
		t.Setenv(probeModeEnv, "env")
		t.Setenv(probePIDFileEnv, out)
		p, err := startSuccessor(home, self, managed)
		if err != nil {
			t.Fatal(err)
		}
		waitFor(t, "the successor", func() bool { return p.Exited() })
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		want := ""
		if managed {
			want = "1"
		}
		if got := strings.TrimSpace(string(data)); got != want {
			t.Fatalf("managed=%v: the successor saw KANDER_MANAGED=%q", managed, got)
		}
	}
}
