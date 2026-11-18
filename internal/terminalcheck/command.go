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
		RunE: func(*cobra.Command, []string) error {
			if command.JSON() {
				return command.Exit(listJSON())
			}
			return command.Exit(list(out))
		},
	}
	var options Options
	test := &cobra.Command{
		Use:  "test <name>",
		Args: command.Args(1, 1),
		RunE: func(_ *cobra.Command, args []string) error {
			backend, err := selectBackend(args[0])
			if err != nil {
				return fail(diagnostics, err)
			}
			if command.JSON() {
				return command.Exit(testJSON(args[0], backend, options))
			}
			if err = Check(backend, options, out); err != nil {
				return fail(diagnostics, err)
			}
			return nil
		},
	}
	test.Flags().BoolVar(&options.Keep, "keep", false, "")
	test.Flags().BoolVar(&options.SkipFocus, "skip-focus", false, "")
	return command.Group("terminal", list, test)
}

// fail reports a failure of terminal test: on diagnostics without --json, as the error object
// with it.
func fail(diagnostics io.Writer, err error) error {
	if command.JSON() {
		return command.Exit(command.Fail(err))
	}
	fmt.Fprintln(diagnostics, err)
	return command.Exit(1)
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

// definitionEntry is one terminal definition of the result of kander terminal list.
type definitionEntry struct {
	Status  string `json:"status"`
	Source  string `json:"source"`
	Name    string `json:"name"`
	Path    string `json:"path,omitempty"`
	Detail  string `json:"detail"`
	Active  bool   `json:"active"`
	Invalid bool   `json:"invalid"`
}

// listJSON is list with --json: every definition with its status; the exit status is 1 when a
// definition is invalid.
func listJSON() int {
	entries := []definitionEntry{}
	code := 0
	for _, report := range terminal.DefinitionInventory() {
		status, detail := "pass", config.Text("terminal.definition_loaded", report.Path, strings.Join(report.Launchers, ", "))
		if report.Err != nil {
			status, detail = "fail", config.Text("terminal.definition_invalid", report.Err.Error())
			code = 1
		} else if !report.Active {
			status, detail = "skip", config.Text("terminal.definition_overridden", report.Path, report.Name)
		}
		entries = append(entries, definitionEntry{Status: status, Source: report.Source, Name: report.Name, Path: report.Path, Detail: detail, Active: report.Active, Invalid: report.Err != nil})
	}
	return command.ResultStatus(struct {
		Definitions []definitionEntry `json:"definitions"`
	}{entries}, code)
}

// testStep is one step of the result of kander terminal test.
type testStep struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// testJSON is the conformance run of one launcher with --json. A failed step is the result of
// the run, not a failure of the command: passed is false and the exit status is 1.
func testJSON(name string, backend terminal.Backend, options Options) int {
	steps := []testStep{}
	notes := []string{}
	options.Step = func(step, status, detail string) { steps = append(steps, testStep{step, status, detail}) }
	options.Note = func(line string) { notes = append(notes, line) }
	err := Check(backend, options, io.Discard)
	result := struct {
		Launcher string     `json:"launcher"`
		Passed   bool       `json:"passed"`
		Error    string     `json:"error,omitempty"`
		Steps    []testStep `json:"steps"`
		Notes    []string   `json:"notes"`
	}{Launcher: name, Passed: err == nil, Steps: steps, Notes: notes}
	status := 0
	if err != nil {
		result.Error, status = err.Error(), 1
	}
	return command.ResultStatus(result, status)
}
