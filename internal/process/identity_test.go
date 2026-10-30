package process

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func alwaysRunning() bool { return true }

// samples answers a watched incarnation with a fixed sequence; the last one repeats.
func samples(facts ...ProcessFacts) func(context.Context, int) (ProcessFacts, error) {
	return func(context.Context, int) (ProcessFacts, error) {
		next := facts[0]
		if len(facts) > 1 {
			facts = facts[1:]
		}
		return next, nil
	}
}

func requireSupported(t *testing.T) {
	t.Helper()
	if !ProcessIdentitySupported {
		t.Skip("no process identity on " + runtime.GOOS)
	}
}

func TestObserveProcessReadsTheCurrentProcess(t *testing.T) {
	requireSupported(t)
	self, err := ObserveProcess(context.Background(), os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if self.PID != os.Getpid() || self.ParentPID != os.Getppid() || self.Start == "" || self.Executable == "" {
		t.Fatalf("facts=%+v", self)
	}
	again, err := ObserveProcess(context.Background(), os.Getpid())
	if err != nil || again != self {
		t.Fatalf("second reading %+v err=%v, first %+v", again, err, self)
	}
}

func TestObserveProcessReportsAnExitedChildAsGone(t *testing.T) {
	requireSupported(t)
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if _, err := ObserveProcess(context.Background(), cmd.Process.Pid); !errors.Is(err, ErrProcessGone) {
		t.Fatalf("err=%v", err)
	}
}

func TestObserveProcessHonorsTheDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ObserveProcess(ctx, os.Getpid()); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestBootIdentityIsStable(t *testing.T) {
	requireSupported(t)
	first, err := BootIdentity()
	if err != nil || first == "" {
		t.Fatalf("boot=%q err=%v", first, err)
	}
	second, err := BootIdentity()
	if err != nil || !SameBoot(first, second) {
		t.Fatalf("boot %q then %q err=%v", first, second, err)
	}
}

// The tolerance is pinned here and in the card's plan.md: Windows derives the boot
// time from the wall clock, and macOS adjusts it with the calendar clock.
func TestSameBoot(t *testing.T) {
	for _, c := range []struct {
		recorded, current string
		same              bool
	}{
		{"3fa85f64-5717-4562-b3fc-2c963f66afa6", "3fa85f64-5717-4562-b3fc-2c963f66afa6", true},
		{"3fa85f64-5717-4562-b3fc-2c963f66afa6", "7c9e6679-7425-40de-944b-e07fc1f90ae7", false},
		{"1000", "1000", true},
		{"1000", "1300", true},
		{"1300", "1000", true},
		{"1000", "1301", false},
		{"1301", "1000", false},
		{"1000", "3fa85f64-5717-4562-b3fc-2c963f66afa6", false},
		{"", "1000", false},
	} {
		if got := SameBoot(c.recorded, c.current); got != c.same {
			t.Errorf("SameBoot(%q, %q) = %v", c.recorded, c.current, got)
		}
	}
}

// The ID was reused while the launcher watched. The later process is never
// recorded, however ready the agent looks.
func TestAwaitReadyRefusesALaterProcess(t *testing.T) {
	later := ProcessFacts{PID: 7, Start: "200", Executable: "agent"}
	watched := &incarnation{pid: 7, start: "100", running: alwaysRunning, observe: samples(later)}
	if _, err := awaitReady(context.Background(), watched, alwaysRunning); !errors.Is(err, ErrProcessReplaced) {
		t.Fatalf("err=%v", err)
	}
}

func TestSampleKeepsTheFirstStartTime(t *testing.T) {
	first := ProcessFacts{PID: 7, Start: "100", Executable: "agent"}
	later := ProcessFacts{PID: 7, Start: "200", Executable: "agent"}
	watched := &incarnation{pid: 7, running: alwaysRunning, observe: samples(first, later)}
	if got, err := watched.sample(context.Background()); err != nil || got != first {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if _, err := watched.sample(context.Background()); !errors.Is(err, ErrProcessReplaced) {
		t.Fatalf("err=%v", err)
	}
}

// A sample taken after the handle saw the exit can name anybody, even on the first
// read, where there is no earlier start time to compare.
func TestSampleChecksTheStartHandleAfterTheRead(t *testing.T) {
	facts := ProcessFacts{PID: 7, Start: "100", Executable: "agent"}
	watched := &incarnation{pid: 7, running: func() bool { return false }, observe: samples(facts)}
	if _, err := watched.sample(context.Background()); !errors.Is(err, ErrProcessGone) {
		t.Fatalf("err=%v", err)
	}
}

// The sample that returns postdates the evidence.
func TestAwaitReadyReadsAfterTheEvidence(t *testing.T) {
	early := ProcessFacts{PID: 7, Start: "100", Executable: "dash"}
	final := ProcessFacts{PID: 7, Start: "100", Executable: "agent"}
	calls := 0
	ready := func() bool { calls++; return calls > 1 }
	watched := &incarnation{pid: 7, start: "100", running: alwaysRunning, observe: samples(early, final)}
	got, err := awaitReady(context.Background(), watched, ready)
	if err != nil || got != final {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

// The evidence of a running agent. A filesystem without access times skips this
// test, and the launcher then keeps unknown; the skip names the gap in CI logs.
func TestFileAccessTimeFollowsARead(t *testing.T) {
	if !ProcessIdentitySupported || !ImageCanChange {
		t.Skip("no use for access times on " + runtime.GOOS)
	}
	path := filepath.Join(t.TempDir(), "task")
	if err := os.WriteFile(path, []byte("task"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := FileAccessTime(path)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if unchanged, err := FileAccessTime(path); err != nil || !unchanged.Equal(before) {
		t.Fatalf("a stat moved the access time: %v %v", unchanged, err)
	}
	if _, err = os.ReadFile(path); err != nil {
		t.Fatal(err)
	}
	after, err := FileAccessTime(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.After(before) {
		t.Skip("this filesystem records no access time")
	}
}
