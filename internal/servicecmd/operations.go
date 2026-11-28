// Package servicecmd is kander service: it installs, removes, and reports the server and worker
// services of this host. The definitions and the service manager calls belong to
// internal/svcinstall; this package adds what needs the processes themselves: the global entry,
// the client home, the stop of a detached server or worker before the service manager starts
// its own, and the readiness wait. Doctor and kander uninstall use it as well.
package servicecmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/internal/install"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/servicectl"
	"github.com/dualface/kander/v2/internal/svcinstall"
)

func init() { install.RegisterUninstallServices(inspectUninstallServices) }

// The waits of a worker stop and of a service that starts. Tests shorten them.
var (
	workerStopPoll    = 250 * time.Millisecond
	workerStopTimeout = 30 * time.Second
	readyTimeout      = 20 * time.Second
	readyPoll         = 200 * time.Millisecond
)

// The hooks tests replace: the global entry and the MSIX check.
var (
	globalEntry = install.GlobalEntry
	packaged    = install.Packaged
)

// ServerOptions are the options of a server service, as for kander server start.
type ServerOptions struct {
	Listen, DataDir string
	TTL, Renew      time.Duration
}

// InstallResult is what kander service install did.
type InstallResult struct {
	Service    string   `json:"service"`
	Manager    string   `json:"manager"`
	Location   string   `json:"location"`
	Executable string   `json:"executable"`
	Args       []string `json:"args"`
	ClientHome string   `json:"client_home,omitempty"`
	// Changed reports a definition written or rewritten; false leaves the file as it was.
	Changed bool `json:"changed"`
	// Stopped names the detached process stopped before the service manager started its own.
	Stopped string `json:"stopped,omitempty"`
	// Restarted reports a running service restarted for a changed definition.
	Restarted bool `json:"restarted"`
	// Linger is yes, no, or unknown on systemd and empty elsewhere.
	Linger string `json:"linger,omitempty"`
}

// spec builds the definition of kind for this host.
func spec(kind svcinstall.Kind, server ServerOptions) (svcinstall.Spec, error) {
	if packaged() {
		return svcinstall.Spec{}, errors.New(config.Text("service.msix_refused"))
	}
	entry, err := globalEntry()
	if err != nil {
		return svcinstall.Spec{}, err
	}
	home, err := remote.Home()
	if err != nil {
		return svcinstall.Spec{}, err
	}
	s := svcinstall.Spec{Kind: kind, Executable: entry, ClientHome: os.Getenv(config.EnvClientHome)}
	if kind == svcinstall.KindWorker {
		s.Args, s.WorkDir, s.LogPath = remote.WorkerArguments(), home, filepath.Join(home, "worker.log")
		return s, nil
	}
	dir, err := servicectl.DataDir(server.DataDir)
	if err != nil {
		return svcinstall.Spec{}, err
	}
	s.Args = servicectl.ServerStartArguments(dir, servicectl.StartOptions{Listen: server.Listen, TTL: server.TTL, Renew: server.Renew})
	s.WorkDir, s.LogPath = dir, filepath.Join(dir, "server.log")
	return s, nil
}

// ensureLog creates the private working directory and the log of a service before the service
// manager does, which would create them with its own permissions.
func ensureLog(s svcinstall.Spec) error {
	if err := os.MkdirAll(filepath.Dir(s.WorkDir), 0o700); err != nil {
		return err
	}
	if err := fs.EnsurePrivateDirectory(filepath.Dir(s.WorkDir), s.WorkDir, true); err != nil {
		return err
	}
	log, err := fs.OpenAppendFile(s.WorkDir, s.LogPath)
	if err != nil {
		return err
	}
	return log.Close()
}

// Install registers the service of kind and makes sure it runs. A detached process of the same
// kind is stopped first; a managed one is left alone when the definition did not change and
// restarted when it did.
func Install(ctx context.Context, kind svcinstall.Kind, server ServerOptions) (InstallResult, error) {
	if err := svcinstall.Available(ctx); err != nil {
		return InstallResult{}, err
	}
	s, err := spec(kind, server)
	if err != nil {
		return InstallResult{}, err
	}
	prepared, err := svcinstall.Prepare(ctx, s)
	if err != nil {
		return InstallResult{}, err
	}
	result := InstallResult{Service: string(kind), Manager: svcinstall.Manager(), Location: prepared.Location,
		Executable: s.Executable, Args: s.Args, ClientHome: s.ClientHome, Changed: prepared.Changed}
	if err := ensureLog(s); err != nil {
		return result, err
	}
	running, managed, pid, err := runningProcess(ctx, s)
	if err != nil {
		return result, err
	}
	if running && !managed {
		if err := stopDirect(ctx, s); err != nil {
			return result, err
		}
		result.Stopped = processText(kind, pid)
	}
	if prepared.Changed {
		if err := prepared.Write(ctx); err != nil {
			return result, err
		}
	}
	switch {
	case running && managed && prepared.Changed:
		result.Restarted = true
		err = restart(ctx, s)
	case running && managed:
		// The service runs this definition already.
	default:
		err = svcinstall.Start(ctx, kind)
	}
	if err != nil {
		return result, err
	}
	if err := awaitReady(ctx, s); err != nil {
		return result, err
	}
	if enabled, known, applies := svcinstall.Linger(ctx); applies {
		switch {
		case !known:
			result.Linger = "unknown"
		case enabled:
			result.Linger = "yes"
		default:
			result.Linger = "no"
		}
	}
	return result, nil
}

// runningProcess finds the process of kind for the definition s: the worker that holds the lock
// of its client home, or the server that answers at its listen address. managed reports one a
// service manager started.
func runningProcess(ctx context.Context, s svcinstall.Spec) (running, managed bool, pid int, err error) {
	if s.Kind == svcinstall.KindWorker {
		home, err := svcinstall.SpecHome(s)
		if err != nil {
			return false, false, 0, err
		}
		status, err := remote.InspectWorker(home)
		if err != nil || status.Run != remote.WorkerRunning {
			return false, false, 0, err
		}
		recorded := status.RecordState == remote.WorkerRecordRecorded
		return true, recorded && status.Record.Managed, status.Record.PID, nil
	}
	target, err := servicectl.ProbeURL(servicectl.ServiceListen(s))
	if err != nil {
		return false, false, 0, err
	}
	probe, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	info, err := servicectl.Info(probe, target)
	if client.IsUnreachable(err) {
		return false, false, 0, nil
	}
	if err != nil {
		return false, false, 0, err
	}
	record, found, err := servicectl.ReadListenRecord(info.DataDir)
	if err != nil {
		return true, false, info.PID, err
	}
	return true, found && record.Managed && record.PID == info.PID, info.PID, nil
}

// stopDirect stops the process of kind through the Kander protocol, whoever started it: the
// stop request of a worker, the authenticated stop of a server.
func stopDirect(ctx context.Context, s svcinstall.Spec) error {
	if s.Kind == svcinstall.KindWorker {
		home, err := svcinstall.SpecHome(s)
		if err != nil {
			return err
		}
		_, err = remote.StopWorker(ctx, home, workerStopPoll, workerStopTimeout)
		if errors.Is(err, remote.ErrWorkerStopTimeout) {
			return errors.New(config.Text("install.uninstall_worker_timeout", workerStopTimeout, filepath.Join(home, remote.WorkerLockName)))
		}
		return err
	}
	target, err := servicectl.ProbeURL(servicectl.ServiceListen(s))
	if err != nil {
		return err
	}
	_, err = servicectl.Controller{Unmanaged: true}.Stop(ctx, target)
	return err
}

// restart restarts the service; on Windows the process stops through the Kander protocol and
// the task starts again.
func restart(ctx context.Context, s svcinstall.Spec) error {
	err := svcinstall.Restart(ctx, s.Kind)
	if !errors.Is(err, svcinstall.ErrProtocolStop) {
		return err
	}
	if err := stopDirect(ctx, s); err != nil {
		return err
	}
	return svcinstall.Start(ctx, s.Kind)
}

// awaitReady waits until the worker holds its lock or the server answers as ready.
func awaitReady(ctx context.Context, s svcinstall.Spec) error {
	wait, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	for {
		running, _, _, err := runningProcess(wait, s)
		if err == nil && running {
			return nil
		}
		select {
		case <-wait.Done():
			return errors.New(config.Text("service.not_ready", s.Kind, s.LogPath))
		case <-time.After(readyPoll):
		}
	}
}

// Uninstall stops the service of kind and removes its definition. found is false when there is
// none; a definition without the Kander marker is refused.
func Uninstall(ctx context.Context, kind svcinstall.Kind) (location string, found bool, err error) {
	inst, found, err := svcinstall.Lookup(ctx, kind)
	if err != nil || !found {
		return svcinstall.Location(kind), false, err
	}
	if !inst.Managed {
		return inst.Location, true, &svcinstall.ForeignError{Location: inst.Location}
	}
	return inst.Location, true, unregister(ctx, inst)
}

// unregister removes a managed service. Where the service manager does not stop the process
// (Windows), the process stops through the Kander protocol when it is the managed one.
func unregister(ctx context.Context, inst svcinstall.Installed) error {
	if err := svcinstall.Remove(ctx, inst.Kind); err != nil {
		return err
	}
	if svcinstall.ManagerStops() {
		return nil
	}
	running, managed, _, err := runningProcess(ctx, inst.Spec)
	if err != nil || !running || !managed {
		return err
	}
	return stopDirect(ctx, inst.Spec)
}

// inspectUninstallServices lists the managed services for kander uninstall.
func inspectUninstallServices(ctx context.Context) ([]install.UninstallService, error) {
	var services []install.UninstallService
	var failed error
	for _, kind := range svcinstall.Kinds {
		inst, found, err := svcinstall.Lookup(ctx, kind)
		if err != nil {
			failed = errors.Join(failed, err)
			continue
		}
		if !found || !inst.Managed {
			continue
		}
		services = append(services, install.UninstallService{Kind: string(kind), Location: inst.Location,
			Unregister: func(ctx context.Context) error { return unregister(ctx, inst) }})
	}
	return services, failed
}

// processText names a process in the output.
func processText(kind svcinstall.Kind, pid int) string {
	if pid > 0 {
		return config.Text("service.process_pid", kind, pid)
	}
	return config.Text("service.process", kind)
}

// defaultServerOptions are the options kander server start uses without flags.
func defaultServerOptions() (ServerOptions, error) {
	cfg, err := config.Load(true)
	if err != nil {
		return ServerOptions{}, err
	}
	return ServerOptions{Listen: servicectl.ListenAddress(config.ServerURL(cfg)), TTL: api.DefaultTTL, Renew: api.DefaultRenewInterval}, nil
}
