package command

import (
	"strconv"

	"github.com/spf13/cobra"
)

// Args accepts between min and max positional arguments; max < 0 means no upper bound.
func Args(min, max int) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) >= min && (max < 0 || len(args) <= max) {
			return nil
		}
		want := strconv.Itoa(min)
		switch {
		case max < 0:
			want += "+"
		case max != min:
			want += "-" + strconv.Itoa(max)
		}
		return Usage("cli.error_argument_count", want, len(args))
	}
}

// unknownCommand rejects a word where a command or subcommand was expected.
func unknownCommand(c *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	name := Escape(args[0])
	if c.HasParent() {
		name = c.CommandPath()[len("kander "):] + " " + name
	}
	return Usage("cli.error_unknown_command", name)
}

// Group returns a command that only groups subcommands: an unknown or missing subcommand is an
// argument error.
func Group(use string, subcommands ...*cobra.Command) *cobra.Command {
	group := &cobra.Command{
		Use:  use,
		Args: unknownCommand,
		RunE: func(*cobra.Command, []string) error { return Usage("cli.error_subcommand_required") },
	}
	group.AddCommand(subcommands...)
	return group
}
