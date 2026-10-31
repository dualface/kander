package launch

import (
	"context"
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/dualface/kander/internal/terminal"
)

// paneEntryTimeout is the budget of the whole pane watch of one launch when
// extended_launch_timeouts is off.
var paneEntryTimeout = terminal.DefaultReadyTimeout

// validateStartupDirectory refuses a directory a container cannot start in. The
// terminal does not report one: tmux falls back to $HOME with exit status 0.
func validateStartupDirectory(dir string) error {
	if err := rejectPaneControlChars(dir); err != nil {
		return launchError("launch.startup_directory_unusable", strconv.Quote(dir), err.Error())
	}
	info, err := os.Stat(dir)
	if err != nil {
		return launchError("launch.startup_directory_unusable", strconv.Quote(dir), err.Error())
	}
	if !info.IsDir() {
		return launchError("launch.startup_directory_unusable", strconv.Quote(dir), t("launch.startup_directory_is_not_a_directory"))
	}
	return nil
}

// quotedPaneCwd is the run_command form of the startup directory. Quoting stays
// here, beside paneCommand, so terminal definitions never escape a value.
func quotedPaneCwd(dir string) string {
	if runtimeWindows() {
		return ""
	}
	return posixQuote(dir)
}

// paneEntry is what the watch of a pane learned after run_command.
type paneEntry int

const (
	// paneEntered: the pane reached the startup directory or shows the agent.
	paneEntered paneEntry = iota
	// paneGone: the pane closed first, and its window with it.
	paneGone
	// paneDead: the pane process ended first; remain-on-exit kept the pane.
	paneDead
	// paneUnconfirmed: the budget ran out with no answer either way.
	paneUnconfirmed
)

// watchPaneEntry decides whether the pane entered dir. The shell runs cd after
// respawn-pane returned 0, so the exit status says nothing, and a pane that is
// merely open proves nothing while a slow shell has not reached cd yet. A
// backend that cannot report the directory has nothing to watch. A probe can
// fail while the pane closes, so an error only leaves the decision to the next
// poll. One deadline bounds the probes and the waits together.
func watchPaneEntry(plan LaunchPlan, pane, dir string) paneEntry {
	backend := plan.backend()
	reader, ok := backend.(terminal.PaneDirectoryReader)
	if !ok {
		return paneEntered
	}
	timeout := plan.launchTimeout(paneEntryTimeout)
	deadline := time.Now().Add(timeout)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	conn := probeConn(plan)
	for {
		directory, err := reader.PaneDirectory(ctx, conn, pane)
		if errors.Is(err, terminal.ErrUnsupported) {
			return paneEntered
		}
		if err == nil && sameDirectory(directory, dir) {
			return paneEntered
		}
		facts, err := backend.PaneFacts(ctx, conn, pane)
		switch {
		case err != nil:
		case facts.Gone:
			return paneGone
		case facts.Dead == "1":
			return paneDead
		case plan.processName != "" && facts.Command == plan.processName:
			return paneEntered
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return paneUnconfirmed
		}
		sleepFn(min(notifyPollInterval, remaining))
	}
}

// sameDirectory compares by file identity: the terminal reports the kernel's
// path, which can differ from dir by symlinks or letter case.
func sameDirectory(reported, dir string) bool {
	if reported == "" {
		return false
	}
	first, err := os.Stat(reported)
	if err != nil {
		return false
	}
	second, err := os.Stat(dir)
	return err == nil && os.SameFile(first, second)
}
