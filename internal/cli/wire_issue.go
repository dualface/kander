package cli

import (
	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/issue/ghcli"
)

func init() {
	Commands["issue"] = issue.Command(func() issue.RepositoryResolver {
		return ghcli.NewProvider(ghcli.Options{})
	})
}
