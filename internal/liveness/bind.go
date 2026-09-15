package liveness

import "github.com/dualface/kander/internal/cli"

func init() {
	// subscribe stays here. check is owned by internal/check, which
	// delegates the no-mode form to RunCheck.
	cli.Commands["subscribe"] = RunSubscribe
}
