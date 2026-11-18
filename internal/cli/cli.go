// Package cli assembles the single kander cobra command tree: implementation packages
// register their commands here, and Run parses the global --lang, executes the tree, and
// returns the process exit code. The binary main.go remains a single call to Run.
package cli

import (
	"fmt"
	"os"
	"slices"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/install"
	"github.com/dualface/kander/internal/menu"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/terminalcheck"
	"github.com/dualface/kander/internal/version"
)

// builders construct the top-level commands. A tree is built for every Run, so no flag state
// survives from one run to the next.
var builders []func() *cobra.Command

// Register adds the top-level command build returns to the tree. Implementation packages call
// it from init; registering one name twice is a programming error that NewRoot reports.
func Register(build func() *cobra.Command) { builders = append(builders, build) }

// DefaultRunner is what runs without a subcommand. internal/tui sets it on registration,
// so a bare kander opens the alt-screen board; when nil, the usage text is printed.
var DefaultRunner func() int

func init() {
	Register(menu.NewDoctorCommand)
	Register(menu.NewConfigCommand)
	Register(newVersionCommand)
	Register(install.NewInstallCommand)
	Register(install.NewUninstallCommand)
	Register(terminalcheck.NewCommand)
	Register(remote.NewInitCommand)
	Register(remote.NewShowCommand)
	Register(remote.NewNewCommand)
	Register(remote.NewUpdateCommand)
	Register(remote.NewMoveCommand)
	Register(remote.NewPickCommand)
	Register(remote.NewGuardWriteCommand)
	Register(remote.NewRequestCommand)
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:  "version",
		Args: command.Args(0, 0),
		RunE: func(*cobra.Command, []string) error {
			if command.JSON() {
				return command.Exit(command.Result(struct {
					Version string `json:"version"`
				}{version.String()}))
			}
			fmt.Fprintln(os.Stdout, "kander "+version.String())
			return nil
		},
	}
}

// NewRoot builds the command tree of every registered command.
func NewRoot() *cobra.Command {
	root := command.NewRoot()
	root.PersistentFlags().Var(languageValue{}, "lang", "")
	root.RunE = func(*cobra.Command, []string) error {
		if err := command.NoJSON(); err != nil {
			return err
		}
		if DefaultRunner == nil {
			return command.UsageText("")
		}
		return command.Exit(DefaultRunner())
	}
	root.PersistentPreRunE = func(c *cobra.Command, _ []string) error {
		if command.ProjectRequired(c) {
			return command.Exit(remote.CheckProject())
		}
		return nil
	}
	seen := map[string]bool{}
	for _, build := range builders {
		c := build()
		if seen[c.Name()] {
			panic("cli: command registered twice: " + c.Name())
		}
		seen[c.Name()] = true
		root.AddCommand(c)
	}
	return root
}

// languageValue is the --lang flag; the value itself is applied before the tree runs.
type languageValue struct{}

func (languageValue) String() string { return "" }
func (languageValue) Type() string   { return "lang" }
func (languageValue) Set(raw string) error {
	if !slices.Contains(config.Languages, raw) {
		return command.Usage("cli.error_lang_must_be_cn_or_en")
	}
	return nil
}

// Run executes os.Args-shaped arguments and returns the process exit code.
func Run(arguments []string) int {
	args := arguments
	if len(args) > 0 {
		args = args[1:]
	}
	config.ApplyLanguageArgument(args)
	return command.Execute(NewRoot(), args)
}
