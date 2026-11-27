package board

import (
	"context"
	"errors"
	"os"

	"github.com/dualface/kander/v2/internal/fs"
)

// The entrances in this file serve maintenance that must never wait for a
// board writer, such as the server gc: a held lock ends the call at once with
// board.lock_busy, and the caller skips the work and reports it. Every other
// caller keeps the waiting entrances (Scan, RequireNoPendingOperations).

// noWaitKey marks a context whose shared lock acquisitions try each lock once.
// Only this file sets it, so contexts of other callers never carry it.
type noWaitKey struct{}

func noWait(ctx context.Context) bool { return ctx.Value(noWaitKey{}) != nil }

func noWaitContext() context.Context {
	return context.WithValue(context.Background(), noWaitKey{}, true)
}

// trySharedOnce takes the shared lock of file without waiting, or fails with
// board.lock_busy naming path.
func trySharedOnce(file *os.File, path string) (*fs.ExclusiveLock, error) {
	lock, held, err := fs.TryLockShared(file)
	if err == nil && !held {
		err = kanbanError("board.lock_busy", path)
	}
	return lock, err
}

// ScanNoWait captures the same committed view as Scan, but tries each lock of
// the scan (the board lock, every card lock, the journal lock) once. When one
// is held, it releases the locks it took and fails with board.lock_busy.
func ScanNoWait(root string) (Board, error) {
	return scanContext(noWaitContext(), root, nil, false)
}

// RequireNoPendingOperationsNoWait is RequireNoPendingOperations that tries
// the journal lock once and fails with board.lock_busy when it is held.
func RequireNoPendingOperationsNoWait(root string) error {
	var journal lockSet
	if err := journal.takeSharedContext(noWaitContext(), root, control(root, "locks", "journal.lock")); err != nil {
		return err
	}
	records, err := readPendingRecords(root)
	if err = errors.Join(err, journal.close()); err != nil {
		return err
	}
	for _, r := range records {
		if r.Phase == "prepared" {
			return kanbanError("board.transaction_pending", r.ID)
		}
	}
	return nil
}

// LockBusy reports an error that a lock another process holds caused in an
// entrance that does not wait (board.lock_busy).
func LockBusy(err error) bool {
	var failure *Error
	return errors.As(err, &failure) && failure.Code == "board.lock_busy"
}
