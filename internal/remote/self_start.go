package remote

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/servicectl"
)

// A test binary must never start a process on its own: os.Executable is the
// test binary, which runs every test again and may reach the real data
// directory of the user. A test marks the starter it supplied, and only a
// marked starter may run in a test binary.
var (
	serverStarterInjected atomic.Bool
	workerStarterInjected atomic.Bool
)

func serverStartAllowed() bool { return !testing.Testing() || serverStarterInjected.Load() }
func workerStartAllowed() bool { return !testing.Testing() || workerStarterInjected.Load() }

// dataLockHeld probes the data directory lock without waiting and without
// creating it: a missing lock file means no server and no offline command
// ever ran there. The lock is released again at once.
func dataLockHeld(dir string) (bool, error) {
	return lockHeld(dir, filepath.Join(dir, servicectl.DataLockName))
}

// lockHeld reports whether another handle holds the lock file at path.
func lockHeld(root, path string) (bool, error) {
	file, err := fs.OpenExistingLockFile(root, path)
	if err != nil || file == nil {
		return false, err
	}
	defer file.Close()
	lock, taken, err := fs.TryLockExclusive(file)
	if err != nil || !taken {
		return err == nil, err
	}
	return false, lock.Unlock()
}

// startContext is the 30 s budget of a server start. It is independent of the
// deadline of the command, which an earlier wait or a question may have used
// up. With followCancel (a start nobody asked for) an explicit cancellation of
// the command, such as Ctrl+C, ends the start wait as well.
func startContext(ctx context.Context, followCancel bool) (context.Context, context.CancelFunc) {
	fresh, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if !followCancel {
		return fresh, cancel
	}
	stop := context.AfterFunc(ctx, func() {
		if errors.Is(ctx.Err(), context.Canceled) {
			cancel()
		}
	})
	return fresh, func() { stop(); cancel() }
}

// lockGrace is how long a client waits for a server it found the data
// directory lock held by. A server that a concurrent client just started holds
// the lock before it listens, so a held lock alone does not mean a server that
// cannot be reached; the wait is short because a lock held by an offline
// command or by an unreachable server never lets go.
const lockGrace = 750 * time.Millisecond

// awaitServer polls the server until it answers or lockGrace has passed.
// reachable is false when it never answered; err is then meaningless.
func awaitServer(ctx context.Context, c *client.Client) (v api.VersionInfo, reachable bool, err error) {
	// A copy without retries, so one probe does not outlast the grace.
	probe := *c
	probe.Retries = 0
	ctx, cancel := context.WithTimeout(ctx, lockGrace)
	defer cancel()
	for {
		v, err = probe.CheckCompatible(ctx)
		if ctx.Err() != nil {
			return v, false, nil
		}
		if !client.IsUnreachable(err) {
			return v, true, err
		}
		select {
		case <-ctx.Done():
			return v, false, nil
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// dataLockUnavailable is the unreachable error of a start that was refused
// because the data directory lock is held, or could not be probed. Starting
// then would only fail on the lock, and the start wait loop would not notice
// until its 10 s deadline.
func dataLockUnavailable(unavailable error, dir string, probeErr error) error {
	lock := filepath.Join(dir, servicectl.DataLockName)
	if probeErr != nil {
		return command.Coded(CodeUnreachable, errors.New(unavailable.Error()+": "+probeErr.Error()))
	}
	return command.Coded(CodeUnreachable, errors.New(unavailable.Error()+"\n"+config.Text("remote.server_lock_held", lock)))
}

// startCredentialWorker starts the worker of a client home. It is a variable so
// a test can count the starts.
var startCredentialWorker = StartWorker

// renewableCredential reports whether home holds a credential the worker
// would renew: a record without a release tombstone whose lease is not lost.
// A lost credential is no work for a worker, which would then only idle out.
func renewableCredential(home string) (bool, error) {
	credentials, err := Credentials(home)
	if err != nil {
		return false, err
	}
	for _, credential := range credentials {
		if !credential.Lost {
			return true, nil
		}
	}
	return false, nil
}

// ensureCredentialWorker starts the worker when home holds a renewable
// credential and no process holds worker.lock, so one command after a reboot
// resumes the renewals. It looks at the credentials first and never creates
// worker.lock: a missing lock file means no worker ever ran. A damaged
// credential record, which the worker itself cannot read either, starts
// nothing and is not reported here.
func ensureCredentialWorker(home string) error {
	if !workerStartAllowed() {
		return nil
	}
	renewable, err := renewableCredential(home)
	if err != nil || !renewable {
		return nil
	}
	held, err := lockHeld(home, filepath.Join(home, WorkerLockName))
	if err != nil || held {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	_, err = startCredentialWorker(home, self)
	return err
}

// UseServerStarterForTest lets a test binary of another package start a local
// server on a session open with start, which is otherwise refused there (see
// serverStarterInjected). It returns the function that restores the previous
// starter.
func UseServerStarterForTest(start func(context.Context, servicectl.StartOptions) (api.ServerInfo, error)) (restore func()) {
	previous := startLocalServer
	startLocalServer = start
	serverStarterInjected.Store(true)
	return func() {
		startLocalServer = previous
		serverStarterInjected.Store(false)
	}
}
