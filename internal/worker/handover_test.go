package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/version"
)

const (
	probeModeEnv    = "KANDER_WORKER_PROBE_MODE"
	probePIDFileEnv = "KANDER_WORKER_PROBE_PID_FILE"
)

// TestMain lets the test binary play the part of a kander executable for the
// default probe: `<test binary> version` behaves as the mode in the
// environment says.
func TestMain(m *testing.M) {
	if mode := os.Getenv(probeModeEnv); mode != "" {
		os.Exit(playProbeTarget(mode))
	}
	os.Exit(m.Run())
}

func playProbeTarget(mode string) int {
	switch mode {
	case "ok":
		fmt.Println("kander v-test")
		return 0
	case "no-prefix":
		fmt.Println("hello")
		return 0
	case "fail":
		fmt.Fprintln(os.Stderr, "no such command")
		return 3
	case "hang":
		// Leave a descendant behind that outlives its parent unless the whole
		// tree is killed.
		child, err := startSleeper()
		if err != nil {
			return 1
		}
		_ = os.WriteFile(os.Getenv(probePIDFileEnv), []byte(strconv.Itoa(child)), 0o600)
		time.Sleep(time.Minute)
		return 0
	case "sleep":
		time.Sleep(time.Minute)
		return 0
	}
	return 2
}

// startSleeper starts a copy of the test binary that only sleeps and returns
// its process ID.
func startSleeper() (int, error) {
	self, err := os.Executable()
	if err != nil {
		return 0, err
	}
	cmd := exec.Command(self, "version")
	cmd.Env = append(os.Environ(), probeModeEnv+"=sleep")
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	return cmd.Process.Pid, nil
}

// logSink collects the JSON log lines of a worker.
type logSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *logSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *logSink) find(msg string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, line := range strings.Split(s.buf.String(), "\n") {
		var record map[string]any
		if json.Unmarshal([]byte(line), &record) == nil && record["msg"] == msg {
			return record
		}
	}
	return nil
}

type fakeSuccessor struct {
	pid    int
	exited atomic.Bool
}

func (f *fakeSuccessor) PID() int     { return f.pid }
func (f *fakeSuccessor) Exited() bool { return f.exited.Load() }

// rig runs Serve against the harness with every process-touching step
// replaced, and a real file standing in for the worker's executable.
type rig struct {
	*harness
	exe        string
	sink       *logSink
	identities atomic.Int32
	probes     atomic.Int32
	spawns     atomic.Int32
	probe      func() error
	spawn      func(home, executable string) (Successor, error)
	timeout    time.Duration
	mu         sync.Mutex
	delivered  []string
}

func newRig(t *testing.T) *rig {
	r := &rig{harness: newHarness(t), sink: &logSink{}, timeout: 2 * time.Second}
	r.exe = filepath.Join(t.TempDir(), "kander")
	r.writeExecutable("v1")
	r.probe = func() error { return nil }
	r.spawn = func(string, string) (Successor, error) { return nil, errors.New("no spawn expected") }
	return r
}

// writeExecutable replaces the file the way an upgrade does: a new file is
// renamed over the old path.
func (r *rig) writeExecutable(content string) {
	next := r.exe + ".new"
	if err := os.WriteFile(next, []byte(content), 0o700); err != nil {
		r.t.Fatal(err)
	}
	if err := os.Rename(next, r.exe); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) options() Options {
	return Options{
		Home:   r.home,
		Tick:   5 * time.Millisecond,
		Logger: slog.New(slog.NewJSONHandler(r.sink, nil)),
		Now:    r.now,
		Stop:   func(int) error { return nil },
		Deliver: func(window, session, text string) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.delivered = append(r.delivered, text)
			return nil
		},
		Executable: func() (string, error) { return r.exe, nil },
		Identity: func(path string) (os.FileInfo, error) {
			r.identities.Add(1)
			return fileIdentity(path)
		},
		Probe: func(context.Context, string) error {
			r.probes.Add(1)
			return r.probe()
		},
		Spawn: func(home, executable string) (Successor, error) {
			r.spawns.Add(1)
			return r.spawn(home, executable)
		},
		ConfirmTimeout: r.timeout,
		ConfirmPoll:    5 * time.Millisecond,
	}
}

func (r *rig) deliveries() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.delivered)
}

// serve runs a worker loop once it has recorded its executable; the result
// channel yields Serve's return value.
func (r *rig) serve(ctx context.Context) <-chan error {
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, r.options()) }()
	waitFor(r.t, "the worker start", func() bool { return r.sink.find("worker started") != nil })
	return done
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// ticks waits until the worker checked its executable n more times.
func (r *rig) ticks(n int32) {
	r.t.Helper()
	target := r.identities.Load() + n
	waitFor(r.t, "worker ticks", func() bool { return r.identities.Load() >= target })
}

// expectRenewal advances the clock one renewal interval and waits until the
// credential shows a renewal at the new time.
func (r *rig) expectRenewal() {
	r.t.Helper()
	r.advance(16 * time.Second)
	waitFor(r.t, "a renewal", func() bool { return r.credential().RenewedAt.Equal(r.now()) })
}

func finish(t *testing.T, done <-chan error, cancel context.CancelFunc) {
	t.Helper()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return")
	}
}

// homeBytes reads every private file of the client home except the worker's
// own lock and log.
func homeBytes(t *testing.T, home string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(home, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() == remote.WorkerLockName || d.Name() == "worker.log" {
			return err
		}
		data, err := os.ReadFile(path)
		files[path] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestServeLogsTheWorkerStart(t *testing.T) {
	r := newRig(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := r.serve(ctx)
	finish(t, done, cancel)
	got := r.sink.find("worker started")
	if got["version"] != version.String() || got["pid"] != float64(os.Getpid()) || got["executable"] != r.exe {
		t.Fatalf("start log lacks version, pid or executable: %v", got)
	}
}

func TestNoHandoverWhileTheExecutableIsUnchanged(t *testing.T) {
	r := newRig(t)
	r.claimed("unchanged")
	ctx, cancel := context.WithCancel(t.Context())
	done := r.serve(ctx)
	r.ticks(10)
	finish(t, done, cancel)
	if r.probes.Load() != 0 || r.spawns.Load() != 0 {
		t.Fatalf("an unchanged executable must not be probed or replaced: probes=%d spawns=%d", r.probes.Load(), r.spawns.Load())
	}
}

func TestHandoverToTheReplacedBinary(t *testing.T) {
	r := newRig(t)
	p, c := r.claimed("handover", true)
	b := r.project("host-b")
	if _, err := b.CreateRequest(t.Context(), "", c.TaskID, api.NewCollabRequest{Kind: api.RequestFix, Message: "fix the test", TargetGeneration: 1}); err != nil {
		t.Fatal(err)
	}
	successorCtx, stopSuccessor := context.WithCancel(t.Context())
	successor := make(chan error, 1)
	var spawnHome, spawnExe string
	var lockFree bool
	var before map[string]string
	r.spawn = func(home, executable string) (Successor, error) {
		spawnHome, spawnExe = home, executable
		held, err := remote.WorkerHeld(home)
		lockFree = err == nil && !held
		before = homeBytes(t, home)
		// The new worker is a second Serve on the same client home.
		go func() { successor <- Serve(successorCtx, r.options()) }()
		return &fakeSuccessor{pid: 4321}, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := r.serve(ctx)
	r.advance(16 * time.Second)
	waitFor(t, "the first delivery", func() bool { return r.deliveries() == 1 })

	r.writeExecutable("v2")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a handover ends Serve with nil: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the worker did not hand over")
	}
	if got := r.spawns.Load(); got != 1 || spawnHome != r.home || spawnExe != r.exe {
		t.Fatalf("one start with the recorded path and home expected: n=%d home=%q exe=%q", got, spawnHome, spawnExe)
	}
	if !lockFree {
		t.Fatal("worker.lock must be released before the new worker starts")
	}
	if after := homeBytes(t, r.home); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("a handover must not write credentials:\nbefore %v\nafter  %v", before, after)
	}
	log := r.sink.find("worker handed over")
	if log == nil || log["old_version"] != version.String() || log["executable"] != r.exe || log["new_pid"] != float64(4321) {
		t.Fatalf("handover log lacks old version, path or new PID: %v", log)
	}

	r.expectRenewal()
	shown, err := p.Show(t.Context(), c.TaskID)
	if err != nil || shown.Claim.Generation != c.Generation || r.credential().Generation != c.Generation {
		t.Fatalf("the new worker renews the same claim: %+v %v", shown.Claim, err)
	}
	if got := r.deliveries(); got != 1 {
		t.Fatalf("a request delivered before the handover is not delivered again: %d", got)
	}
	finish(t, successor, stopSuccessor)
}

func TestProbeFailureKeepsServingAndBacksOff(t *testing.T) {
	r := newRig(t)
	r.claimed("probe-fails")
	r.probe = func() error { return errors.New("exit 3") }
	ctx, cancel := context.WithCancel(t.Context())
	done := r.serve(ctx)
	r.writeExecutable("v2")
	waitFor(t, "the first probe", func() bool { return r.probes.Load() == 1 })
	if r.sink.find("replaced worker binary is not usable; handover skipped") == nil {
		t.Fatal("a failed probe is logged")
	}
	for range 3 {
		r.expectRenewal()
		r.ticks(2)
	}
	if r.probes.Load() != 1 {
		t.Fatalf("the failed file identity is not probed again within a minute: %d", r.probes.Load())
	}
	r.writeExecutable("v3")
	waitFor(t, "an immediate probe of the new identity", func() bool { return r.probes.Load() == 2 })
	for range 4 {
		r.expectRenewal()
	}
	r.ticks(2)
	if r.probes.Load() != 3 {
		t.Fatalf("a minute later the identity is probed again: %d", r.probes.Load())
	}
	if r.spawns.Load() != 0 {
		t.Fatal("a failed probe never starts a worker")
	}
	finish(t, done, cancel)
}

func TestMissingExecutableIsNotProbed(t *testing.T) {
	r := newRig(t)
	r.claimed("missing")
	ctx, cancel := context.WithCancel(t.Context())
	done := r.serve(ctx)
	r.ticks(2)
	if err := os.Remove(r.exe); err != nil {
		t.Fatal(err)
	}
	r.expectRenewal()
	r.ticks(5)
	if r.probes.Load() != 0 || r.spawns.Load() != 0 {
		t.Fatalf("a path that cannot be read is neither probed nor replaced: probes=%d spawns=%d", r.probes.Load(), r.spawns.Load())
	}
	finish(t, done, cancel)
}

func TestSpawnFailureTakesTheLockBackAndBacksOff(t *testing.T) {
	r := newRig(t)
	r.claimed("spawn-fails")
	r.spawn = func(string, string) (Successor, error) { return nil, errors.New("exec format error") }
	ctx, cancel := context.WithCancel(t.Context())
	done := r.serve(ctx)
	r.writeExecutable("v2")
	waitFor(t, "the start attempt", func() bool { return r.spawns.Load() == 1 })
	waitFor(t, "the lock back", func() bool { held, _ := remote.WorkerHeld(r.home); return held })
	r.expectRenewal()
	r.ticks(5)
	if r.probes.Load() != 1 || r.spawns.Load() != 1 {
		t.Fatalf("the same identity is not tried again within a minute: probes=%d spawns=%d", r.probes.Load(), r.spawns.Load())
	}
	finish(t, done, cancel)
}

func TestSuccessorThatExitsIsNotTrusted(t *testing.T) {
	r := newRig(t)
	r.claimed("successor-exits")
	r.spawn = func(string, string) (Successor, error) {
		s := &fakeSuccessor{pid: 4321}
		s.exited.Store(true)
		return s, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := r.serve(ctx)
	r.writeExecutable("v2")
	waitFor(t, "the start attempt", func() bool { return r.spawns.Load() == 1 })
	waitFor(t, "the lock back", func() bool { held, _ := remote.WorkerHeld(r.home); return held })
	r.expectRenewal()
	if r.sink.find("worker handed over") != nil {
		t.Fatal("an exited worker is not a handover")
	}
	finish(t, done, cancel)
}

func TestSuccessorThatNeverTakesTheLockIsNotTrusted(t *testing.T) {
	r := newRig(t)
	r.claimed("successor-idle")
	r.timeout = 50 * time.Millisecond
	r.spawn = func(string, string) (Successor, error) { return &fakeSuccessor{pid: 4321}, nil }
	ctx, cancel := context.WithCancel(t.Context())
	done := r.serve(ctx)
	r.writeExecutable("v2")
	waitFor(t, "the start attempt", func() bool { return r.spawns.Load() == 1 })
	waitFor(t, "the lock back", func() bool { held, _ := remote.WorkerHeld(r.home); return held })
	r.expectRenewal()
	if r.sink.find("worker handed over") != nil {
		t.Fatal("a worker that holds no lock is not a handover")
	}
	finish(t, done, cancel)
}

func TestProbeExecutable(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		mode string
		ok   bool
	}{{"ok", true}, {"fail", false}, {"no-prefix", false}} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Setenv(probeModeEnv, tc.mode)
			err := probeExecutable(t.Context(), self, 5*time.Second)
			if (err == nil) != tc.ok {
				t.Fatalf("mode %s: err=%v", tc.mode, err)
			}
		})
	}
}

func TestProbeExecutableTimeoutReapsTheProcessTree(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv(probeModeEnv, "hang")
	t.Setenv(probePIDFileEnv, pidFile)
	if err := probeExecutable(t.Context(), self, time.Second); err == nil {
		t.Fatal("a probe past its timeout fails")
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("the probed process never started its child: %v", err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the descendant to be gone", func() bool {
		_, err := process.ObserveProcess(t.Context(), pid)
		return errors.Is(err, process.ErrProcessGone)
	})
}
