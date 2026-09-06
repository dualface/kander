package install

import (
	"fmt"
	"os"

	"github.com/dualface/kander/internal/config"
)

// Run is the kander install subcommand.
func Run(args []string) int {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			fmt.Println(config.Text("cli.install_usage"))
			return 0
		}
		fmt.Fprintln(os.Stderr, config.Text("cli.install_usage"))
		fmt.Fprintln(os.Stderr, config.Text("cli.error_unknown_option", arg))
		return 2
	}
	return RunInteractive()
}
