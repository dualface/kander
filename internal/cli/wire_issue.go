package cli

import (
	"github.com/spf13/cobra"

	"github.com/dualface/kander/v2/internal/issue"
	"github.com/dualface/kander/v2/internal/issue/ghcli"
	"github.com/dualface/kander/v2/internal/worker"
)

// IssueProvider builds the GitHub CLI provider. The TUI reuses this factory so
// the concrete provider stays bound in one place.
func IssueProvider() issue.IssueProvider {
	return ghcli.NewProvider(ghcli.Options{})
}

func init() {
	Register(func() *cobra.Command { return issue.NewCommand(IssueProvider) })
	Register(worker.NewCommand)
}
