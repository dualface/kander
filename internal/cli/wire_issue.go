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
	command := issue.Command(IssueProvider)
	required := RequireService(command)
	Commands["issue"] = func(args []string) int {
		if len(args) > 0 && (args[0] == "import" || args[0] == "triage" || args[0] == "result") {
			return required(args)
		}
		return command(args)
	}
}
