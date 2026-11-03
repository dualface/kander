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

// RecoveryPreflight refuses, without side effects, a notify recovery whose
// configured launcher cannot identify the card's agent; the recovery launch
// itself would refuse the same agent, after the dispatch attempt began.
func RecoveryPreflight(text string) error {
	session := parseTaskSession(text)
	if session == nil {
		return nil
	}
	cfg, err := loadEffective()
	if err != nil {
		return err
	}
	return programStatusPreflight(cfg, "", session.Agent)
}
