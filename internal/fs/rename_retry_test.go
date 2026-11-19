package fs

import (
	"testing"
	"time"
)

func TestNextRenameWait(t *testing.T) {
	var total time.Duration
	for _, wait := range renameBackoff {
		total += wait
	}
	if total > time.Second {
		t.Fatalf("backoff adds up to %v, want at most 1s", total)
	}

	backoff := []time.Duration{time.Millisecond, 2 * time.Millisecond}
	for attempt, want := range backoff {
		wait, retry := nextRenameWait(backoff, attempt, true)
		if !retry || wait != want {
			t.Fatalf("busy attempt %d: got (%v, %v), want (%v, true)", attempt, wait, retry, want)
		}
	}
	if _, retry := nextRenameWait(backoff, len(backoff), true); retry {
		t.Fatal("a busy failure after the last wait must not be retried")
	}
	for attempt := 0; attempt <= len(backoff); attempt++ {
		if _, retry := nextRenameWait(backoff, attempt, false); retry {
			t.Fatalf("a non-busy failure at attempt %d must not be retried", attempt)
		}
	}
}
