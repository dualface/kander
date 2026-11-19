package remote

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

// WorkerRun says whether a worker runs for a client home.
type WorkerRun int

const (
	// WorkerNoHome means the client directory does not exist.
	WorkerNoHome WorkerRun = iota
	// WorkerStopped means no worker holds worker.lock.
	WorkerStopped
	// WorkerRunning means a worker holds worker.lock.
	WorkerRunning
)

// WorkerStatus is what a read-only look at a client home found. The record is
// read only while a worker holds the lock, because only then does it describe
// the process that runs.
type WorkerStatus struct {
	Run         WorkerRun
	Record      WorkerRecord
	RecordState WorkerRecordState
}

// InspectWorker looks at the worker of home without creating or changing
// anything: no client directory, no lock file, no record. A lock file that is
// missing, or deleted while the probe runs, means no worker runs.
func InspectWorker(home string) (WorkerStatus, error) {
	if _, err := os.Stat(home); os.IsNotExist(err) {
		return WorkerStatus{Run: WorkerNoHome}, nil
	} else if err != nil {
		return WorkerStatus{}, err
	}
	held, err := workerLockHeld(home)
	if err != nil {
		return WorkerStatus{}, err
	}
	if !held {
		return WorkerStatus{Run: WorkerStopped}, nil
	}
	return heldWorkerStatus(home), nil
}

// workerLockHeld is WorkerHeld for a probe that must not create the lock
// file: it opens only an existing one, and an absent one is not held. Opening
// and the absence answer are one step, so no interleaving with a delete can
// leave a new lock file behind.
func workerLockHeld(home string) (bool, error) {
	file, err := fs.OpenExistingLockFile(home, filepath.Join(home, WorkerLockName))
	if err != nil {
		return false, err
	}
	if file == nil {
		return false, nil
	}
	defer file.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	lock, err := fs.LockExclusiveContext(ctx, file)
	if err != nil {
		return true, nil
	}
	return false, lock.Unlock()
}

// heldWorkerStatus reads the record of a worker known to hold the lock. A read
// error leaves the record unknown: the record only serves diagnostics.
func heldWorkerStatus(home string) WorkerStatus {
	record, state, _ := ReadWorkerRecord(home)
	return WorkerStatus{Run: WorkerRunning, Record: record, RecordState: state}
}

// WorkerFinding is the judgement of a running worker against this binary.
type WorkerFinding int

const (
	// WorkerFindingNone means nothing to report: the versions are the same, or
	// the worker is not running.
	WorkerFindingNone WorkerFinding = iota
	// WorkerFindingMismatch means the worker records another version.
	WorkerFindingMismatch
	// WorkerFindingUnrecorded means the worker has no record file.
	WorkerFindingUnrecorded
	// WorkerFindingUnreadable means a record file exists but cannot be used.
	WorkerFindingUnreadable
)

// Finding compares the status with the version of this binary.
func (s WorkerStatus) Finding(selfVersion string) WorkerFinding {
	if s.Run != WorkerRunning {
		return WorkerFindingNone
	}
	switch s.RecordState {
	case WorkerRecordMissing:
		return WorkerFindingUnrecorded
	case WorkerRecordUnknown:
		return WorkerFindingUnreadable
	}
	if s.Record.Version == selfVersion {
		return WorkerFindingNone
	}
	return WorkerFindingMismatch
}

// MismatchText is the warning for a worker that runs another version
// than this binary. It names both versions, the worker pid and both
// executables, and one of two ways out: wait for the handover, or stop the
// worker. It is localized with the configured language.
func (s WorkerStatus) MismatchText(selfVersion, selfExecutable string) string {
	id := "remote.worker_version_mismatch_restart"
	if s.Record.Handover && samePath(s.Record.Executable, selfExecutable) {
		id = "remote.worker_version_mismatch_handover"
	}
	return config.Text(id,
		s.Record.Version, selfVersion, strconv.Itoa(s.Record.PID),
		orNA(s.Record.Executable), orNA(selfExecutable), config.Text("remote.worker_stop_consequence"))
}

// WorkerUnrecordedText is the hint for a running worker without a usable
// record: an older worker that cannot hand over to this binary.
func WorkerUnrecordedText(home string) string {
	return config.Text("remote.worker_version_unrecorded", home, config.Text("remote.worker_stop_consequence"))
}

// samePath compares two executable paths after cleaning; Windows paths ignore
// case. An empty path never matches.
func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func orNA(path string) string {
	if path == "" {
		return "N/A"
	}
	return path
}
