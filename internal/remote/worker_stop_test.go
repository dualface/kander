package remote

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/fs"
)

// takeWorkerLock takes worker.lock of home the way a running worker does and returns its release.
func takeWorkerLock(t *testing.T, home string) func() {
	t.Helper()
	file, err := fs.OpenLockFile(home, filepath.Join(home, WorkerLockName))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := fs.LockExclusive(file)
	if err != nil {
		t.Fatal(err)
	}
	return func() {
		_ = lock.Unlock()
		_ = file.Close()
	}
}

func TestStopWorkerWithoutAWorkerWritesNothing(t *testing.T) {
	home := t.TempDir()
	stopped, err := StopWorker(t.Context(), home, time.Millisecond, time.Second)
	if err != nil || stopped {
		t.Fatalf("stopped=%t err=%v", stopped, err)
	}
	for _, name := range []string{WorkerStopName, WorkerLockName} {
		if _, err := os.Lstat(filepath.Join(home, name)); !os.IsNotExist(err) {
			t.Fatalf("%s was created: %v", name, err)
		}
	}
}

func TestStopWorkerWaitsForTheLock(t *testing.T) {
	home := t.TempDir()
	release := takeWorkerLock(t, home)
	// The holder answers the request the way a worker does: it releases the lock.
	go func() {
		for {
			if _, found, _ := ReadWorkerStopRequest(home); found {
				release()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	stopped, err := StopWorker(t.Context(), home, 5*time.Millisecond, 5*time.Second)
	if err != nil || !stopped {
		t.Fatalf("stopped=%t err=%v", stopped, err)
	}
	if _, found, err := ReadWorkerStopRequest(home); err != nil || found {
		t.Fatalf("the request stays after the lock was freed: found=%t err=%v", found, err)
	}
}

func TestStopWorkerTimesOutAndKeepsTheRequest(t *testing.T) {
	home := t.TempDir()
	release := takeWorkerLock(t, home)
	defer release()
	_, err := StopWorker(t.Context(), home, 5*time.Millisecond, 50*time.Millisecond)
	if !errors.Is(err, ErrWorkerStopTimeout) {
		t.Fatalf("err=%v", err)
	}
	if _, found, err := ReadWorkerStopRequest(home); err != nil || !found {
		t.Fatalf("the request is gone after a timeout: found=%t err=%v", found, err)
	}
}
