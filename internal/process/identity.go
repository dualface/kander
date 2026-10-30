package process

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Errors of a process observation. Anything else, including a context error, is
// a failed observation that proves nothing about the process.
var (
	// ErrProcessGone: no running process has this ID. A zombie counts as gone.
	ErrProcessGone = errors.New("process does not exist")
	// ErrProcessDenied: the platform refused the observation.
	ErrProcessDenied = errors.New("process observation denied")
	// ErrProcessUnsupported: this platform has no process identity.
	ErrProcessUnsupported = errors.New("process identity is unavailable on this platform")
	// ErrProcessReplaced: the ID now names another process than the one observed
	// first. The started process has exited.
	ErrProcessReplaced = errors.New("process was replaced by a later process")
	// ErrProcessUnsettled: the process kept changing up to the observation limit.
	ErrProcessUnsettled = errors.New("process did not settle")
)

// ProcessFacts are kernel facts of one process. Start is a platform token that
// only compares for equality. Executable is a base name, never a command line.
type ProcessFacts struct {
	PID        int
	ParentPID  int
	Start      string
	Executable string
}

const (
	settleInterval = 100 * time.Millisecond
	settleQuiet    = 500 * time.Millisecond
	settleLimit    = 3 * time.Second
	readyInterval  = 500 * time.Millisecond

	// bootClockTolerance bounds the drift of a boot time derived from the wall
	// clock. Widening it costs nothing: after a reboot the exact start time of a
	// process cannot match, so only the reason of the stopped result differs.
	bootClockTolerance = 300

	// commLimit is the length a platform may truncate an executable name to.
	commLimit = 16
)

// incarnation binds observations to one started process. A process ID alone can
// name a later process, so every sample is read first and then checked against the
// start handle: until the handle reports an exit, the ID cannot have been reused.
type incarnation struct {
	pid     int
	start   string
	running func() bool
	observe func(context.Context, int) (ProcessFacts, error)
}

func (i *incarnation) sample(ctx context.Context) (ProcessFacts, error) {
	facts, err := i.observe(ctx, i.pid)
	if err != nil {
		return ProcessFacts{}, err
	}
	if !i.running() {
		return ProcessFacts{}, ErrProcessGone
	}
	if i.start == "" {
		i.start = facts.Start
	}
	if facts.Start != i.start {
		return ProcessFacts{}, ErrProcessReplaced
	}
	return facts, nil
}

// ObserveSettled reads a new process once it stopped changing for a short time. A
// shim execs the real binary, so an early reading can name an interpreter. Quiet
// does not prove the exec happened: only ImageConfirmed says the result is final.
// running reports whether the handle that started the process still holds it.
func ObserveSettled(ctx context.Context, pid int, running func() bool) (ProcessFacts, error) {
	return settle(ctx, &incarnation{pid: pid, running: running, observe: ObserveProcess})
}

func settle(ctx context.Context, watched *incarnation) (ProcessFacts, error) {
	limit, cancel := context.WithTimeout(ctx, settleLimit)
	defer cancel()
	latest, err := watched.sample(limit)
	if err != nil {
		return ProcessFacts{}, err
	}
	since := time.Now()
	ticker := time.NewTicker(settleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-limit.Done():
			if ctx.Err() != nil {
				return ProcessFacts{}, ctx.Err()
			}
			return ProcessFacts{}, ErrProcessUnsettled
		case <-ticker.C:
		}
		next, err := watched.sample(limit)
		if err != nil {
			return ProcessFacts{}, err
		}
		if next != latest {
			latest, since = next, time.Now()
		} else if time.Since(since) >= settleQuiet {
			return latest, nil
		}
	}
}

// ObserveWhenReady waits for ready and then reads the same incarnation as first.
// ready must be evidence that the agent itself runs; time is not.
func ObserveWhenReady(ctx context.Context, first ProcessFacts, running, ready func() bool) (ProcessFacts, error) {
	return awaitReady(ctx, &incarnation{pid: first.PID, start: first.Start, running: running, observe: ObserveProcess}, ready)
}

func awaitReady(ctx context.Context, watched *incarnation, ready func() bool) (ProcessFacts, error) {
	ticker := time.NewTicker(readyInterval)
	defer ticker.Stop()
	for {
		// Ready first: the sample that returns must postdate the evidence.
		isReady := ready()
		facts, err := watched.sample(ctx)
		if err != nil {
			return ProcessFacts{}, err
		}
		if isReady {
			return facts, nil
		}
		select {
		case <-ctx.Done():
			return ProcessFacts{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

// ImageConfirmed reports whether facts can be final for the process started from
// program. A platform without exec never changes the image. Elsewhere the kernel
// image must be the started file itself; a script or a wrapper may still exec.
func ImageConfirmed(program string, facts ProcessFacts) bool {
	if !ImageCanChange {
		return true
	}
	base := filepath.Base(program)
	return base == facts.Executable || len(facts.Executable) == commLimit && strings.HasPrefix(base, facts.Executable)
}

// SameBoot compares two boot identifiers. Numeric identifiers are boot times in
// Unix seconds, which move with clock corrections; any other identifier is exact.
func SameBoot(recorded, current string) bool {
	a, errA := strconv.ParseInt(recorded, 10, 64)
	b, errB := strconv.ParseInt(current, 10, 64)
	if errA != nil || errB != nil {
		return recorded == current
	}
	return max(a, b)-min(a, b) <= bootClockTolerance
}
