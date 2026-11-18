package notify

import (
	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/launch"
	"github.com/dualface/kander/internal/remote"
)

func init() {
	cli.Register(NewCommand)
}

func fail(err error) int { return command.Fail(err) }

// NewCommand builds kander notify, which delivers a message to the executing agent of a card
// against the server.
func NewCommand() *cobra.Command {
	var message, messageFile, pane string
	var timeout float64
	var dispatch launch.DispatchOptions
	c := command.RequireProject(&cobra.Command{
		Use:  "notify <task-id>",
		Args: command.Args(1, 1),
	})
	c.PreRunE = func(c *cobra.Command, _ []string) error { return launch.RequireMessage(c, "notify") }
	c.RunE = func(_ *cobra.Command, args []string) error {
		return command.Exit(run(args[0], message, messageFile, pane, c.Flags().Changed("message"), timeout, dispatch))
	}
	c.Flags().StringVar(&message, "message", "", "")
	c.Flags().StringVar(&messageFile, "message-file", "", "")
	c.Flags().StringVar(&pane, "pane", "", "")
	command.Seconds(c.Flags(), &timeout, "timeout", 120, "", "notify.notify_timeout_must_be_a_finite_number_greater_than")
	launch.AddDispatchFlags(c, &dispatch)
	return c
}

func run(task, message, messageFile, pane string, messageSet bool, timeout float64, dispatch launch.DispatchOptions) int {
	cfg, err := config.Load(false)
	if err != nil {
		return fail(err)
	}
	if err := remote.RequireBinding(cfg); err != nil {
		return fail(err)
	}
	request, err := launch.NotifyRemote(task, message, messageFile, pane, messageSet, timeout, dispatch)
	if err != nil {
		return fail(err)
	}
	return command.Object(request)
}
