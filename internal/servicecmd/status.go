package servicecmd

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/svcinstall"
)

// Status is what kander service status and doctor report about one service.
type Status struct {
	Service  string `json:"service"`
	Manager  string `json:"manager"`
	Location string `json:"location"`
	// Installed reports a definition Kander wrote; Foreign one without the Kander marker.
	Installed bool `json:"installed"`
	Foreign   bool `json:"foreign"`
	// Executable, Args, and ClientHome are what the definition records.
	Executable string   `json:"executable,omitempty"`
	Args       []string `json:"args,omitempty"`
	ClientHome string   `json:"client_home,omitempty"`
	// State is the word of the service manager.
	State string `json:"state,omitempty"`
	// Running, PID, and Managed describe the process: for a worker the holder of worker.lock
	// and its worker.json, for a server the one that answers at the listen address and its
	// listen record. Managed reports one a service manager started.
	Running bool `json:"running"`
	PID     int  `json:"pid,omitzero"`
	Managed bool `json:"managed"`
	// MissingExecutable reports a definition whose executable does not exist; doctor counts it
	// as a problem.
	MissingExecutable bool     `json:"missing_executable"`
	Warnings          []string `json:"warnings"`
}

// Inspect looks at the services of kinds without changing anything.
func Inspect(ctx context.Context, kinds []svcinstall.Kind) []Status {
	var out []Status
	for _, kind := range kinds {
		out = append(out, inspect(ctx, kind))
	}
	return out
}

func inspect(ctx context.Context, kind svcinstall.Kind) Status {
	st := Status{Service: string(kind), Manager: svcinstall.Manager(), Location: svcinstall.Location(kind), Warnings: []string{}}
	inst, found, err := svcinstall.Lookup(ctx, kind)
	if err != nil {
		st.Warnings = append(st.Warnings, err.Error())
		return st
	}
	if !found {
		return st
	}
	st.Location = inst.Location
	if !inst.Managed {
		st.Foreign = true
		st.Warnings = append(st.Warnings, config.Text("service.foreign_definition", inst.Location))
		return st
	}
	st.Installed = true
	st.Executable, st.Args, st.ClientHome = inst.Spec.Executable, inst.Spec.Args, inst.Spec.ClientHome
	if state, err := svcinstall.StateOf(ctx, kind); err != nil {
		st.Warnings = append(st.Warnings, err.Error())
	} else {
		st.State = state.Active
	}
	running, managed, pid, err := runningProcess(ctx, inst.Spec)
	if err != nil {
		st.Warnings = append(st.Warnings, err.Error())
	}
	st.Running, st.Managed, st.PID = running, managed, pid
	if running && !managed {
		st.Warnings = append(st.Warnings, config.Text("service.unmanaged_process", kind))
	}
	if _, err := os.Stat(inst.Spec.Executable); err != nil {
		st.MissingExecutable = true
		st.Warnings = append(st.Warnings, config.Text("service.executable_missing", inst.Spec.Executable))
	} else if entry, err := globalEntry(); err == nil && !samePath(entry, inst.Spec.Executable) {
		st.Warnings = append(st.Warnings, config.Text("service.executable_not_entry", inst.Spec.Executable, entry, kind))
	}
	return st
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// Lines is the text of a status: a headline and the detail lines, without the warnings.
func (st Status) Lines() (headline string, details []string) {
	switch {
	case st.Foreign:
		return config.Text("service.status_foreign", st.Service, st.Location), nil
	case !st.Installed:
		return config.Text("service.status_not_installed", st.Service, st.Manager), nil
	}
	headline = config.Text("service.status_installed", st.Service, st.Manager, st.State)
	details = append(details, config.Text("service.status_definition", st.Location))
	details = append(details, config.Text("service.status_command", strings.Join(append([]string{st.Executable}, st.Args...), " ")))
	home := st.ClientHome
	if home == "" {
		home = config.Text("service.status_default_home")
	}
	details = append(details, config.Text("service.status_client_home", home))
	switch {
	case !st.Running:
		details = append(details, config.Text("service.status_stopped"))
	case st.Managed:
		details = append(details, config.Text("service.status_running_managed", st.PID))
	default:
		details = append(details, config.Text("service.status_running_detached", st.PID))
	}
	return headline, details
}
