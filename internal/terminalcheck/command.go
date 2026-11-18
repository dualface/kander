package terminalcheck

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/terminal"
	_ "github.com/dualface/kander/internal/terminal/builtin"
)

// NewCommand builds kander terminal, whose list and test subcommands never require or create
// a board.
func NewCommand() *cobra.Command { return newCommand(os.Stdout, os.Stderr) }

func newCommand(out, diagnostics io.Writer) *cobra.Command {
	list := &cobra.Command{
		Use:  "list",
		Args: command.Args(0, 0),
		RunE: func(*cobra.Command, []string) error { return command.Exit(list(out)) },
	}
	var options Options
	test := &cobra.Command{
		Use:  "test <name>",
		Args: command.Args(1, 1),
		RunE: func(_ *cobra.Command, args []string) error {
			backend, err := selectBackend(args[0])
			if err == nil {
				err = Check(backend, options, out)
			}
			if err != nil {
				fmt.Fprintln(diagnostics, err)
				return command.Exit(1)
			}
			return nil
		},
	}
	test.Flags().BoolVar(&options.Keep, "keep", false, "")
	test.Flags().BoolVar(&options.SkipFocus, "skip-focus", false, "")
	return command.Group("terminal", list, test)
}

func list(out io.Writer) int {
	code := 0
	for _, report := range terminal.DefinitionInventory() {
		status, detail := "pass", config.Text("terminal.definition_loaded", report.Path, strings.Join(report.Launchers, ", "))
		if report.Err != nil {
			status, detail = "fail", config.Text("terminal.definition_invalid", report.Err.Error())
			code = 1
		} else if !report.Active {
			status, detail = "skip", config.Text("terminal.definition_overridden", report.Path, report.Name)
		}
		if _, err := fmt.Fprintln(out, config.Text("terminal.list_entry", status, report.Source, report.Name, detail)); err != nil {
			return 1
		}
	}
	return code
}

func selectBackend(name string) (terminal.Backend, error) {
	reports := terminal.DefinitionInventory()
	// Unlike runtime fallback, a conformance run requires a valid inventory.
	// A malformed file may not expose its name or launchers, so guessing which
	// fallback it could override would risk testing a different definition.
	for _, report := range reports {
		if report.Err != nil {
			return nil, report.Err
		}
	}
	for _, report := range reports {
		if !report.Active || report.Err != nil {
			continue
		}
		for _, launcher := range report.Launchers {
			if launcher == name {
				backend, _ := terminal.Lookup(launcher)
				return backend, nil
			}
		}
		if report.Name == name && len(report.Launchers) > 0 {
			backend, _ := terminal.Lookup(report.Launchers[0])
			return backend, nil
		}
	}
	return nil, fmt.Errorf("%s", config.Text("terminal.test_unknown", name))
}
