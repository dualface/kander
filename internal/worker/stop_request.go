package worker

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/dualface/kander/v2/internal/remote"
)

// stopWatch follows the stop request file of the client home (remote.WorkerStopName) that kander
// uninstall writes. A request created after this worker started cancels the serve context, so
// the worker leaves through the same path as a signal: both loops stop, the request file is
// deleted, and the lock is released. A request created before this worker started was left by an
// earlier uninstall that was interrupted; it is deleted and the worker runs on.
type stopWatch struct {
	o       Options
	started time.Time
	seen    atomic.Bool
}

// newStopWatch removes a request older than started. started is taken before the worker waits
// for its lock, so a request written while this worker waited is meant for it.
func newStopWatch(o Options, started time.Time) *stopWatch {
	s := &stopWatch{o: o, started: started}
	s.check()
	return s
}

// check reads the request once and reports whether it asks this worker to stop.
func (s *stopWatch) check() bool {
	created, found, err := remote.ReadWorkerStopRequest(s.o.Home)
	if err != nil {
		s.o.Logger.Warn("the stop request could not be read", "error", err)
		return false
	}
	if !found {
		return false
	}
	if created.After(s.started) {
		return true
	}
	if err := remote.RemoveWorkerStopRequest(s.o.Home); err != nil {
		s.o.Logger.Warn("a stop request older than this worker could not be removed", "error", err)
	} else {
		s.o.Logger.Info("removed a stop request older than this worker", "created_at", created)
	}
	return false
}

// watch checks the request every tick until ctx ends, and calls stop once on a request.
func (s *stopWatch) watch(ctx context.Context, stop context.CancelFunc) {
	ticker := time.NewTicker(s.o.Tick)
	defer ticker.Stop()
	for {
		if s.check() {
			s.seen.Store(true)
			s.o.Logger.Info("stop requested; the worker exits")
			stop()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// finish deletes the request this worker answered. It runs after both loops stopped and before
// the lock is released, so the uninstall waiting on the lock finds the request gone.
func (s *stopWatch) finish() {
	if !s.seen.Load() {
		return
	}
	if err := remote.RemoveWorkerStopRequest(s.o.Home); err != nil {
		s.o.Logger.Error("the stop request could not be removed", "error", err)
	}
}
