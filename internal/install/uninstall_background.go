package install

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/dualface/kander/v2/internal/config"
)

// UninstallProcess is one local background process of this host that kander uninstall stops.
type UninstallProcess struct {
	// Kind is UninstallServer or UninstallWorker.
	Kind string
	// PID is the operating system process ID; 0 when it is not known.
	PID int
	// Where is the URL of a server, or the client home of a worker.
	Where string
	// Stop stops the process. Nil means it cannot be stopped; Reason says why.
	Stop   func(ctx context.Context) error
	Reason string
}

// The kinds of UninstallProcess.
const (
	UninstallServer = "server"
	UninstallWorker = "worker"
)

// UninstallLease is one execution credential of this host that is neither released nor lost.
// Its lease is not renewed while no worker runs.
type UninstallLease struct {
	ProjectID string `json:"project_id"`
	TaskID    string `json:"task_id"`
	// Kind is the resource of the claim: task, group, or integration.
	Kind       string `json:"kind"`
	Generation uint64 `json:"generation"`
	// AgentPID is the managed agent of the execution; 0 when the claim was taken by hand.
	AgentPID int `json:"agent_pid,omitzero"`
}

// UninstallBackground is what a read-only look at the local server and worker found.
type UninstallBackground struct {
	Processes []UninstallProcess
	Leases    []UninstallLease
	// ClientHome and ServerDataDir are data directories uninstall keeps.
	ClientHome    string
	ServerDataDir string
	// Err joins the inspections that failed; each is reported and nothing is stopped for it.
	Err error
}

// inspectUninstallBackground is the inspection of the local background processes. The remote
// package registers it, because it owns the client home and this package cannot import it; a
// binary without it stops nothing.
var inspectUninstallBackground func(ctx context.Context) UninstallBackground

// RegisterUninstallBackground sets the inspection kander uninstall uses to find the local server,
// the worker, and the leases of this host. The inspection never changes anything; only the Stop
// functions it returns do.
func RegisterUninstallBackground(inspect func(ctx context.Context) UninstallBackground) {
	inspectUninstallBackground = inspect
}

// planBackground adds the processes to stop and the leases to report to the plan.
func (plan *UninstallPlan) planBackground(ctx context.Context) {
	if inspectUninstallBackground == nil {
		return
	}
	found := inspectUninstallBackground(ctx)
	plan.Leases = found.Leases
	plan.clientHome, plan.serverDataDir = found.ClientHome, found.ServerDataDir
	if found.Err != nil {
		plan.Processes = append(plan.Processes, UninstallItem{Action: UninstallSkip, Path: config.Text("install.uninstall_background"), Reason: found.Err.Error(), process: &UninstallProcess{}})
	}
	for _, p := range found.Processes {
		item := UninstallItem{Action: UninstallStop, Path: processText(p), Reason: p.Reason, process: &p}
		if p.Stop == nil {
			item.Action = UninstallSkip
		}
		plan.Processes = append(plan.Processes, item)
	}
}

// processText describes a process for the plan and the result.
func processText(p UninstallProcess) string {
	pid := config.Text("install.uninstall_pid_unknown")
	if p.PID > 0 {
		pid = strconv.Itoa(p.PID)
	}
	if p.Kind == UninstallServer {
		return config.Text("install.uninstall_process_server", p.Where, pid)
	}
	return config.Text("install.uninstall_process_worker", p.Where, pid)
}

// stopProcesses stops every planned process in order. A failure is reported and the rest go on.
func stopProcesses(ctx context.Context, items []UninstallItem, out io.Writer) error {
	var failed error
	for _, item := range items {
		if item.Action != UninstallStop {
			continue
		}
		if err := item.process.Stop(ctx); err != nil {
			fmt.Fprintln(out, config.Text("install.uninstall_stop_failed", item.Path, err))
			failed = joinFailure(failed, item.Path, err)
			continue
		}
		fmt.Fprintln(out, config.Text("install.uninstall_stopped", item.Path))
	}
	return failed
}

// printLeases lists the leases of this host and what uninstalling means for them.
func printLeases(out io.Writer, leases []UninstallLease) {
	if len(leases) == 0 {
		return
	}
	fmt.Fprintln(out, config.Text("install.uninstall_leases"))
	for _, lease := range leases {
		if lease.AgentPID > 0 {
			fmt.Fprintln(out, config.Text("install.uninstall_lease_agent", lease.ProjectID, lease.TaskID, lease.Generation, lease.Kind, lease.AgentPID))
			continue
		}
		fmt.Fprintln(out, config.Text("install.uninstall_lease", lease.ProjectID, lease.TaskID, lease.Generation, lease.Kind))
	}
	fmt.Fprintln(out, config.Text("install.uninstall_lease_impact"))
}
