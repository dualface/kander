package remote

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/internal/version"
)

// holdWorkerLock stands in for a running worker: it holds worker.lock until
// the test ends.
func holdWorkerLock(t *testing.T, home string) {
	t.Helper()
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := fs.OpenLockFile(home, filepath.Join(home, WorkerLockName))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lock, err := fs.LockExclusiveContext(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Unlock(); _ = file.Close() })
}

func englishText(t *testing.T) {
	t.Helper()
	t.Setenv(config.EnvLangCLI, "1")
	t.Setenv(config.EnvLang, "en")
}

func writeRecord(t *testing.T, home string, r WorkerRecord) {
	t.Helper()
	if r.StartedAt.IsZero() {
		r.StartedAt = time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)
	}
	if err := WriteWorkerRecord(home, r); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureWorkerStartsNoticeFreeWhenNoWorkerHoldsTheLock(t *testing.T) {
	englishText(t)
	home := t.TempDir()
	// A record that an earlier worker left must not be read: the worker that
	// starts now is this binary.
	writeRecord(t, home, WorkerRecord{Version: "v-stale", PID: 9, Handover: true})
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var started []string
	notice, err := ensureWorker(home, func(h, executable string) error {
		started = append(started, h, executable)
		return nil
	})
	if err != nil || notice != "" {
		t.Fatalf("notice %q, error %v", notice, err)
	}
	if len(started) != 2 || started[0] != home || started[1] != self {
		t.Fatalf("one start of this binary for the home: %v", started)
	}
}

func TestEnsureWorkerNoticeForARunningWorker(t *testing.T) {
	englishText(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	const other = "/elsewhere/kander"
	for _, tc := range []struct {
		name     string
		record   *WorkerRecord
		raw      string
		contains []string
		absent   []string
	}{
		{name: "the same version", record: &WorkerRecord{Version: version.String(), PID: 11, Executable: other, Handover: true}},
		{name: "another version at this path with handover",
			record: &WorkerRecord{Version: "v9.9.9", PID: 4711, Executable: self, Handover: true},
			contains: []string{"kander v9.9.9", "kander " + version.String(), "pid 4711", self,
				"hands over to this binary on its next round", "after a minute, stop process 4711", "Until the next kander start, server claim or kander coordinator claim starts a worker"},
			absent: []string{"does not hand over"}},
		{name: "another version at another path",
			record: &WorkerRecord{Version: "v9.9.9", PID: 4711, Executable: other, Handover: true},
			contains: []string{"kander v9.9.9", "kander " + version.String(), "pid 4711", other, self,
				"does not hand over to this binary", "Stop process 4711", "the worker started next runs this binary", "Until the next kander start, server claim or kander coordinator claim starts a worker"},
			absent: []string{"on its next round"}},
		{name: "another version without handover at this path",
			record:   &WorkerRecord{Version: "v9.9.9", PID: 4711, Executable: self, Handover: false},
			contains: []string{"does not hand over to this binary", "Stop process 4711", "Until the next kander start, server claim or kander coordinator claim starts a worker"},
			absent:   []string{"on its next round"}},
		{name: "no record", contains: []string{"no version record", "older worker", "Until the next kander start, server claim or kander coordinator claim starts a worker"}},
		{name: "a record of another schema", raw: `{"schema":2,"version":"v9"}`},
		{name: "a record that is not JSON", raw: `not json`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			holdWorkerLock(t, home)
			switch {
			case tc.record != nil:
				writeRecord(t, home, *tc.record)
			case tc.raw != "":
				if err := os.WriteFile(filepath.Join(home, WorkerRecordName), []byte(tc.raw), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			notice, err := ensureWorker(home, func(string, string) error {
				t.Fatal("a running worker is not started again")
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(tc.contains) == 0 && notice != "" {
				t.Fatalf("no notice expected, got %q", notice)
			}
			for _, want := range tc.contains {
				if !strings.Contains(notice, want) {
					t.Errorf("notice lacks %q:\n%s", want, notice)
				}
			}
			for _, bad := range tc.absent {
				if strings.Contains(notice, bad) {
					t.Errorf("notice must not contain %q:\n%s", bad, notice)
				}
			}
		})
	}
}

func TestInspectWorkerReadsWithoutCreatingAnything(t *testing.T) {
	t.Run("no client directory", func(t *testing.T) {
		home := filepath.Join(t.TempDir(), "client")
		status, err := InspectWorker(home)
		if err != nil || status.Run != WorkerNoHome {
			t.Fatalf("status %+v, error %v", status, err)
		}
		if _, err := os.Stat(home); !os.IsNotExist(err) {
			t.Fatalf("the client directory must not be created: %v", err)
		}
	})
	t.Run("no lock file", func(t *testing.T) {
		home := t.TempDir()
		status, err := InspectWorker(home)
		if err != nil || status.Run != WorkerStopped {
			t.Fatalf("status %+v, error %v", status, err)
		}
		if entries, _ := os.ReadDir(home); len(entries) != 0 {
			t.Fatalf("the client directory stays empty: %v", entries)
		}
	})
	t.Run("lock file without a holder", func(t *testing.T) {
		home := t.TempDir()
		writeRecord(t, home, WorkerRecord{Version: "v-stale", PID: 5})
		if err := os.WriteFile(filepath.Join(home, WorkerLockName), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		status, err := InspectWorker(home)
		if err != nil || status.Run != WorkerStopped || status.Record.Version != "" {
			t.Fatalf("a record is only read while a worker holds the lock: %+v, error %v", status, err)
		}
	})
	t.Run("held", func(t *testing.T) {
		home := t.TempDir()
		holdWorkerLock(t, home)
		writeRecord(t, home, WorkerRecord{Version: "v9.9.9", PID: 77})
		status, err := InspectWorker(home)
		if err != nil || status.Run != WorkerRunning || status.RecordState != WorkerRecordRecorded || status.Record.PID != 77 {
			t.Fatalf("status %+v, error %v", status, err)
		}
		if got := status.Finding("v9.9.9"); got != WorkerFindingNone {
			t.Fatalf("the same version has no finding: %v", got)
		}
		if got := status.Finding("v1"); got != WorkerFindingMismatch {
			t.Fatalf("another version is a mismatch: %v", got)
		}
	})
}

// The window between "the lock file exists" and "the lock file is opened" is
// microseconds wide, so this test covers it only by chance. The deterministic
// evidence is that InspectWorker has no path that creates the lock file.
func TestInspectWorkerNeverCreatesTheLockFile(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("a Windows handle does not share DELETE, so the concurrent remove fails by design")
	}
	home := t.TempDir()
	lockPath := filepath.Join(home, WorkerLockName)
	if err := os.WriteFile(lockPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	inspected := make(chan error, 1)
	go func() {
		for {
			select {
			case <-stop:
				inspected <- nil
				return
			default:
			}
			if status, err := InspectWorker(home); err != nil || status.Run != WorkerStopped {
				inspected <- fmt.Errorf("status %+v, error %v", status, err)
				return
			}
		}
	}()
	time.Sleep(5 * time.Millisecond)
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	close(stop)
	if err := <-inspected; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("the probe must not bring the lock file back: %v", err)
	}
}

// Runs on every platform; the name keeps it in the Windows CI step that runs
// only TestWindows* of this package.
func TestWindowsInspectWorkerSeesAHeldLock(t *testing.T) {
	home := t.TempDir()
	file, err := fs.OpenLockFile(home, filepath.Join(home, WorkerLockName))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	lock, err := fs.LockExclusive(file)
	if err != nil {
		t.Fatal(err)
	}
	writeRecord(t, home, WorkerRecord{Version: "v9.9.9", PID: 41})
	status, err := InspectWorker(home)
	if err != nil || status.Run != WorkerRunning || status.RecordState != WorkerRecordRecorded || status.Record.PID != 41 {
		t.Fatalf("a held lock is a running worker with its record: %+v, error %v", status, err)
	}
	if err := lock.Unlock(); err != nil {
		t.Fatal(err)
	}
	status, err = InspectWorker(home)
	if err != nil || status.Run != WorkerStopped {
		t.Fatalf("a released lock is a stopped worker: %+v, error %v", status, err)
	}
}

func TestSamePathCleansAndRejectsEmptyPaths(t *testing.T) {
	if !samePath("/a/b/../kander", "/a/kander") {
		t.Fatal("paths are cleaned before the comparison")
	}
	if samePath("/a/kander", "/b/kander") {
		t.Fatal("different paths do not match")
	}
	if got, want := samePath("/Opt/Kander", "/opt/kander"), runtime.GOOS == "windows"; got != want {
		t.Fatalf("paths that differ only in case match on Windows only: got %v", got)
	}
	if samePath("", "") || samePath("", "/a") {
		t.Fatal("an empty path never matches")
	}
}
