package check

import (
	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/liveness"
)

func init() {
	// Sole full-binary registration for check. Do not also bind
	// board.RunCheck or liveness.RunCheck in internal/cli; a missing
	// import used to silently degrade to the structural-only helper.
	cli.Commands["check"] = Run
	cli.RemoteCommands["check"] = func(args []string) int {
		if len(args) > 0 && (args[0] == checkDelivery || args[0] == checkOverlap) {
			return Run(args)
		}
		return cli.RequireService(liveness.RunCheckRemote)(args)
	}
}
