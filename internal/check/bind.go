package check

import "github.com/dualface/kander/internal/cli"

func init() {
	// Sole full-binary registration for check. Do not also bind
	// board.RunCheck or liveness.RunCheck in internal/cli; a missing
	// import used to silently degrade to the structural-only helper.
	cli.Commands["check"] = Run
}
