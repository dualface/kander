package menu

import (
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/install"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/version"
)

// The version refresh brings what a kander version writes outside its binary up to that version
// the first time the global entry runs with a version host.json does not record: the rules
// directory, the kander-leader skill, the agent extensions, the global part of the doctor repair,
// the host scan, and the global injection cleanup. It never copies a binary, never writes a Git
// repository, and never asks a question; what doctor would have to ask about is listed as pending.

// VersionRefreshStatus says what one call of RefreshForVersion did.
type VersionRefreshStatus int

const (
	// VersionRefreshCurrent means host.json already records the running version, or the call
	// was not made (a test without the refresh injected).
	VersionRefreshCurrent VersionRefreshStatus = iota
	// VersionRefreshNotGlobalEntry means the running binary is not the global entry, so it only
	// names the mismatch.
	VersionRefreshNotGlobalEntry
	// VersionRefreshBusy means another kander process holds the refresh lock.
	VersionRefreshBusy
	// VersionRefreshDone means every step succeeded and host.json records the running version.
	VersionRefreshDone
	// VersionRefreshFailed means a step or the version check failed; nothing was recorded, so
	// the next command tries again.
	VersionRefreshFailed
)

// VersionRefresh is the outcome of one RefreshForVersion call.
type VersionRefresh struct {
	Status VersionRefreshStatus
	// Running is the version of this binary; Recorded the version host.json held.
	Running, Recorded string
	// Executable is the running binary, named when it is not the global entry.
	Executable string
	// Elapsed is how long the steps took.
	Elapsed time.Duration
	// Pending lists what kander doctor would ask about or must run for; it is not a failure.
	Pending []string
	// Failures lists the failed steps.
	Failures []string
	// Cleanup is what the global injection cleanup removed and kept.
	Cleanup []install.GlobalCleanupNotice
}

// VersionRefreshNotice is one line of the outcome with its --json notice code.
type VersionRefreshNotice struct {
	Code string
	Text string
}

// Ran reports whether the steps ran, to the end or to a failure.
func (r VersionRefresh) Ran() bool {
	return r.Status == VersionRefreshDone || r.Status == VersionRefreshFailed && r.Elapsed > 0
}

// Notice codes of the version refresh.
const (
	NoticeVersionRefreshed      = "version_refreshed"
	NoticeVersionRefreshFailed  = "version_refresh_failed"
	NoticeVersionRefreshSkipped = "version_refresh_skipped"
)

// Notices renders the outcome: one line for the refresh itself, then the lines of the global
// injection cleanup. A refresh that was not needed or found the lock taken renders nothing.
func (r VersionRefresh) Notices() []VersionRefreshNotice {
	var out []VersionRefreshNotice
	elapsed := r.Elapsed.Round(100 * time.Millisecond).String()
	switch r.Status {
	case VersionRefreshNotGlobalEntry:
		recorded := r.Recorded
		if recorded == "" {
			recorded = config.Text("versionrefresh.none_recorded")
		}
		out = append(out, VersionRefreshNotice{Code: NoticeVersionRefreshSkipped,
			Text: config.Text("versionrefresh.not_global_entry", r.Running, r.Executable, recorded)})
	case VersionRefreshDone:
		out = append(out, VersionRefreshNotice{Code: NoticeVersionRefreshed,
			Text: r.withPending(config.Text("versionrefresh.done", r.Running, elapsed))})
	case VersionRefreshFailed:
		out = append(out, VersionRefreshNotice{Code: NoticeVersionRefreshFailed,
			Text: r.withPending(config.Text("versionrefresh.failed", r.Running, elapsed, oneLine(r.Failures)))})
	}
	for _, notice := range r.Cleanup {
		out = append(out, VersionRefreshNotice(notice))
	}
	return out
}

func (r VersionRefresh) withPending(text string) string {
	if len(r.Pending) == 0 {
		return text
	}
	return text + " " + config.Text("versionrefresh.pending", oneLine(r.Pending))
}

// oneLine joins items into one line of text.
func oneLine(items []string) string {
	return strings.Join(strings.Fields(strings.Join(items, "; ")), " ")
}

// Seams of the refresh: tests decide whether the running binary is the global entry, and only a
// test that injects the refresh lets RefreshForVersion run at all.
var (
	runningFromGlobalEntry = install.RunningFromGlobalEntry
	versionRefreshInjected atomic.Bool
)

// RefreshForVersion runs the version refresh when host.json records another version than the
// running one, or none. Only the global entry refreshes; another binary only reports the
// mismatch. The refresh holds a lock and never waits for it: a process that finds it taken skips
// the refresh. It never fails the caller: the outcome carries what to report.
func RefreshForVersion() VersionRefresh {
	out := VersionRefresh{Running: version.String()}
	if testing.Testing() && !versionRefreshInjected.Load() {
		// A test of another package that runs a command must never touch the real home.
		return out
	}
	home, err := remote.Home()
	if err != nil {
		return out.failBefore(err)
	}
	record, err := remote.ReadHostRecord(home)
	if err != nil {
		return out.failBefore(err)
	}
	out.Recorded = record.Version
	if record.Version == out.Running {
		return out
	}
	global, err := runningFromGlobalEntry()
	if err != nil {
		return out.failBefore(err)
	}
	if !global {
		out.Status = VersionRefreshNotGlobalEntry
		out.Executable, _ = os.Executable()
		return out
	}
	release, taken, err := remote.TryVersionRefreshLock(home)
	if err != nil {
		return out.failBefore(err)
	}
	if !taken {
		out.Status = VersionRefreshBusy
		return out
	}
	defer release()
	// Another process may have finished the refresh between the first read and the lock.
	if record, err = remote.ReadHostRecord(home); err != nil {
		return out.failBefore(err)
	}
	if record.Version == out.Running {
		return out
	}
	start := time.Now()
	out.runSteps(home, record)
	out.Elapsed = max(time.Since(start), time.Nanosecond)
	if len(out.Failures) > 0 {
		out.Status = VersionRefreshFailed
	} else {
		out.Status = VersionRefreshDone
	}
	return out
}

func (r VersionRefresh) failBefore(err error) VersionRefresh {
	r.Status = VersionRefreshFailed
	r.Failures = append(r.Failures, err.Error())
	return r
}

func (r *VersionRefresh) fail(err error) {
	r.Failures = append(r.Failures, err.Error())
}

// runSteps runs the steps in order. A failed step does not stop the later ones; only a complete
// run records the version and removes the startup file earlier releases wrote.
func (r *VersionRefresh) runSteps(home string, record remote.HostRecord) {
	paths, err := config.GlobalInstallPaths()
	if err != nil {
		r.fail(err)
		return
	}
	r.refreshRules(paths)
	if switches, err := config.ScopeInstallRules(paths); err != nil {
		r.fail(err)
	} else {
		r.refreshLeaderSkills(paths, switches)
		r.refreshExtensions(paths, switches)
	}
	r.repairGlobal(paths, record)
	r.refreshScan(home, record)
	cleanup := install.CleanGlobalInjection(paths)
	for _, notice := range install.GlobalCleanupNotices(cleanup) {
		if notice.Code != install.NoticeGlobalRulesCleanupFailed {
			r.Cleanup = append(r.Cleanup, notice)
		}
	}
	if cleanup.Err != nil {
		r.fail(cleanup.Err)
	}
	if len(r.Failures) > 0 {
		return
	}
	if err := install.RemoveStartupState(paths); err != nil {
		r.fail(err)
		return
	}
	if err := remote.RecordHostVersion(home, r.Running); err != nil {
		r.fail(err)
	}
}

// refreshRules writes the missing and outdated rule files. A rule file changed locally is kept
// and pending: replacing it needs the backup decision of kander doctor.
func (r *VersionRefresh) refreshRules(paths config.InstallPaths) {
	if _, err := install.RepairRules(paths); err != nil {
		r.fail(err)
		return
	}
	report, err := install.InspectRules(paths)
	if err != nil {
		r.fail(err)
		return
	}
	for _, name := range report.Missing {
		r.fail(errors.New(config.Text("install.rule_missing", name)))
	}
	for _, name := range report.Outdated {
		r.fail(errors.New(config.Text("install.rule_outdated", name)))
	}
	if len(report.Modified) > 0 {
		r.Pending = append(r.Pending, config.Text("menu.doctor_confirm_modified_rules", len(report.Modified)))
	}
}

// refreshLeaderSkills installs and updates the kander-leader skill under the install_rules
// switches; local edits and user-owned directories are kept, as kander doctor keeps them.
func (r *VersionRefresh) refreshLeaderSkills(paths config.InstallPaths, switches config.InstallRules) {
	for _, outcome := range install.EnsureLeaderSkills(paths, switches) {
		if outcome.Err != nil {
			r.fail(errors.New(config.Text("menu.leader_skill_error", config.AgentDisplayName(outcome.Agent), outcome.Err.Error())))
		}
	}
}

// refreshExtensions installs and updates the agent extensions under the install_rules switches.
func (r *VersionRefresh) refreshExtensions(paths config.InstallPaths, switches config.InstallRules) {
	for _, agent := range install.ExtensionAgents(paths) {
		label := config.AgentDisplayName(agent)
		outcome, err := install.EnsureExtensionWith(agent, paths, switches)
		switch {
		case err != nil:
			r.fail(errors.New(config.Text("menu.agent_extension_error", label, err.Error())))
		case outcome.Status == install.ExtensionMissing || outcome.Status == install.ExtensionOutdated:
			r.fail(errors.New(config.Text("menu.agent_extension_missing", label, outcome.Target)))
		}
	}
}

// repairGlobal is the part of the doctor repair that writes outside every project: the stale
// binary left by an update and the schema of the scope config.json, when it exists. It never
// creates config.json (bare kander's first launch does), never replaces an agent or a launcher
// (that choice depends on the environment doctor probes), and never touches a project. What
// doctor would ask about is pending.
func (r *VersionRefresh) repairGlobal(paths config.InstallPaths, record remote.HostRecord) {
	install.CleanupStaleBinary(paths)
	exists, err := config.Exists()
	if err != nil {
		r.fail(err)
	} else if exists {
		if _, _, err := config.Repair(nil); err != nil {
			r.fail(errors.New(config.Text("menu.config_repair_failed") + err.Error()))
		}
	}
	if scope, err := config.LoadScope(true); err == nil {
		for _, name := range config.AgentNames(scope) {
			if _, _, ok := sessionFileInheritSpec(scope, name); ok {
				r.Pending = append(r.Pending, config.Text("menu.doctor_confirm_session_file", name))
			}
		}
	}
	pathList := os.Getenv("PATH")
	if record.Scan != nil && record.Scan.Path != "" {
		pathList = record.Scan.Path
	}
	if binaries := install.PathKanderExecutables(pathList); pathCleanupOffered(binaries) {
		r.Pending = append(r.Pending, config.Text("menu.doctor_confirm_path_cleanup", len(binaries)))
	}
}

// refreshScan scans the registered projects again with the PATH and the config_home.env values
// of the last scan, keeping both. Without a recorded PATH it scans nothing: kander doctor records
// one. A project whose configuration cannot be read is pending, not a failure: the user fixes it.
func (r *VersionRefresh) refreshScan(home string, record remote.HostRecord) {
	if len(record.Projects) == 0 {
		return
	}
	env, ok := recordedScanEnvironment(record)
	if !ok {
		r.Pending = append(r.Pending, config.Text("versionrefresh.scan_needs_doctor"))
		return
	}
	_, problems, err := scanHost(home, time.Now(), env)
	if err != nil {
		r.fail(errors.New(config.Text("host.scan_failed", err.Error())))
		return
	}
	for _, problem := range problems {
		r.Pending = append(r.Pending, config.Text("host.scan_project_failed", problem.Project, problem.Root, problem.Error))
	}
}
