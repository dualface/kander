package remote

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/process"
)

// WorkerLockName is the lock one worker per client home holds while it runs.
const WorkerLockName = "worker.lock"

// WorkerHeld reports whether a worker holds the lock of home.
func WorkerHeld(home string) (bool, error) {
	file, err := fs.OpenLockFile(home, filepath.Join(home, WorkerLockName))
	if err != nil {
		return false, err
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

// EnsureWorker starts the background worker of home unless one runs. The
// worker is a detached `kander worker` process, independent of the TUI and
// of the command that started it; a second worker exits on the lock.
func EnsureWorker(home string) error {
	if held, err := WorkerHeld(home); err != nil || held {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	log, err := fs.OpenAppendFile(home, filepath.Join(home, "worker.log"))
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(self, "worker")
	cmd.Env = append(os.Environ(), EnvClientHome+"="+home)
	cmd.Stdout, cmd.Stderr = log.File, log.File
	process.Detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
