// Package cli builds and runs the same command tree as the Kander executable.
package cli

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/v2/client"
	core "github.com/dualface/kander/v2/internal/cli"
	_ "github.com/dualface/kander/v2/internal/commands"
)

// Options let the host extend the command tree and install a process-wide
// transport for newly created Kander clients.
type Options struct {
	// Transport, when non-nil, becomes the default for subsequent client.New
	// calls. A nil transport leaves the process default unchanged.
	Transport http.RoundTripper
	// Commands contains new top-level commands not attached to another tree.
	Commands []*cobra.Command
}

// NewRoot builds the complete Kander command tree and adds the host's
// commands. Duplicate command names or aliases return an error before any
// extension is attached or a transport installed.
func NewRoot(options Options) (*cobra.Command, error) {
	root := core.NewRoot()
	root.InitDefaultHelpCmd()
	used := make(map[string]bool)
	for _, c := range root.Commands() {
		used[c.Name()] = true
		for _, alias := range c.Aliases {
			used[alias] = true
		}
	}
	for _, c := range options.Commands {
		if c == nil || c.Name() == "" || c.HasParent() {
			return nil, errors.New("each additional command must be named and unattached")
		}
		for _, name := range append([]string{c.Name()}, c.Aliases...) {
			if used[name] {
				return nil, fmt.Errorf("CLI command name or alias %q is already in use", name)
			}
			used[name] = true
		}
	}
	root.AddCommand(options.Commands...)
	if options.Transport != nil {
		client.SetDefaultTransport(options.Transport)
	}
	return root, nil
}

// Execute runs a root from NewRoot with os.Args-shaped arguments. It applies
// the CLI's language and output handling and returns the exit code.
func Execute(root *cobra.Command, arguments []string) int { return core.Execute(root, arguments) }

// Run builds and executes the standard Kander tree using os.Args-shaped
// arguments. It returns the exit code without exiting the host process.
func Run(arguments []string) int { return core.Run(arguments) }
