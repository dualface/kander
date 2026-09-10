package cli

import (
	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/issue/ghcli"
)

// IssueProvider builds the GitHub CLI provider. The TUI reuses this factory so
// the concrete provider stays bound in one place.
func IssueProvider() issue.IssueProvider {
	return ghcli.NewProvider(ghcli.Options{})
}

func init() {
	Commands["issue"] = issue.Command(IssueProvider)
}
