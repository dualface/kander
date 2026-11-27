package remote

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/install"
	"github.com/dualface/kander/v2/internal/servicectl"
)

func init() { install.RegisterUninstallBackground(InspectUninstallBackground) }

// The worker stop of kander uninstall probes worker.lock every workerStopPoll and gives up after
// workerStopTimeout, which is longer than the longest single step of the renewal and work loops.
// Tests shorten the timeout.
const workerStopPoll = 250 * time.Millisecond

var workerStopTimeout = 30 * time.Second

// serverProbeTimeout bounds the read-only look at one local server.
const serverProbeTimeout = 3 * time.Second

// InspectUninstallBackground finds the local server and the worker of this host, and the
// unreleased leases of the client home, without changing anything. Only the Stop functions it
// returns change state: the server stops like kander server stop, and the worker through its
// stop request file.
func InspectUninstallBackground(ctx context.Context) install.UninstallBackground {
	var found install.UninstallBackground
	dataDir, err := servicectl.DataDir("")
	if err != nil {
		found.Err = errors.Join(found.Err, err)
	} else {
		found.ServerDataDir = dataDir
	}
	servers, err := localServers(ctx, dataDir)
	found.Processes = append(found.Processes, servers...)
	found.Err = errors.Join(found.Err, err)
	home, err := Home()
	if err != nil {
		found.Err = errors.Join(found.Err, err)
		return found
	}
	found.ClientHome = home
	worker, running, err := localWorker(home)
	found.Err = errors.Join(found.Err, err)
	if running {
		found.Processes = append(found.Processes, worker)
	}
	leases, err := unreleasedLeases(home)
	found.Leases = leases
	found.Err = errors.Join(found.Err, err)
	return found
}

// localServers returns the loopback servers this host runs: the one at the configured URL and
// the one the default data directory recorded, each once.
func localServers(ctx context.Context, dataDir string) ([]install.UninstallProcess, error) {
	targets := []string{config.DefaultServerURL}
	if cfg, err := config.Load(true); err == nil {
		targets[0] = config.ServerURL(cfg)
	}
	if dataDir != "" {
		if recorded, err := servicectl.RecordedURL(dataDir); err == nil && recorded != "" {
			targets = append(targets, recorded)
		}
	}
	var servers []install.UninstallProcess
	var failed error
	seen := map[string]bool{}
	for _, target := range targets {
		if !servicectl.Local(target) || seen[target] {
			continue
		}
		seen[target] = true
		probe, cancel := context.WithTimeout(ctx, serverProbeTimeout)
		info, err := servicectl.Info(probe, target)
		cancel()
		if client.IsUnreachable(err) {
			continue
		}
		if err != nil {
			failed = errors.Join(failed, fmt.Errorf("%s: %w", target, err))
			continue
		}
		key := fmt.Sprintf("%d\x00%s", info.PID, filepath.Clean(info.DataDir))
		if seen[key] {
			continue
		}
		seen[key] = true
		server := install.UninstallProcess{Kind: install.UninstallServer, PID: info.PID, Where: target}
		if err := servicectl.CheckAdminToken(info.DataDir); err != nil {
			server.Reason = config.Text("install.uninstall_server_no_token", info.DataDir, err)
		} else {
			server.Stop = func(ctx context.Context) error {
				_, err := servicectl.Controller{}.Stop(ctx, target)
				return err
			}
		}
		servers = append(servers, server)
	}
	return servers, failed
}

// localWorker returns the worker that holds worker.lock of home. running is false when none does.
func localWorker(home string) (install.UninstallProcess, bool, error) {
	status, err := InspectWorker(home)
	if err != nil || status.Run != WorkerRunning {
		return install.UninstallProcess{}, false, err
	}
	worker := install.UninstallProcess{Kind: install.UninstallWorker, Where: home}
	if status.RecordState == WorkerRecordRecorded {
		worker.PID = status.Record.PID
	}
	worker.Stop = func(ctx context.Context) error {
		_, err := StopWorker(ctx, home, workerStopPoll, workerStopTimeout)
		if errors.Is(err, ErrWorkerStopTimeout) {
			return errors.New(config.Text("install.uninstall_worker_timeout", workerStopTimeout, filepath.Join(home, WorkerLockName)))
		}
		return err
	}
	return worker, true, nil
}

// unreleasedLeases lists the execution credentials of home that are neither released nor lost.
func unreleasedLeases(home string) ([]install.UninstallLease, error) {
	credentials, err := Credentials(home)
	if err != nil {
		return nil, err
	}
	var leases []install.UninstallLease
	for _, c := range credentials {
		if c.Lost {
			continue
		}
		kind := c.ResourceKind
		if kind == "" {
			kind = "task"
		}
		leases = append(leases, install.UninstallLease{ProjectID: c.ProjectID, TaskID: c.TaskID, Kind: kind, Generation: c.Generation, AgentPID: c.AgentPID})
	}
	return leases, nil
}
