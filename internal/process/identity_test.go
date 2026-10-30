package process

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"testing"
)

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
