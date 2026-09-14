package terminalcheck

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/terminal"
	_ "github.com/dualface/kander/internal/terminal/builtin"
)

// Run implements terminal list/test. It never requires or creates a board.
func Run(args []string) int { return run(args, os.Stdout, os.Stderr) }

func run(args []string, out, diagnostics io.Writer) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprintln(out, config.Text("terminal.usage"))
		return 0
	}
	if len(args) == 1 && args[0] == "list" {
		return list(out)
	}
	if len(args) < 2 || args[0] != "test" || strings.HasPrefix(args[1], "-") {
		fmt.Fprintln(diagnostics, config.Text("terminal.usage"))
		return 2
	}
	options := Options{}
	for _, arg := range args[2:] {
		switch arg {
		case "--keep":
			options.Keep = true
		case "--skip-focus":
			options.SkipFocus = true
		default:
			fmt.Fprintln(diagnostics, config.Text("terminal.usage"))
			return 2
		}
	}
	backend, err := selectBackend(args[1])
	if err == nil {
		err = Check(backend, options, out)
	}
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	return 0
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
