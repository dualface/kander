package install

import (
	"fmt"
	"os"

	"github.com/dualface/kander/internal/config"
)

// Run is the kander install subcommand.
func Run(args []string) int {
	for _, arg := range args {
		fmt.Fprintln(os.Stderr, config.CommandUsage("install"))
		fmt.Fprintln(os.Stderr, config.Text("cli.error_unknown_option", arg))
		return 2
	}
	return RunInteractive()
}
