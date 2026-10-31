package launch

import (
	"context"
	"os"
	"strconv"
)

// paneEntryPolls bounds the watch of a pane after run_command to two seconds of
// notifyPollInterval.
const paneEntryPolls = 20

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

// paneEnded watches the pane after run_command. The shell that enters the
// startup directory runs after respawn-pane returned 0, so a failed cd only
// shows as a pane that closes. A pane closing during a probe can fail that probe,
// so a probe error only means the next poll decides. A pane not seen to end by
// the time the watch runs out counts as started.
func paneEnded(plan LaunchPlan, pane string) (ended, gone bool) {
	for attempt := 0; attempt < paneEntryPolls; attempt++ {
		if attempt > 0 {
			sleepFn(notifyPollInterval)
		}
		facts, err := plan.backend().PaneFacts(context.Background(), probeConn(plan), pane)
		if err != nil {
			continue
		}
		switch {
		case facts.Gone:
			return true, true
		case facts.Dead == "1":
			return true, false
		case plan.processName != "" && facts.Command == plan.processName:
			return false, false
		}
	}
	return false, false
}
