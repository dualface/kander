package liveness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const (
	subscriptionQueueSize    = 16
	subscriptionMaxLine      = 1 << 20
	subscriptionWriteTimeout = 2 * time.Second
)

// ContextWriter must return after cancellation and retain no asynchronous writes.
// Subscribe owns its writes exclusively until it returns. Wrapping an arbitrary
// blocking Write in a goroutine does not satisfy this contract.
type ContextWriter interface {
	WriteContext(context.Context, []byte) (int, error)
}

type contextWriteFunc func(context.Context, []byte) (int, error)

func (f contextWriteFunc) WriteContext(ctx context.Context, p []byte) (int, error) { return f(ctx, p) }

type queuedLine struct {
	data     []byte
	deadline time.Time
}

type subscriptionOutputQueue struct {
	ctx    context.Context
	cancel context.CancelFunc
	lines  chan queuedLine
	done   chan struct{}
	err    error // Read only after done closes.
}

func subscriptionWriter(w io.Writer) (ContextWriter, func() error, error) {
	cleanup := func() error { return nil }
	if writer, ok := w.(ContextWriter); ok {
		return writer, cleanup, nil
	}
	if file, ok := w.(*os.File); ok {
		return subscriptionFileWriter(file)
	}
	switch w.(type) {
	case *bytes.Buffer, *strings.Builder:
		return contextWriteFunc(func(ctx context.Context, p []byte) (int, error) {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			return w.Write(p)
		}), cleanup, nil
	}
	if w == io.Discard {
		return contextWriteFunc(func(ctx context.Context, p []byte) (int, error) { return len(p), ctx.Err() }), cleanup, nil
	}
	return nil, cleanup, fmt.Errorf("%s", t("liveness.subscription_writer_unsupported"))
}

func newSubscriptionOutput(ctx context.Context, writer ContextWriter) *subscriptionOutputQueue {
	ctx, cancel := context.WithCancel(ctx)
	out := &subscriptionOutputQueue{ctx: ctx, cancel: cancel, lines: make(chan queuedLine, subscriptionQueueSize), done: make(chan struct{})}
	go func() {
		defer close(out.done)
		for line := range out.lines {
			writeCtx, stop := context.WithDeadline(ctx, line.deadline)
			n, err := writer.WriteContext(writeCtx, line.data)
			if err == nil {
				err = writeCtx.Err()
			}
			stop()
			if err == nil && n != len(line.data) {
				err = io.ErrShortWrite
			}
			if err != nil {
				out.err = fmt.Errorf("%s: %w", t("liveness.subscription_output_failed"), err)
				return
			}
		}
	}()
	return out
}

func (o *subscriptionOutputQueue) Write(p []byte) (int, error) {
	if len(p) > subscriptionMaxLine {
		return 0, fmt.Errorf("%s", t("liveness.subscription_line_too_large", subscriptionMaxLine))
	}
	select {
	case <-o.ctx.Done():
		return 0, o.ctx.Err()
	case <-o.done:
		return 0, o.err
	default:
	}
	line := queuedLine{data: append([]byte(nil), p...), deadline: time.Now().Add(subscriptionWriteTimeout)}
	select {
	case o.lines <- line:
		return len(p), nil
	default:
		o.cancel()
		return 0, fmt.Errorf("%s", t("liveness.subscription_queue_full", subscriptionQueueSize))
	}
}

// finish joins the sole writer. Pending lines share their enqueue-time deadlines.
func (o *subscriptionOutputQueue) finish() error {
	close(o.lines)
	<-o.done
	o.cancel()
	return o.err
}

func subscriptionResult(ctx context.Context, runErr, outputErr error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errors.Join(runErr, outputErr)
}
