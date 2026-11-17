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

// WorkerProcess is a worker process this process started. A goroutine reaps
// it, so Exited answers without leaving a zombie behind.
type WorkerProcess struct {
	pid  int
	done chan struct{}
}

// PID is the operating system process ID of the worker.
func (p *WorkerProcess) PID() int { return p.pid }

// Exited reports whether the worker process has ended.
func (p *WorkerProcess) Exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// StartWorker starts `<executable> worker` detached for home. The worker is
// independent of this process; a second worker exits on the lock.
func StartWorker(home, executable string) (*WorkerProcess, error) {
	log, err := fs.OpenAppendFile(home, filepath.Join(home, "worker.log"))
	if err != nil {
		return nil, err
	}
	defer log.Close()
	cmd := exec.Command(executable, "worker")
	cmd.Env = append(os.Environ(), EnvClientHome+"="+home)
	cmd.Stdout, cmd.Stderr = log.File, log.File
	process.Detach(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &WorkerProcess{pid: cmd.Process.Pid, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(p.done)
	}()
	return p, nil
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
	_, err = StartWorker(home, self)
	return err
}
