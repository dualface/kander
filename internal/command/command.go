// Package command holds what every kander cobra command shares: the error types that carry
// an exit status or an argument error out of a command, the root constructor that fixes the
// parsing semantics, the localized flag errors, and the report that turns an error into the
// process exit code. Implementation packages build their own commands on top of it, and
// internal/cli assembles them into the single command tree.
package command

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/config"
)

// ExitError ends a command with a process exit status. The command has already printed
// whatever the user needs to see, so reporting it prints nothing.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// Exit converts the exit status of a command body into the error a RunE returns.
func Exit(code int) error {
	if code == 0 {
		return nil
	}
	return &ExitError{Code: code}
}

// UsageError is an argument error: an unknown flag, a missing value, a wrong number or value
// of positional arguments, or a usage error the command contract names. Message is already
// localized.
type UsageError struct{ Message string }

func (e *UsageError) Error() string { return e.Message }

// Usage returns a UsageError whose message is the localized text id.
func Usage(id string, args ...any) error {
	return &UsageError{Message: config.Text(id, args...)}
}

// UsageText returns a UsageError with an already localized message.
func UsageText(message string) error { return &UsageError{Message: message} }

// Report prints the error a command returned and returns the process exit code. c is the
// command ExecuteC returned, which names the contract an argument error prints.
func Report(c *cobra.Command, err error) int {
	var exit *ExitError
	var usage *UsageError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return exit.Code
	case errors.As(err, &usage):
		if usage.Message != "" {
			fmt.Fprintln(os.Stderr, "kander: "+usage.Message)
		}
		fmt.Fprintln(os.Stderr, c.UsageString())
		return 2
	default:
		fmt.Fprintf(os.Stderr, "kander: %s\n", err)
		return 1
	}
}

// NewRoot returns a command root with the parsing semantics every kander command shares:
// GNU-style flags through pflag, no cobra output of its own (the caller reports errors), no
// shell completion, localized flag and argument errors, and the English help of help.go.
func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "kander",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          unknownCommand,
	}
	root.CompletionOptions.DisableDefaultCmd = true
	addJSONFlag(root)
	root.SetFlagErrorFunc(flagError)
	root.SetUsageFunc(func(c *cobra.Command) error {
		_, err := fmt.Fprint(c.ErrOrStderr(), usageText(c))
		return err
	})
	root.SetHelpFunc(func(c *cobra.Command, _ []string) { printHelp(c) })
	root.SetHelpCommand(helpCommand(root))
	return root
}

// helpCommand answers `kander help [<command> [<subcommand>]]` like `--help`.
func helpCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "help [command]",
		Short: "Help about any command",
		RunE: func(_ *cobra.Command, args []string) error {
			target, rest, err := root.Find(args)
			if err != nil || len(rest) > 0 {
				name := ""
				if len(rest) > 0 {
					name = rest[0]
				}
				return Usage("cli.error_unknown_command", name)
			}
			return target.Help()
		},
	}
}

// Annotations that make the root refuse to run a command in a checkout that is not bound to a
// project: NeedsProject marks the command, and ProjectUnless names a flag whose presence lifts
// the requirement (a command that only prints static text, such as review --schema).
const (
	NeedsProject  = "kander/needs-project"
	ProjectUnless = "kander/project-unless-flag"
)

// RequireProject marks c as needing a project binding and returns it. A flag named in unless
// lifts the requirement when it is given.
func RequireProject(c *cobra.Command, unless ...string) *cobra.Command {
	if c.Annotations == nil {
		c.Annotations = map[string]string{}
	}
	c.Annotations[NeedsProject] = "true"
	if len(unless) > 0 {
		c.Annotations[ProjectUnless] = unless[0]
	}
	return c
}

// ProjectRequired reports whether c, as invoked, needs a project binding.
func ProjectRequired(c *cobra.Command) bool {
	if c.Annotations[NeedsProject] != "true" {
		return false
	}
	flag := c.Annotations[ProjectUnless]
	return flag == "" || !c.Flags().Changed(flag)
}

// Execute runs root with args and returns the process exit code.
func Execute(root *cobra.Command, args []string) int {
	begin(args)
	defer current.reset()
	if target, _, err := root.Find(args); err == nil {
		markStreaming(target)
	}
	root.SetArgs(args)
	c, err := root.ExecuteC()
	return finish(c, err)
}
