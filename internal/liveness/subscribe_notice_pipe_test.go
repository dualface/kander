//go:build unix

package liveness

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"

	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/command/commandtest"
)

// The remarks held until the first event of a subscription are written through the bounded
// writer. With stdout and stderr on one full pipe, and the subscription's writer holding the
// shared description non-blocking, a plain stderr write would fail with EAGAIN and lose them.
func TestHeldStreamRemarksSurviveAFullMergedPipe(t *testing.T) {
	for _, bound := range []bool{true, false} {
		reader, writer := fullSubscriptionPipe(t)
		// Stderr is opened while the description is still blocking, like the descriptor 2 of a
		// process; the subscription's writer then makes the shared description non-blocking.
		duplicate, err := unix.Dup(int(writer.Fd()))
		if err != nil {
			t.Fatal(err)
		}
		merged := os.NewFile(uintptr(duplicate), "merged-stderr")
		_, release, err := subscriptionFileWriter(writer)
		if err != nil {
			t.Fatal(err)
		}
		c := command.Streaming(&cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error {
			if bound {
				defer bindStreamNotices(context.Background())()
			}
			os.Stderr = merged
			command.Notify("held", "the version differs")
			command.StreamStarted()
			return nil
		}})
		collected := make(chan string, 1)
		go func() {
			time.Sleep(150 * time.Millisecond)
			var buf bytes.Buffer
			deadline := time.Now().Add(1500 * time.Millisecond)
			chunk := make([]byte, 4096)
			_ = reader.SetReadDeadline(deadline)
			for {
				n, err := reader.Read(chunk)
				buf.Write(chunk[:n])
				if err != nil || strings.Contains(buf.String(), "the version differs") {
					break
				}
			}
			collected <- buf.String()
		}()
		commandtest.Capture(func() int { return commandtest.Run(c, "--json") })
		got := <-collected
		_ = release()
		_ = merged.Close()
		if has := strings.Contains(got, `"message":"the version differs"`); has != bound {
			t.Fatalf("bounded writer=%v: the remark arrived=%v", bound, has)
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(reader, 0))
	}
}

// A held remark written after the first event stops with the subscription: with the reader
// paused and the pipe full, canceling the subscription ends the write at once instead of
// after the write limit.
func TestHeldStreamRemarkWriteEndsWithTheSubscriptionContext(t *testing.T) {
	_, writer := fullSubscriptionPipe(t)
	duplicate, err := unix.Dup(int(writer.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	merged := os.NewFile(uintptr(duplicate), "merged-stderr")
	defer merged.Close()
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan time.Time, 1)
	finished := make(chan time.Duration, 1)
	c := command.Streaming(&cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error {
		defer bindStreamNotices(ctx)()
		os.Stderr = merged
		command.Notify("held", "the version differs")
		started <- time.Now()
		command.StreamStarted()
		finished <- time.Since(<-started)
		return nil
	}})
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	commandtest.Capture(func() int { return commandtest.Run(c, "--json") })
	if took := <-finished; took > subscriptionWriteTimeout/2 {
		t.Fatalf("the remark write outlived the canceled subscription: %s", took)
	}
}
