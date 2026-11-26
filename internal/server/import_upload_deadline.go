package server

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"
)

// uploadReceive enforces the read deadlines of one upload request: an
// absolute receive deadline, and while file bodies arrive, an idle limit
// between two reads. Once stopped (server shutdown) or finished, no later read or
// extension moves the deadline again.
type uploadReceive struct {
	rc       *http.ResponseController
	cancel   context.CancelFunc
	mu       sync.Mutex
	timer    *time.Timer
	absolute time.Time
	idle     time.Duration
	stopped  bool
}

// newUploadReceive applies the absolute deadline to the connection and
// cancels the request context when it passes.
func newUploadReceive(rc *http.ResponseController, cancel context.CancelFunc, absolute time.Time) (*uploadReceive, error) {
	u := &uploadReceive{rc: rc, cancel: cancel, absolute: absolute}
	if err := rc.SetReadDeadline(absolute); err != nil {
		return nil, err
	}
	u.timer = time.AfterFunc(time.Until(absolute), cancel)
	return u, nil
}

// extend moves the absolute deadline, once the manifest declared its bytes.
func (u *uploadReceive) extend(absolute time.Time) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.stopped {
		return
	}
	u.absolute = absolute
	u.timer.Reset(time.Until(absolute))
	u.applyLocked()
}

// watchIdle starts (idle > 0) or ends (idle == 0) the idle limit.
func (u *uploadReceive) watchIdle(idle time.Duration) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.idle = idle
	u.applyLocked()
}

// touch records that bytes arrived.
func (u *uploadReceive) touch() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.idle > 0 {
		u.applyLocked()
	}
}

// stop ends the receive at once.
func (u *uploadReceive) stop() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.stopped = true
	u.cancel()
	_ = u.rc.SetReadDeadline(time.Now())
}

// finish clears the deadlines once nothing more is read from the request.
func (u *uploadReceive) finish() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.stopped = true
	u.timer.Stop()
	_ = u.rc.SetReadDeadline(time.Time{})
}

func (u *uploadReceive) applyLocked() {
	if u.stopped {
		return
	}
	deadline := u.absolute
	if idle := time.Now().Add(u.idle); u.idle > 0 && idle.Before(deadline) {
		deadline = idle
	}
	_ = u.rc.SetReadDeadline(deadline)
}

// uploadBodyReader reports every read that returned bytes.
type uploadBodyReader struct {
	r       io.Reader
	receive *uploadReceive
}

func (b uploadBodyReader) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if n > 0 {
		b.receive.touch()
	}
	return n, err
}
