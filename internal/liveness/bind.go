package liveness

import "github.com/dualface/kander/internal/cli"

func init() {
	// Sole registration for check: structural board validation plus liveness probing.
	// Do not also bind board.RunCheck in internal/cli; that caused silent degradation
	// whenever this package was not linked into the binary.
	cli.Commands["check"] = RunCheck
	cli.Commands["subscribe"] = RunSubscribe
}
