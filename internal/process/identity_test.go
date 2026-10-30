package process

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"testing"
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

// The ID was reused while the process settled. The later process is never recorded,
// however stable it looks.
func TestSettleRefusesALaterProcess(t *testing.T) {
	first := ProcessFacts{PID: 7, Start: "100", Executable: "agent"}
	later := ProcessFacts{PID: 7, Start: "200", Executable: "agent"}
	watched := &incarnation{pid: 7, running: alwaysRunning, observe: samples(first, later)}
	if _, err := settle(context.Background(), watched); !errors.Is(err, ErrProcessReplaced) {
		t.Fatalf("err=%v", err)
	}
}

func TestAwaitReadyRefusesALaterProcess(t *testing.T) {
	first := ProcessFacts{PID: 7, Start: "100", Executable: "dash"}
	later := ProcessFacts{PID: 7, Start: "200", Executable: "agent"}
	watched := &incarnation{pid: 7, start: first.Start, running: alwaysRunning, observe: samples(later)}
	if _, err := awaitReady(context.Background(), watched, alwaysRunning); !errors.Is(err, ErrProcessReplaced) {
		t.Fatalf("err=%v", err)
	}
}

// A sample taken after the handle saw the exit can name anybody, even on the first
// read, where there is no earlier start time to compare.
func TestSampleChecksTheStartHandleAfterTheRead(t *testing.T) {
	facts := ProcessFacts{PID: 7, Start: "100", Executable: "agent"}
	exited := func() bool { return false }
	watched := &incarnation{pid: 7, running: exited, observe: samples(facts)}
	if _, err := settle(context.Background(), watched); !errors.Is(err, ErrProcessGone) {
		t.Fatalf("err=%v", err)
	}
}

func TestSettleKeepsAnEarlyImageChange(t *testing.T) {
	early := ProcessFacts{PID: 7, Start: "100", Executable: "dash"}
	final := ProcessFacts{PID: 7, Start: "100", Executable: "agent"}
	watched := &incarnation{pid: 7, running: alwaysRunning, observe: samples(early, final)}
	got, err := settle(context.Background(), watched)
	if err != nil || got != final {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestSettleGivesUpOnAProcessThatKeepsChanging(t *testing.T) {
	var n int
	observe := func(context.Context, int) (ProcessFacts, error) {
		n++
		return ProcessFacts{PID: 7, Start: "100", Executable: strconv.Itoa(n)}, nil
	}
	watched := &incarnation{pid: 7, running: alwaysRunning, observe: observe}
	if _, err := settle(context.Background(), watched); !errors.Is(err, ErrProcessUnsettled) {
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

func TestImageConfirmed(t *testing.T) {
	for _, c := range []struct {
		program, executable string
		confirmed           bool
	}{
		{"/usr/bin/agent", "agent", true},
		{"/opt/a-very-long-agent-binary-name", "a-very-long-agen", true},
		{"/usr/bin/agent", "dash", !ImageCanChange},
		{"/usr/bin/agent", "age", !ImageCanChange},
		{"/usr/bin/agent", "", !ImageCanChange},
	} {
		if got := ImageConfirmed(c.program, ProcessFacts{Executable: c.executable}); got != c.confirmed {
			t.Errorf("ImageConfirmed(%q, %q) = %v", c.program, c.executable, got)
		}
	}
}
