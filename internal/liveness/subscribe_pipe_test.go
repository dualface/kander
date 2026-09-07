package liveness

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

func fullSubscriptionPipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close(); writer.Close() })
	adapter, cleanup, err := subscriptionFileWriter(writer)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err = adapter.WriteContext(ctx, bytes.Repeat([]byte("x"), 4<<20))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("pipe did not block: %v", err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	return reader, writer
}

// Reverse the old audit using a real full pipe, without a rescue reader.
func TestAuditBlockedWriterIgnoresStop(t *testing.T) {
	root, opts, _ := factsMember(t)
	_, writer := fullSubscriptionPipe(t)
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- Subscribe(root, opts, writer, stop) }()
	time.Sleep(20 * time.Millisecond)
	started := time.Now()
	close(stop)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(400 * time.Millisecond):
		t.Fatal("blocked writer survived stop")
	}
	t.Logf("full pipe cancellation and writer join: %s", time.Since(started))
}

func TestSubscriptionPipeDeadlineAndFailure(t *testing.T) {
	for _, mode := range []string{"deadline", "broken", "caller-context"} {
		t.Run(mode, func(t *testing.T) {
			root, opts, _ := factsMember(t)
			opts.Heartbeat = 30
			reader, writer := fullSubscriptionPipe(t)
			if mode == "broken" {
				reader.Close()
			}
			ctx := context.Background()
			cancel := func() {}
			if mode == "caller-context" {
				ctx, cancel = context.WithTimeout(ctx, 50*time.Millisecond)
			}
			defer cancel()
			started := time.Now()
			err := SubscribeContext(ctx, root, opts, writer)
			if err == nil {
				t.Fatal("output failure reported success")
			}
			if mode != "broken" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("deadline lost: %v", err)
			}
			bound := subscriptionWriteTimeout + 500*time.Millisecond
			if mode != "deadline" {
				bound = 500 * time.Millisecond
			}
			if time.Since(started) > bound {
				t.Fatalf("exit exceeded %s: %v", bound, err)
			}
		})
	}
}

func TestSubscriptionSlowConsumerPreservesLines(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	adapter, cleanup, err := subscriptionFileWriter(writer)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := newSubscriptionOutput(ctx, adapter)
	line := append(bytes.Repeat([]byte("x"), 8191), '\n')
	received := make(chan []byte, 1)
	go func() {
		var out bytes.Buffer
		buf := make([]byte, 1024)
		for out.Len() < 4*len(line) {
			n, err := reader.Read(buf)
			if n > 0 {
				out.Write(buf[:n])
			}
			if err != nil {
				break
			}
			time.Sleep(time.Millisecond)
		}
		received <- out.Bytes()
	}()
	for n := 0; n < 4; n++ {
		if _, err := q.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.finish(); err != nil {
		t.Fatal(err)
	}
	select {
	case data := <-received:
		if !bytes.Equal(data, bytes.Repeat(line, 4)) {
			t.Fatal("slow consumer lost or reordered bytes")
		}
	case <-time.After(time.Second):
		reader.Close()
		<-received
		t.Fatal("slow consumer did not finish")
	}
}

func TestSubscriptionShortWriteFails(t *testing.T) {
	q := newSubscriptionOutput(context.Background(), contextWriteFunc(func(_ context.Context, p []byte) (int, error) { return len(p) - 1, nil }))
	if _, err := q.Write([]byte("line\n")); err != nil {
		t.Fatal(err)
	}
	if err := q.finish(); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write=%v", err)
	}
}

func TestSubscriptionBlockedDiagnosticHasDeadline(t *testing.T) {
	_, writer := fullSubscriptionPipe(t)
	started := time.Now()
	err := writeSubscriptionDiagnostic(writer, "subscription output failed")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("diagnostic deadline=%v", err)
	}
	if time.Since(started) > subscriptionWriteTimeout+500*time.Millisecond {
		t.Fatal("diagnostic blocked exit")
	}
}
