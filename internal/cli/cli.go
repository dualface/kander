// Package cli assembles the single kander cobra command tree: implementation packages
// register their commands here, and Run parses the global --lang, executes the tree, and
// returns the process exit code. The binary main.go remains a single call to Run.
package cli

import (
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/install"
	"github.com/dualface/kander/v2/internal/menu"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/terminalcheck"
	"github.com/dualface/kander/v2/internal/version"
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
	Register(remote.NewAssignCommand)
	Register(remote.NewDecisionCommand)
	Register(remote.NewUpdateCommand)
	Register(remote.NewMoveCommand)
	Register(remote.NewPickCommand)
	Register(remote.NewConfirmCommand)
	Register(remote.NewGuardWriteCommand)
	Register(remote.NewRequestCommand)
	Register(remote.NewPolicyCommand)
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
	seen := map[string]bool{}
	for _, build := range builders {
		c := build()
		if seen[c.Name()] {
			panic("cli: command registered twice: " + c.Name())
		}
		seen[c.Name()] = true
		root.AddCommand(c)
	}
	command.GateProject(root, remote.CheckProject)
	command.BeforeRun(root, startupMaintenance)
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
	return Execute(NewRoot(), arguments)
}

// Execute runs the supplied root with os.Args-shaped arguments and returns
// the process exit code.
func Execute(root *cobra.Command, arguments []string) int {
	args := arguments
	if len(args) > 0 {
		args = args[1:]
	}
	config.ApplyLanguageArgument(languageArguments(root, args))
	return command.Execute(root, args)
}

// languageArguments is the --lang selection of args as a synthetic argument list for
// config.ApplyLanguageArgument, empty when args select none. The selection is read with the flag
// definitions of the command the arguments name, so a word that is the value of another flag
// (`notify TASK --message --lang=ja`) is not mistaken for the global option. A command that takes
// its arguments verbatim selects nothing.
func languageArguments(root *cobra.Command, args []string) []string {
	target, _, err := root.Find(args)
	if err != nil || target.DisableFlagParsing {
		target = root
	}
	var selected string
	var flags pflag.FlagSet
	flags.Init("lang", pflag.ContinueOnError)
	flags.SetInterspersed(true)
	flags.SetOutput(io.Discard)
	flags.ParseErrorsAllowlist.UnknownFlags = true
	add := func(f *pflag.Flag) {
		if flags.Lookup(f.Name) != nil {
			return
		}
		flags.AddFlag(&pflag.Flag{
			Name:        f.Name,
			Shorthand:   f.Shorthand,
			NoOptDefVal: f.NoOptDefVal,
			Value:       &scanValue{kind: f.Value.Type(), selected: &selected, record: f.Name == "lang"},
		})
	}
	target.LocalFlags().VisitAll(add)
	target.InheritedFlags().VisitAll(add)
	// cobra adds the help flag only when it runs the command.
	if flags.Lookup("help") == nil {
		flags.BoolP("help", "h", false, "")
	}
	_ = flags.Parse(args)
	if selected == "" {
		return nil
	}
	return []string{"kander", "--lang", selected}
}

// scanValue is a flag value that only notes what was given.
type scanValue struct {
	kind     string
	selected *string
	record   bool
}

func (v *scanValue) String() string { return "" }
func (v *scanValue) Type() string   { return v.kind }
func (v *scanValue) Set(raw string) error {
	if v.record {
		*v.selected = raw
	}
	return nil
}
