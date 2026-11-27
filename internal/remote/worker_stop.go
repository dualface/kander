package remote

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"time"

	"github.com/dualface/kander/v2/internal/fs"
)

// WorkerStopName is the stop request file of a client home. kander uninstall writes it next to
// worker.lock; the worker that holds the lock reads it on every tick, exits through its normal
// shutdown, and deletes it before it releases the lock. The request replaces a signal: no
// process is signaled, and it behaves the same on every platform.
const WorkerStopName = "worker.stop"

// workerStopSchema is the only schema of the stop request.
const workerStopSchema = 1

// workerStopRequest is the content of WorkerStopName.
type workerStopRequest struct {
	Schema    int       `json:"schema"`
	CreatedAt time.Time `json:"created_at"`
}

// WriteWorkerStopRequest writes the stop request of home atomically, created at the given time.
func WriteWorkerStopRequest(home string, createdAt time.Time) error {
	data, err := json.Marshal(workerStopRequest{Schema: workerStopSchema, CreatedAt: createdAt.UTC()})
	if err != nil {
		return err
	}
	return fs.WriteTextAtomic(home, filepath.Join(home, WorkerStopName), string(data)+"\n", true)
}

// ReadWorkerStopRequest reads the stop request of home. found is false when there is none. A
// file that is not a request of the known schema is found with a zero time, so a worker treats
// it as older than itself and deletes it.
func ReadWorkerStopRequest(home string) (createdAt time.Time, found bool, err error) {
	data, exists, err := fs.ReadRegularFileIfExists(home, filepath.Join(home, WorkerStopName))
	if err != nil || !exists {
		return time.Time{}, false, err
	}
	var request workerStopRequest
	if json.Unmarshal(data, &request) != nil || request.Schema != workerStopSchema {
		return time.Time{}, true, nil
	}
	return request.CreatedAt, true, nil
}

// RemoveWorkerStopRequest deletes the stop request of home, if there is one.
func RemoveWorkerStopRequest(home string) error {
	_, err := fs.RemoveRegularFileIfExists(home, filepath.Join(home, WorkerStopName))
	return err
}

// WorkerLockHeld reports whether a worker holds worker.lock of home without creating the lock
// file: a missing lock file means no worker runs.
func WorkerLockHeld(home string) (bool, error) {
	return workerLockHeld(home)
}

// ErrWorkerStopTimeout reports that worker.lock was still held when the wait for a stopped
// worker ended.
var ErrWorkerStopTimeout = errors.New("the worker still holds worker.lock")

// StopWorker asks the worker of home to exit and waits until worker.lock is free, probing every
// poll until timeout. It writes the stop request only while the lock is held, so a home without
// a worker is left untouched. A request the worker did not delete is removed once the lock is
// free; after a timeout it stays, so the worker still exits when it reads it. It never signals a
// process.
func StopWorker(ctx context.Context, home string, poll, timeout time.Duration) (stopped bool, err error) {
	held, err := workerLockHeld(home)
	if err != nil || !held {
		return false, err
	}
	if err := WriteWorkerStopRequest(home, time.Now()); err != nil {
		return false, err
	}
	wait, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-wait.Done():
			return false, ErrWorkerStopTimeout
		case <-ticker.C:
		}
		held, err := workerLockHeld(home)
		if err != nil {
			return false, err
		}
		if !held {
			// A worker that went idle and exited before it read the request leaves the file
			// behind; the request has served its purpose either way.
			return true, RemoveWorkerStopRequest(home)
		}
	}
}
