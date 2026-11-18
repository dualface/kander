package cli

import "github.com/spf13/cobra"

// Started from the Explorer, kander opens its terminal board like any other start; cobra's own
// refusal to run a command-line tool by double-click is turned off.
func init() { cobra.MousetrapHelpText = "" }
