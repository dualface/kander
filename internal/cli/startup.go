package cli

import (
	"github.com/spf13/cobra"

	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/install"
	"github.com/dualface/kander/v2/internal/menu"
)

// NoticeProjectInstallRetired is the notice code of a kander that runs from a retired project
// install directory.
const NoticeProjectInstallRetired = "project_install_retired"

// refreshForVersion is the version refresh the startup maintenance runs; tests replace it.
var refreshForVersion = menu.RefreshForVersion

// startupMaintenance runs before the body of every command that is not exempt: it names a
// retired project install the binary runs from, runs the version refresh when host.json records
// another kander version than the running one, and removes what earlier releases wrote into the
// agents' global rules files, once per configuration directory. They report through notices and
// never fail the command. Bare kander leaves the version refresh to the TUI, which shows the
// doctor report after it.
func startupMaintenance(c *cobra.Command) {
	if startupExempt(c) {
		return
	}
	if root, ok := config.LegacyProjectInstallRoot(); ok {
		command.Notify(NoticeProjectInstallRetired, config.Text("projectrules.legacy_entry_notice", root))
	}
	if c.HasParent() {
		for _, notice := range refreshForVersion().Notices() {
			command.Notify(notice.Code, notice.Text)
		}
	}
	paths, err := config.GlobalInstallPaths()
	if err != nil {
		return
	}
	for _, notice := range install.GlobalCleanupNotices(install.AutoCleanGlobalInjection(paths)) {
		command.Notify(notice.Code, notice.Text)
	}
}

// startupExempt reports the commands that skip startupMaintenance, the version refresh included:
// the ones that only print static text (version, help), uninstall, which removes the global
// injection itself, the background worker and the foreground server (a managed service runs it
// too), which other kander commands start, and guard-write, which agent hooks call before every
// write.
func startupExempt(c *cobra.Command) bool {
	top := c
	for top.HasParent() && top.Parent().HasParent() {
		top = top.Parent()
	}
	switch top.Name() {
	case "version", "help", "uninstall", "worker", "guard-write":
		return true
	case "server":
		if foreground, err := c.Flags().GetBool("foreground"); err == nil && foreground {
			return true
		}
	}
	return false
}
