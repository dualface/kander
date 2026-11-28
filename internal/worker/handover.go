package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/internal/probe"
	"github.com/dualface/kander/v2/internal/process"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/version"
)

const (
	// probeTimeout bounds the `version` probe of a replaced binary.
	probeTimeout = 10 * time.Second
	// confirmTimeout bounds the wait for a started worker to take the lock.
	// Renewals pause between the stop of the renewal loop and the confirmation,
	// not during the probe; confirmation alone stays far below the default
	// lease TTL of 90 s.
	confirmTimeout = 10 * time.Second
	// confirmPoll is the interval of the lock check. It is longer than the
	// time WorkerHeld holds the lock, so polling never starves the new worker.
	confirmPoll = 250 * time.Millisecond
	// handoverBackoff is how long a file identity that failed the probe or the
	// handover is left alone.
	handoverBackoff = time.Minute
)

// Successor is a worker process started by a handover.
type Successor interface {
	PID() int
	// Exited reports whether the process has ended.
	Exited() bool
}

// workerLock is the exclusive worker.lock of one client home. It can be
// released and taken again, which a handover needs.
type workerLock struct {
	home string
	file *os.File
	lock *fs.ExclusiveLock
}

// acquire takes the lock, waiting at most 100 ms; it returns ErrRunning when
// another process holds it.
func (l *workerLock) acquire(ctx context.Context) error {
	file, err := fs.OpenLockFile(l.home, filepath.Join(l.home, remote.WorkerLockName))
	if err != nil {
		return err
	}
	lockCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	lock, err := fs.LockExclusiveContext(lockCtx, file)
	if err != nil {
		file.Close()
		return ErrRunning
	}
	l.file, l.lock = file, lock
	return nil
}

// release unlocks and closes the lock file; calling it again does nothing.
func (l *workerLock) release() {
	if l.file == nil {
		return
	}
	_ = l.lock.Unlock()
	_ = l.file.Close()
	l.file, l.lock = nil, nil
}

// handover tracks the executable this worker started from and the file
// identities that were already tried without success.
type handover struct {
	executable string
	identity   os.FileInfo
	failed     os.FileInfo
	failedAt   time.Time
}

// defaultHandover fills the handover fields that tests leave unset.
func (o *Options) defaultHandover() {
	if o.Executable == nil {
		o.Executable = os.Executable
	}
	if o.Identity == nil {
		o.Identity = fileIdentity
	}
	if o.Probe == nil {
		o.Probe = func(ctx context.Context, executable string) error {
			return probeExecutable(ctx, executable, probeTimeout)
		}
	}
	if o.Spawn == nil {
		managed := o.Managed
		o.Spawn = func(home, executable string) (Successor, error) { return startSuccessor(home, executable, managed) }
	}
	if o.Held == nil {
		o.Held = remote.WorkerHeld
	}
	if o.ConfirmTimeout <= 0 {
		o.ConfirmTimeout = confirmTimeout
	}
	if o.ConfirmPoll <= 0 {
		o.ConfirmPoll = confirmPoll
	}
}

// newHandover records the executable and its identity. A worker that cannot
// read either runs without handover.
func newHandover(o Options) (*handover, error) {
	executable, err := o.Executable()
	if err != nil {
		return nil, err
	}
	identity, err := o.Identity(executable)
	if err != nil {
		return &handover{executable: executable}, err
	}
	return &handover{executable: executable, identity: identity}, nil
}

// fileIdentity reads the identity of path through an open handle. A handle
// pins the file ID when it is read; on Windows a path-only FileInfo reads it at
// the first comparison, by when the path may already name the new file.
func fileIdentity(path string) (os.FileInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return file.Stat()
}

// probeExecutable runs `<executable> version` within timeout and requires
// exit code 0 and an output that starts with "kander ". The process tree is
// reaped before it returns.
func probeExecutable(ctx context.Context, executable string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := probe.CaptureContext(ctx, executable, command.VersionArguments())
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("version exited with code %d: %s", res.Code, strings.TrimSpace(res.Stderr))
	}
	if !strings.HasPrefix(res.Stdout, "kander ") {
		return fmt.Errorf("version printed %q, not a kander version", strings.TrimSpace(res.Stdout))
	}
	return nil
}

// startSuccessor starts the worker of the replaced binary. The successor of a managed worker
// stays managed: the detached start drops KANDER_MANAGED, so it is passed on explicitly.
func startSuccessor(home, executable string, managed bool) (Successor, error) {
	var env []string
	if managed {
		env = append(env, process.EnvManaged+"=1")
	}
	p, err := remote.StartWorker(home, executable, env...)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// due reports the current identity of the executable when it differs from the
// recorded one and is not inside the backoff of a failed attempt.
func (h *handover) due(o Options) (os.FileInfo, bool) {
	if !h.enabled() {
		return nil, false
	}
	current, err := o.Identity(h.executable)
	if err != nil || os.SameFile(h.identity, current) {
		return nil, false
	}
	if h.failed != nil && os.SameFile(h.failed, current) && o.Now().Sub(h.failedAt) < handoverBackoff {
		return nil, false
	}
	return current, true
}

// path is the recorded executable, empty when it could not be read.
func (h *handover) path() string {
	if h == nil {
		return ""
	}
	return h.executable
}

// enabled reports whether this worker watches its executable for a
// replacement, which needs the identity of the file.
func (h *handover) enabled() bool {
	return h != nil && h.identity != nil
}

func (h *handover) fail(o Options, current os.FileInfo) {
	h.failed, h.failedAt = current, o.Now()
}

// handoverResult tells Serve what to do after a handover attempt.
type handoverResult int

const (
	handoverNone  handoverResult = iota // keep serving; this worker holds the lock
	handoverDone                        // a new worker took over; exit
	handoverTaken                       // another worker holds the lock now; exit
)

// tryHandover hands the work to the replaced binary once. The order is fixed:
// probe, stop the renewal loop, release the lock, start, confirm. Renewals
// continue during the probe and stop before the lock is released, so only the
// worker that holds worker.lock acts on credentials. A failure after the
// release takes the lock back and resumes the renewals, so no lease lapses
// while the old code keeps running.
func (w *loop) tryHandover(ctx context.Context, h *handover, lock *workerLock) (handoverResult, error) {
	current, ok := h.due(w.o)
	if !ok {
		return handoverNone, nil
	}
	log := w.o.Logger.With("executable", h.executable)
	if err := w.o.Probe(ctx, h.executable); err != nil {
		log.Warn("replaced worker binary is not usable; handover skipped", "error", err)
		h.fail(w.o, current)
		return handoverNone, nil
	}
	if !w.settleLostMarks(ctx) {
		log.Warn("a lost lease could not be stopped yet; handover postponed")
		return handoverNone, nil
	}
	lock.release()
	if w.o.ExitOnHandover {
		// The service manager starts the replaced binary once this process exits.
		w.closeRequestFiles()
		log.Info("worker exits for its service manager to start the replaced binary", "old_version", version.String())
		return handoverDone, nil
	}
	next, err := w.o.Spawn(w.o.Home, h.executable)
	if err == nil {
		err = w.confirm(ctx, next)
	}
	if err == nil {
		w.closeRequestFiles()
		log.Info("worker handed over", "old_version", version.String(), "new_pid", next.PID())
		return handoverDone, nil
	}
	log.Error("worker handover failed; continuing with this binary", "error", err)
	h.fail(w.o, current)
	if err := lock.acquire(ctx); errors.Is(err, ErrRunning) {
		// Another worker took the lock meanwhile; it serves this home now.
		return handoverTaken, nil
	} else if err != nil {
		return handoverNone, err
	}
	w.startRenewals(ctx)
	return handoverNone, nil
}

// settleLostMarks stops the agents of the leases marked lost before the
// renewals pause for a handover, so that no stop attempt delays a renewal while
// they are paused. The stops run while the renewal loop runs, until no mark is
// left. Then the loop is stopped, and a mark that came in that last moment sends
// the worker around again with the renewals running: a renewal loop that has
// exited marks nothing more. It reports whether the handover can go on, with
// the renewals stopped. After a few rounds with a mark still left, for example
// because the credentials cannot be read, it reports false and the renewals
// run: the handover waits for the next tick.
func (w *loop) settleLostMarks(ctx context.Context) bool {
	stopped := map[credentialKey]struct{}{}
	for range 3 {
		for w.stopMarked(ctx, stopped) {
		}
		w.stopRenewals()
		if !w.marked() {
			return true
		}
		w.startRenewals(ctx)
	}
	return false
}

// confirm waits until another process holds worker.lock. A started process
// that already exited cannot take it, for example a build of the same path
// that has no worker command.
func (w *loop) confirm(ctx context.Context, next Successor) error {
	timer := time.NewTimer(w.o.ConfirmTimeout)
	defer timer.Stop()
	poll := time.NewTicker(w.o.ConfirmPoll)
	defer poll.Stop()
	for {
		held, err := w.o.Held(w.o.Home)
		if err != nil {
			return err
		}
		if held {
			return nil
		}
		if next.Exited() {
			return errors.New("the started worker exited before it took the lock")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return errors.New("the started worker did not take the lock in time")
		case <-poll.C:
		}
	}
}
