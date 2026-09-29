//go:build unix

package terminal

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestMatchAgentSessionSpecialFiles(t *testing.T) {
	resetLanguage(t)
	writeTestConfig(t)
	ctx := context.Background()
	fifo := filepath.Join(t.TempDir(), "fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Skip(err)
	}
	done := make(chan SessionMatch, 1)
	go func() {
		got, _ := MatchAgentSession(ctx, "pi", "path", fifo, "s1")
		done <- got
	}()
	select {
	case got := <-done:
		if got != SessionUncertain {
			t.Fatalf("verdict=%v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("FIFO open blocked past the bound")
	}
}
