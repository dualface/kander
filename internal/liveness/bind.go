package liveness

import "github.com/dualface/kander/internal/cli"

func init() {
	cli.Commands["check"] = RunCheck
	cli.Commands["subscribe"] = RunSubscribe
}
