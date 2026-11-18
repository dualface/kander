package liveness

import (
	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/remote"
)

func init() {
	// check is owned by internal/check, which delegates the no-mode form to RunCheckRemote.
	cli.Register(NewSubscribeCommand)
	// list adds the --agent-status observation, which board cannot import.
	cli.Register(func() *cobra.Command { return remote.NewListCommand(ObserveListAgents) })
}
