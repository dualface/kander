package launch

import (
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/terminal"
)

// programStatusPreflight runs, read-only, the agent check of applyAgentDelivery
// for a launch that has not prepared its plan yet, so an entrance can refuse
// before its own first write (a TUI backlog move, a prepared dispatch, a
// dispatch attempt). launcher is the configured or chosen launcher; an auto
// that does not resolve is left to the launch itself to report.
func programStatusPreflight(cfg *config.Config, launcher, agent string) error {
	// An agent this version cannot use is refused first, with the reason, before
	// any launcher-specific check reads its definition.
	if !config.HasAgent(cfg, agent) {
		return unavailableAgentError(cfg, agent)
	}
	if launcher == "" {
		launcher = cfg.Launcher
	}
	if launcher == terminal.Auto {
		resolved, err := resolveStartLauncher(launcher, "")
		if err != nil {
			return nil
		}
		launcher = resolved
	}
	plan := LaunchPlan{Launcher: launcher, agent: agent}
	return rejectUnreportedAgent(&plan, config.AgentFor(cfg, agent))
}
