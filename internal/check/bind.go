package check

import (
	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/liveness"
)

func init() {
	// Sole full-binary registration for check. The Git analyzers run
	// locally; the no-mode form goes through the server.
	cli.Commands["check"] = func(args []string) int {
		if len(args) > 0 && (args[0] == checkDelivery || args[0] == checkOverlap) {
			return Run(args)
		}
		return cli.RequireService(liveness.RunCheckRemote)(args)
	}
}
