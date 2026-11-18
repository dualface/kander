package install

import (
	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
)

// NewInstallCommand builds kander install.
func NewInstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:  "install",
		Args: command.Args(0, 0),
		RunE: func(*cobra.Command, []string) error {
			if command.JSON() {
				return command.Interactive(config.Text("install.interactive_wizard"))
			}
			return command.Exit(RunInteractive())
		},
	}
}
