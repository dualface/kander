package review

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
)

// NewCommand builds kander review: the single review of a delivery, and the evidence
// subcommands that publish and read the review plan, batches, and closure of a card.
func NewCommand() *cobra.Command { return newCommand(runReview) }

// newCommand builds the review command around run, which performs the single review once the
// arguments are checked.
func newCommand(run func(options archiveOptions, args []string) int) *cobra.Command {
	var flags archiveFlags
	c := command.RequireProject(&cobra.Command{
		Use:  "review [agent] <CWD> <base-commit> <commit> <role> <task-goal|absolute-spec-path> [review-context] [reviewed-commit]",
		Args: command.Args(5, 8),
	})
	c.RunE = func(c *cobra.Command, args []string) error {
		if _, err := config.Load(false); err != nil {
			userError(err.Error())
			return command.Exit(1)
		}
		options, err := flags.options(c)
		if err != nil {
			return err
		}
		_, rest, err := splitAgentArgs(args)
		var gate *gateError
		if errors.As(err, &gate) {
			userError(gate.message)
			return command.Exit(gate.code)
		}
		if err := command.Args(5, 7)(c, rest); err != nil {
			return err
		}
		return command.Exit(run(options, args))
	}
	flags.register(c)
	c.AddCommand(
		newJobBootstrapCommand(),
		newEvidenceCommand("plan", "<absolute-CWD> <absolute-plan.json>", 2),
		newEvidenceCommand("extend-plan", "<absolute-CWD> <absolute-extension.json>", 2),
		newEvidenceCommand("assign", "<absolute-CWD> <absolute-assignment.json>", 2),
		newEvidenceCommand("disposition", "<absolute-CWD> <absolute-author-record.json> <expected-card-revision>", 3),
		newEvidenceCommand("interpret", "<absolute-CWD> <absolute-interpretation.json>", 2),
		newEvidenceCommand("map-legacy", "<absolute-CWD> <absolute-map.json>", 2),
		newEvidenceCommand("aggregate", "<absolute-CWD> <batch-id>", 2),
		newEvidenceCommand("advance", "<absolute-CWD> <absolute-advance-request.json>", 2),
		newEvidenceCommand("close", "<absolute-CWD> <absolute-close-request.json>", 2),
		newEvidenceCommand("progress", "<absolute-CWD> <task-id>", 2),
		newEvidenceCommand("confirm", "<absolute-CWD> <publication-id> <task-id>", 3),
	)
	return c
}

// newEvidenceCommand builds one evidence subcommand taking arity positional arguments. A
// subcommand that has a schema prints it with --schema, which needs neither arguments nor a
// project binding.
func newEvidenceCommand(name, use string, arity int) *cobra.Command {
	var schema bool
	c := &cobra.Command{Use: name + " " + use}
	if schemaCommand(name) {
		command.RequireProject(c, "schema")
	} else {
		command.RequireProject(c)
	}
	c.Args = func(c *cobra.Command, args []string) error {
		if schema {
			return command.Args(0, 0)(c, args)
		}
		return command.Args(arity, arity)(c, args)
	}
	c.RunE = func(_ *cobra.Command, args []string) error {
		if schema {
			return command.Exit(printEvidenceSchema(name))
		}
		return command.Exit(runDispositionMode(name, args))
	}
	if schemaCommand(name) {
		c.Flags().BoolVar(&schema, "schema", false, "")
	}
	return c
}

// newJobBootstrapCommand builds the internal command a Windows reviewer launch re-executes to
// join its job object. It takes the reviewer command line verbatim, so no flag is parsed.
func newJobBootstrapCommand() *cobra.Command {
	return &cobra.Command{
		Use:                windowsJobBootstrap,
		Hidden:             true,
		DisableFlagParsing: true,
		RunE:               func(_ *cobra.Command, args []string) error { return command.Exit(windowsJobBootstrapMain(args)) },
	}
}
