package remote

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/config"
)

var errSharedBusy = errors.New("sharing violation")

// stubSharedRead makes errSharedBusy the only busy error and records the sleeps.
// It restores the package hooks, so tests using it must not run in parallel.
func stubSharedRead(t *testing.T) *[]time.Duration {
	t.Helper()
	busy, sleep := sharedReadBusy, sharedReadSleep
	var slept []time.Duration
	sharedReadBusy = func(err error) bool { return errors.Is(err, errSharedBusy) }
	sharedReadSleep = func(d time.Duration) { slept = append(slept, d) }
	t.Cleanup(func() { sharedReadBusy, sharedReadSleep = busy, sleep })
	return &slept
}

func TestSharedReadWaitsStayWithinOneSecond(t *testing.T) {
	var total time.Duration
	for _, wait := range sharedReadWaits {
		total += wait
	}
	if total > time.Second {
		t.Fatalf("waits sum to %v, above the one second bound", total)
	}
}

func TestRetrySharedReadReturnsTheFirstSuccessWithoutSleeping(t *testing.T) {
	slept := stubSharedRead(t)
	calls := 0
	got, err := retrySharedRead(func() (string, error) { calls++; return "first", nil })
	if err != nil || got != "first" || calls != 1 || len(*slept) != 0 {
		t.Fatalf("got %q, %v after %d reads and %v sleeps", got, err, calls, *slept)
	}
}

func TestRetrySharedReadReturnsTheContentOfTheRetryThatSucceeds(t *testing.T) {
	slept := stubSharedRead(t)
	calls := 0
	got, err := retrySharedRead(func() (int, error) {
		calls++
		if calls < 3 {
			return 0, errSharedBusy
		}
		return calls, nil
	})
	if err != nil || got != 3 || calls != 3 {
		t.Fatalf("got %d, %v after %d reads", got, err, calls)
	}
	if len(*slept) != 2 || (*slept)[0] != sharedReadWaits[0] || (*slept)[1] != sharedReadWaits[1] {
		t.Fatalf("slept %v, want the first two waits of %v", *slept, sharedReadWaits)
	}
}

func TestRetrySharedReadDoesNotRetryOtherErrors(t *testing.T) {
	slept := stubSharedRead(t)
	other := os.ErrNotExist
	calls := 0
	_, err := retrySharedRead(func() (int, error) { calls++; return 0, other })
	if err != other || calls != 1 || len(*slept) != 0 {
		t.Fatalf("got %v after %d reads and %v sleeps, want the original error after one read", err, calls, *slept)
	}
}

func TestRetrySharedReadDoesNotRetryAfterTheErrorChanges(t *testing.T) {
	slept := stubSharedRead(t)
	calls := 0
	_, err := retrySharedRead(func() (int, error) {
		calls++
		if calls == 1 {
			return 0, errSharedBusy
		}
		return 0, os.ErrPermission
	})
	if err != os.ErrPermission || calls != 2 || len(*slept) != 1 {
		t.Fatalf("got %v after %d reads and %v sleeps", err, calls, *slept)
	}
}

func TestRetrySharedReadReportsBusyAfterTheLastWait(t *testing.T) {
	slept := stubSharedRead(t)
	calls := 0
	_, err := retrySharedRead(func() (int, error) { calls++; return 0, errSharedBusy })
	if calls != len(sharedReadWaits)+1 || len(*slept) != len(sharedReadWaits) {
		t.Fatalf("%d reads and %d sleeps, want %d and %d", calls, len(*slept), len(sharedReadWaits)+1, len(sharedReadWaits))
	}
	var total time.Duration
	for _, d := range *slept {
		total += d
	}
	if total > time.Second {
		t.Fatalf("slept %v in total", total)
	}
	if !errors.Is(err, errSharedBusy) || !strings.HasPrefix(err.Error(), config.Text("remote.credential_busy")) {
		t.Fatalf("got %q, want the localized busy text around the original error", err)
	}
}
