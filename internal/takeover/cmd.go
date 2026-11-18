package takeover

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/launch"
	"github.com/dualface/kander/internal/remote"
)

func init() {
	cli.Register(NewCommand)
	launch.CleanupTakeover = Cleanup
}

func fail(err error) int {
	fmt.Fprintf(os.Stderr, "kander: %s\n", err)
	return 1
}

// NewCommand builds kander dismiss, which exits an agent and closes its container against the
// server.
func NewCommand() *cobra.Command {
	var timeout float64
	c := command.RequireProject(&cobra.Command{
		Use:  "dismiss <task-id>",
		Args: command.Args(1, 1),
		RunE: func(_ *cobra.Command, args []string) error { return command.Exit(run(args[0], timeout)) },
	})
	command.Seconds(c.Flags(), &timeout, "timeout", 120, "", "takeover.dismiss_timeout_must_be_a_finite_number_greater_than")
	return c
}

func run(task string, timeout float64) int {
	cfg, err := config.Load(false)
	if err != nil {
		return fail(err)
	}
	if err := remote.RequireBinding(cfg); err != nil {
		return fail(err)
	}
	if err := commandRemoteDismiss(cfg, task, timeout); err != nil {
		return fail(err)
	}
	return 0
}
