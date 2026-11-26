package client

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// importStreamIdle is how long a streamed import may stay silent before the
// client treats the connection as lost. The server repeats its progress at
// least once per second. Tests shorten it.
var importStreamIdle = 2 * time.Minute

// errImportIdle ends a streamed import that stopped sending lines.
var errImportIdle = errors.New("lost the connection: no line from the server")

// importWatch cancels an import request that waits too long: first under a
// fixed deadline (the response headers, or the archive being sent), then,
// once a streamed response arrives, for longer than importStreamIdle
// between two lines.
type importWatch struct {
	cancel context.CancelCauseFunc
	mu     sync.Mutex
	timer  *time.Timer
	idle   time.Duration
}

func newImportWatch(ctx context.Context, limit time.Duration) (context.Context, *importWatch) {
	ctx, cancel := context.WithCancelCause(ctx)
	w := &importWatch{cancel: cancel}
	w.timer = time.AfterFunc(limit, func() { cancel(context.DeadlineExceeded) })
	return ctx, w
}

// limit moves the fixed deadline, measured from start. It has no effect
// once the stream is watched.
func (w *importWatch) limit(start time.Time, limit time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.idle == 0 {
		w.timer.Reset(time.Until(start.Add(limit)))
	}
}

// stream switches to the idle limit for a streamed response.
func (w *importWatch) stream() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.idle = importStreamIdle
	w.timer.Stop()
	w.timer = time.AfterFunc(w.idle, func() { w.cancel(errImportIdle) })
}

// alive records one line of the stream.
func (w *importWatch) alive() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.idle > 0 {
		w.timer.Reset(w.idle)
	}
}

func (w *importWatch) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.timer.Stop()
	w.cancel(nil)
}

// failure names the watch as the cause of a transport error it caused.
func (w *importWatch) failure(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); errors.Is(cause, errImportIdle) {
		return &TransportError{Err: fmt.Errorf("%w for %s; the server may still complete the import, run the same command again", errImportIdle, importStreamIdle)}
	}
	return err
}
