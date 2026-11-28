package menu

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/servicecmd"
	"github.com/dualface/kander/v2/internal/svcinstall"
	"github.com/dualface/kander/v2/internal/version"
)

// reportBackgroundWorker reports the v2 background worker of this client and
// compares its version with the running kander. It only reads: it creates
// nothing, changes no file and stops no process. A client directory that does
// not exist yet reports nothing. A worker that runs another version, or one
// without a usable version record, makes the report unhealthy, because the
// leases of this host are then served by code that may not match this binary.
func reportBackgroundWorker() bool {
	home, err := remote.Home()
	if err != nil {
		return true
	}
	status, err := remote.InspectWorker(home)
	if err != nil {
		warning(err.Error())
		return true
	}
	switch status.Run {
	case remote.WorkerNoHome:
		return true
	case remote.WorkerStopped:
		hint(config.Text("menu.background_worker_not_running"))
		return true
	}
	self := version.String()
	switch status.Finding(self) {
	case remote.WorkerFindingMismatch:
		executable, _ := os.Executable()
		warning(status.MismatchText(self, executable))
		return false
	case remote.WorkerFindingUnrecorded, remote.WorkerFindingUnreadable:
		warning(config.Text("menu.background_worker_version_unrecorded", home, config.Text("remote.worker_stop_consequence")))
		return false
	}
	success(config.Text("menu.background_worker_running", status.Record.Version, strconv.Itoa(status.Record.PID)))
	return true
}

// reportServices reports the server and worker services of this host. It only reads. A
// definition whose executable does not exist makes the report unhealthy: the service manager
// cannot start it.
func reportServices() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	healthy := true
	for _, st := range servicecmd.Inspect(ctx, svcinstall.Kinds) {
		headline, _ := st.Lines()
		switch {
		case st.Installed:
			success(headline)
		default:
			hint(headline)
		}
		for _, text := range st.Warnings {
			warning(text)
		}
		if st.MissingExecutable {
			healthy = false
		}
	}
	return healthy
}
