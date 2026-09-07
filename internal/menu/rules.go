package menu

import (
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/install"
)

// rulesIntegration reports whether the agent rules file references the Kander entry of the scope.
func rulesIntegration(agent string, paths config.InstallPaths) (bool, string) {
	return install.RulesIntegration(agent, paths)
}
