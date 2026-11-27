package liveness

import (
	"github.com/spf13/cobra"

	"github.com/dualface/kander/v2/internal/cli"
	"github.com/dualface/kander/v2/internal/remote"
)

func init() {
	// check is owned by internal/check, which delegates the no-mode form to RunCheckRemote.
	cli.Register(NewSubscribeCommand)
	// list adds the --agent-status observation, which board cannot import.
	cli.Register(func() *cobra.Command { return remote.NewListCommand(ObserveListAgents) })
	// gc keeps a worktree while its executor may still run.
	cli.Register(func() *cobra.Command { return remote.NewGCCommand(ObserveExecutors) })
}
