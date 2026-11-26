package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/internal/remote"
)

func readRecord(t *testing.T, home string) remote.WorkerRecord {
	t.Helper()
	record, state, err := remote.ReadWorkerRecord(home)
	if err != nil || state != remote.WorkerRecordRecorded {
		t.Fatalf("worker record: state %v, error %v", state, err)
	}
	return record
}

func TestServeWritesTheWorkerRecord(t *testing.T) {
	r := newRig(t)
	opts := r.options()
	opts.Version, opts.PID = func() string { return "v-record" }, func() int { return 777 }
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, opts) }()
	// The start log follows the write of the record.
	waitFor(t, "the worker start", func() bool { return r.sink.find("worker started") != nil })
	record := readRecord(t, r.home)
	finish(t, done, cancel)
	want := remote.WorkerRecord{Schema: 1, Version: "v-record", PID: 777, Executable: r.exe, Handover: true, StartedAt: r.now().UTC()}
	if record != want {
		t.Fatalf("worker record %+v, want %+v", record, want)
	}
	if got := r.sink.find("worker started"); got["version"] != "v-record" || got["pid"] != float64(777) {
		t.Fatalf("the start log names the injected version and pid: %v", got)
	}
}

func TestWorkerRecordSaysWhetherTheWorkerHandsOver(t *testing.T) {
	for _, tc := range []struct {
		name       string
		executable func(*rig) (string, error)
		identity   func(string) (os.FileInfo, error)
		wantExe    func(*rig) string
	}{
		{"the file identity cannot be read",
			func(r *rig) (string, error) { return r.exe, nil },
			func(string) (os.FileInfo, error) { return nil, errors.New("no identity") },
			func(r *rig) string { return r.exe }},
		{"the executable cannot be read",
			func(*rig) (string, error) { return "", errors.New("no executable") },
			nil,
			func(*rig) string { return "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			opts := r.options()
			opts.Executable = func() (string, error) { return tc.executable(r) }
			if tc.identity != nil {
				opts.Identity = tc.identity
			}
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- Serve(ctx, opts) }()
			waitFor(t, "the worker start", func() bool { return r.sink.find("worker started") != nil })
			record := readRecord(t, r.home)
			finish(t, done, cancel)
			if record.Handover || record.Executable != tc.wantExe(r) {
				t.Fatalf("a worker that cannot watch its executable records handover=false and what it knows: %+v", record)
			}
		})
	}
}

// A record that an earlier worker left names that worker's version and pid, so a
// worker that cannot publish its own removes it.
func TestFailedRecordWriteIsLoggedRemovesTheOldRecordAndTheWorkerRenews(t *testing.T) {
	r := newRig(t)
	r.claimed("record-fails")
	if err := remote.WriteWorkerRecord(r.home, remote.WorkerRecord{Version: "v-earlier", PID: 99, StartedAt: r.now()}); err != nil {
		t.Fatal(err)
	}
	opts := r.options()
	opts.WriteRecord = func(string, remote.WorkerRecord) error { return errors.New("no space left on device") }
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, opts) }()
	waitFor(t, "the worker start", func() bool { return r.sink.find("worker started") != nil })
	if got := r.sink.find("worker record could not be written"); got == nil || got["level"] != "ERROR" {
		t.Fatalf("a failed write of the record is an ERROR in the log: %v", got)
	}
	if _, state, err := remote.ReadWorkerRecord(r.home); state != remote.WorkerRecordMissing || err != nil {
		t.Fatalf("the record of the earlier worker is gone: state %v, error %v", state, err)
	}
	r.expectRenewal()
	finish(t, done, cancel)
}

// kander worker binds the language of its configuration, so the observation
// texts it stores do not follow the locale of the process.
func TestRunBindsTheConfiguredLanguage(t *testing.T) {
	// The project overlay is read from the working directory.
	t.Chdir(t.TempDir())
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	t.Setenv(config.EnvLang, "")
	t.Setenv(config.EnvLangCLI, "")
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Cleanup(func() { config.BindConfigLanguage(nil) })
	cfg := config.DefaultConfig()
	cfg.Language = "ja"
	cfg.WelcomeComplete = true
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv(remote.EnvClientHome, home)
	// A held lock makes run return as soon as it has bound the language.
	file, err := fs.OpenLockFile(home, filepath.Join(home, remote.WorkerLockName))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	lock, err := fs.LockExclusive(file)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Unlock()
	if code := run(); code != 0 {
		t.Fatalf("run exits 0 when another worker holds the lock: %d", code)
	}
	if got := config.ResolveLanguage(); got != "ja" {
		t.Fatalf("language %q, want the configured ja", got)
	}
}

func TestHandoverReplacesTheWorkerRecord(t *testing.T) {
	r := newRig(t)
	r.claimed("record-handover")
	first, second := r.options(), r.options()
	first.Version, first.PID = func() string { return "v1" }, func() int { return 111 }
	second.Version, second.PID = func() string { return "v2" }, func() int { return 222 }
	started := r.now()
	secondDone := make(chan error, 1)
	secondCtx, stopSecond := context.WithCancel(t.Context())
	first.Spawn = func(string, string) (Successor, error) {
		// The new worker starts later than the old one.
		r.advance(time.Hour)
		go func() { secondDone <- Serve(secondCtx, second) }()
		return &fakeSuccessor{pid: 222}, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	firstDone := make(chan error, 1)
	go func() { firstDone <- Serve(ctx, first) }()
	waitFor(t, "the first worker record", func() bool {
		record, state, _ := remote.ReadWorkerRecord(r.home)
		return state == remote.WorkerRecordRecorded && record.Version == "v1"
	})
	if record := readRecord(t, r.home); record.PID != 111 || !record.StartedAt.Equal(started.UTC()) {
		t.Fatalf("the first worker records itself: %+v", record)
	}
	r.writeExecutable("v2")
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("a handover ends Serve with nil: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the worker did not hand over")
	}
	waitFor(t, "the record of the new worker", func() bool {
		record, state, _ := remote.ReadWorkerRecord(r.home)
		return state == remote.WorkerRecordRecorded && record.Version == "v2"
	})
	record := readRecord(t, r.home)
	if record.PID != 222 || !record.StartedAt.Equal(started.Add(time.Hour).UTC()) {
		t.Fatalf("the record is the new worker's: %+v", record)
	}
	finish(t, secondDone, stopSecond)
}
