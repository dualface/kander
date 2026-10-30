package process

import (
	"context"
	"errors"
	"strconv"
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

	// bootClockTolerance bounds the drift of a boot time derived from the wall
	// clock. Widening it costs nothing: after a reboot the exact start time of a
	// process cannot match, so only the reason of the stopped result differs.
	bootClockTolerance = 300
)

// ObserveSettled waits until a new process stops changing. A shim execs the real
// binary, so the first reading names the interpreter. At the limit it returns the
// latest reading: a slower shim is judged by the first exec it finished.
func ObserveSettled(ctx context.Context, pid int) (ProcessFacts, error) {
	limit, cancel := context.WithTimeout(ctx, settleLimit)
	defer cancel()
	latest, err := ObserveProcess(limit, pid)
	if err != nil {
		return ProcessFacts{}, err
	}
	since := time.Now()
	ticker := time.NewTicker(settleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-limit.Done():
			return latest, ctx.Err()
		case <-ticker.C:
		}
		next, err := ObserveProcess(limit, pid)
		if err != nil {
			if limit.Err() != nil && ctx.Err() == nil {
				return latest, nil
			}
			return ProcessFacts{}, err
		}
		if next != latest {
			latest, since = next, time.Now()
		} else if time.Since(since) >= settleQuiet {
			return latest, nil
		}
	}
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
