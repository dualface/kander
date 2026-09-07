package probe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestProbeProcessHelper is a real subprocess on both POSIX and Windows. It
// inherits both output pipes and records readiness before the parent is canceled.
func TestProbeProcessHelper(t *testing.T) {
	args := os.Args
	if len(args) < 4 || args[len(args)-3] != "probe-process-helper" {
		return
	}
	mode, dir := args[len(args)-2], args[len(args)-1]
	switch mode {
	case "output":
		fmt.Fprint(os.Stdout, strings.Repeat("out", 100000))
		fmt.Fprint(os.Stderr, strings.Repeat("err", 100000))
		os.Exit(7)
	case "parent", "parent-exit", "parent-escape":
		child := exec.Command(os.Args[0], "-test.run=^TestProbeProcessHelper$", "--", "probe-process-helper", "child", dir)
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if mode == "parent-escape" {
			detachProbeHelper(child)
		}
		if err := child.Start(); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(2)
		}
		if mode != "parent" {
			// Keep the parent until the child has initialized, including its PID record.
			for i := 0; i < 500; i++ {
				if _, err := os.Stat(filepath.Join(dir, "child")); err == nil {
					os.Exit(0)
				}
				time.Sleep(time.Millisecond * 10)
			}
			os.Exit(3)
		}
	case "child":
	default:
		os.Exit(4)
	}
	if err := os.WriteFile(filepath.Join(dir, mode), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		os.Exit(5)
	}
	time.Sleep(5 * time.Second)
	os.Exit(0)
}

func configureProbeHelpers(t *testing.T) {
	t.Helper()
	// Race instrumentation remains enabled; remove only its one-second sleep on
	// subprocess exit so the test measures pipe/process cleanup, not that delay.
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
}

func helperArgs(mode, dir string) []string {
	return []string{"-test.run=^TestProbeProcessHelper$", "--", "probe-process-helper", mode, dir}
}

func helperPID(t *testing.T, dir, mode string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, mode))
	if err != nil {
		t.Fatalf("helper did not start: %v", err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if helperProcessRunning(pid) {
			process, err := os.FindProcess(pid)
			if err == nil {
				_ = process.Kill()
				_ = process.Release()
			}
		}
	})
	return pid
}

func requireStopped(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !helperProcessRunning(pid) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("helper process %d survived cancellation", pid)
}

func waitHelper(t *testing.T, dir, mode string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(filepath.Join(dir, mode)); err == nil && len(data) > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("helper %s did not become ready", mode)
}

func TestAuditDescendantOutputOutlivesProbeDeadline(t *testing.T) {
	configureProbeHelpers(t)
	// Invert the original audit: a five-second descendant must not hold a
	// 300ms probe open after its parent is killed at the deadline.
	dir := t.TempDir()
	started := time.Now()
	result, err := Capture(os.Args[0], helperArgs("parent", dir), 300*time.Millisecond)
	elapsed := time.Since(started)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if elapsed < 300*time.Millisecond || elapsed > 600*time.Millisecond {
		t.Fatalf("elapsed=%s; deadline=300ms, cleanup tolerance=300ms", elapsed)
	}
	requireStopped(t, helperPID(t, dir, "parent"))
	requireStopped(t, helperPID(t, dir, "child"))
}

func TestCaptureCancellationJoinsProcessesAndReaders(t *testing.T) {
	configureProbeHelpers(t)
	baseline := runtime.NumGoroutine()
	for i := 0; i < 4; i++ {
		dir := t.TempDir()
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		done := make(chan error, 1)
		go func() { _, err := CaptureContext(ctx, os.Args[0], helperArgs("parent", dir)); done <- err }()
		waitHelper(t, dir, "parent")
		waitHelper(t, dir, "child")
		parent, child := helperPID(t, dir, "parent"), helperPID(t, dir, "child")
		started := time.Now()
		cancel()
		err := <-done
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
		if elapsed := time.Since(started); elapsed > 300*time.Millisecond {
			t.Fatalf("cancel took %s", elapsed)
		}
		requireStopped(t, parent)
		requireStopped(t, child)
	}
	deadline := time.Now().Add(300 * time.Millisecond)
	for runtime.NumGoroutine() > baseline && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > baseline {
		t.Fatalf("goroutines before=%d after=%d", baseline, n)
	}
}

func TestCaptureCleansDescendantsAfterParentExits(t *testing.T) {
	configureProbeHelpers(t)
	dir := t.TempDir()
	started := time.Now()
	_, err := Capture(os.Args[0], helperArgs("parent-exit", dir), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("waited for descendant: %s", elapsed)
	}
	requireStopped(t, helperPID(t, dir, "child"))
}

func TestCapturePreservesOutputAndExitCode(t *testing.T) {
	configureProbeHelpers(t)
	result, err := Capture(os.Args[0], helperArgs("output", t.TempDir()), 3*time.Second)
	if err != nil || result.Code != 7 || result.Stdout != strings.Repeat("out", 100000) || result.Stderr != strings.Repeat("err", 100000) {
		t.Fatalf("code=%d stdout=%d stderr=%d err=%v", result.Code, len(result.Stdout), len(result.Stderr), err)
	}
}

func TestCaptureDoesNotStartWithExpiredBudget(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		want := context.DeadlineExceeded
		if canceled {
			cancel()
			ctx, cancel = context.WithCancel(context.Background())
			cancel()
			want = context.Canceled
		}
		_, err := CaptureContext(ctx, "must-not-be-looked-up-or-started", nil)
		cancel()
		if !errors.Is(err, want) {
			t.Fatalf("err=%v want=%v", err, want)
		}
	}
}

func TestTmuxExpiredFactsDoNotStartMarkerProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	withRun(t, func(ctx context.Context, program string, args []string) (Result, error) {
		calls++
		cancel()
		return Result{Stdout: "codex\t0\t0\n"}, nil
	})
	_, err := ProbeTmuxPaneContext(ctx, "tmux", "%1")
	if calls != 1 || !errors.Is(err, context.Canceled) {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}
