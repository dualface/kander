package cli

import (
	"github.com/spf13/cobra"

	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/install"
)

// NoticeProjectInstallRetired is the notice code of a kander that runs from a retired project
// install directory.
const NoticeProjectInstallRetired = "project_install_retired"

// startupMaintenance runs before the body of every command that is not exempt: it names a
// retired project install the binary runs from, and removes what earlier releases wrote into
// the agents' global rules files, once per configuration directory. Both report through
// notices and never fail the command.
func startupMaintenance(c *cobra.Command) {
	if startupExempt(c) {
		return
	}
	if root, ok := config.LegacyProjectInstallRoot(); ok {
		command.Notify(NoticeProjectInstallRetired, config.Text("projectrules.legacy_entry_notice", root))
	}
	paths, err := config.GlobalInstallPaths()
	if err != nil {
		return
	}
	for _, notice := range install.GlobalCleanupNotices(install.AutoCleanGlobalInjection(paths)) {
		command.Notify(notice.Code, notice.Text)
	}
}

// startupExempt reports the commands that skip startupMaintenance: the ones that only print
// static text (version, help), uninstall, which removes the global injection itself, the
// background worker and the foreground server, which other kander commands start, and
// guard-write, which agent hooks call before every write.
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
