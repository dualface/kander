package launch

import (
	"os"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/terminal"
)

// programStatusPreflight runs, read-only, the agent check of applyAgentDelivery
// for a launch that has not prepared its plan yet, so an entrance can refuse
// before its own first write (a TUI backlog move, a prepared dispatch, a
// dispatch attempt). launcher is the configured or chosen launcher; an auto
// that does not resolve is left to the launch itself to report.
func programStatusPreflight(cfg *config.Config, launcher, agent string) error {
	if launcher == "" {
		launcher = cfg.Launcher
	}
	if launcher == terminal.Auto {
		backend, ok := terminal.ResolveAuto(runtimeWindows(), os.Getenv)
		if !ok {
			return nil
		}
		launcher = backend.Name()
	}
	plan := LaunchPlan{Launcher: launcher, agent: agent}
	return rejectUnreportedAgent(&plan, config.AgentFor(cfg, agent))
}
