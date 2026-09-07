package fs

import (
	"context"
	"os"
	"time"
)

// LockSharedContext waits for a shared lock without leaving a blocked worker.
// Cancellation bounds lock contention; opening files and kernel I/O remain OS-bound.
func LockSharedContext(ctx context.Context, file *os.File) (*ExclusiveLock, error) {
	return lockContext(ctx, file, true)
}

// LockExclusiveContext cancels contention without leaving a blocked worker.
func LockExclusiveContext(ctx context.Context, file *os.File) (*ExclusiveLock, error) {
	return lockContext(ctx, file, false)
}

func lockContext(ctx context.Context, file *os.File, shared bool) (*ExclusiveLock, error) {
	if file == nil {
		return nil, wrap("lock", "", os.ErrInvalid)
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		locked, err := tryContextLock(file, shared)
		if err != nil {
			return nil, wrap("lock", file.Name(), err)
		}
		if locked {
			return &ExclusiveLock{file: file}, nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
