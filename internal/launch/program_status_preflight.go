package launch

import (
	"context"
	"os"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/liveness"
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

// launchPreflight checks the agent a resume would launch: the takeover agent,
// else the card's session agent. A card without either is left to the launch.
func launchPreflight(cfg *config.Config, launcher string, agent *string, text string) error {
	name := ""
	if agent != nil {
		name = *agent
	} else if session := parseTaskSession(text); session != nil {
		name = session.Agent
	}
	if name == "" {
		return nil
	}
	return programStatusPreflight(cfg, launcher, name)
}

// dispatchExists reports whether a dispatch ID names an existing dispatch of
// the card; an empty ID names a new one.
func dispatchExists(root, task, id string) bool {
	if id == "" {
		return false
	}
	_, err := board.ReadDispatch(root, task, id)
	return err == nil
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

// NotifyPreflight refuses a new durable notify, before it prepares its
// dispatch, when the notify could only end in a recovery the configured
// launcher would refuse: the executor is observed stopped and its agent has
// no reporting program status declaration for that launcher. A live executor
// still gets direct delivery, and a retry of an existing dispatch is checked
// where it would begin a recovery.
func NotifyPreflight(ctx context.Context, root string, s board.Snapshot, id string) error {
	if dispatchExists(root, s.Entry.TaskID, id) {
		return nil
	}
	refusal := RecoveryPreflight(s.Text)
	if refusal == nil {
		return nil
	}
	if DispatchObservation(ctx, s).Status != liveness.Stopped {
		return nil
	}
	return refusal
}
