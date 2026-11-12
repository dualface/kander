package liveness

import (
	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/remote"
)

func init() {
	// subscribe stays here. check is owned by internal/check, which
	// delegates the no-mode form to RunCheck.
	cli.Commands["subscribe"] = RunSubscribe
	// list adds the --agent-status observation, which board cannot import.
	cli.Commands["list"] = RunList
	cli.RemoteCommands["list"] = func(args []string) int { return remote.RunListWithAgentStatus(args, ObserveListAgents) }
}
