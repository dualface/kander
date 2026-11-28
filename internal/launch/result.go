package launch

import (
	"errors"
	"strings"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/terminal"
)

// launchResult is the result object of kander start and kander resume when they launch an
// agent. Address is the complete terminal address of the container, empty without one;
// Request is the dispatch proposal a resume queued for the new agent.
type launchResult struct {
	Action     string             `json:"action"`
	TaskID     string             `json:"task_id"`
	Size       string             `json:"size,omitempty"`
	Agent      string             `json:"agent,omitempty"`
	Launcher   string             `json:"launcher,omitempty"`
	Address    string             `json:"address,omitempty"`
	Replayed   bool               `json:"replayed"`
	Superseded bool               `json:"superseded"`
	Request    *api.CollabRequest `json:"request,omitempty"`
}

// startedExit ends a start or a resume that launched an agent and returns its exit status.
func startedExit(action string, entry board.Entry, agentName string, plan LaunchPlan, outcome LaunchOutcome, request *api.CollabRequest) int {
	if command.JSON() {
		result := launchResult{Action: action, TaskID: entry.TaskID, Size: entry.Kind, Agent: agentName, Replayed: outcome.Replayed, Request: request}
		if plan.Launcher != "" {
			result.Launcher = plan.Launcher
			if plan.capabilities().Container {
				result.Address = terminal.FormatAddress(plan.backend(), plan.address(outcome))
			}
		}
		return command.Result(result)
	}
	if request != nil {
		if code := command.Object(request); code != 0 {
			return code
		}
	}
	if err := reportLaunch(t("launch.started"), entry, agentName, plan, outcome); err != nil {
		return exitLaunch(err)
	}
	return 0
}

// supersededExit ends a launch that a takeover or an adoption overtook: a normal exit.
func supersededExit(action, task string, plan LaunchPlan, outcome LaunchOutcome) int {
	if command.JSON() {
		return command.Result(launchResult{Action: action, TaskID: task, Superseded: true})
	}
	stepAside(task, plan, outcome)
	return 0
}

// refuseTerminalLauncher is the error of a launch that would occupy the terminal, which --json
// has no terminal for: the foreground and console launchers.
func refuseTerminalLauncher(plan LaunchPlan) error {
	if !command.JSON() || plan.capabilities().Container {
		return nil
	}
	return command.Interactive(t("launch.interactive_launcher", plan.Launcher))
}

// RefuseTerminalLauncher is the early form of the launcher check of prepareLaunch for a command
// that writes a receipt or evidence before it prepares the launch: with --json it refuses a
// launcher that occupies the terminal (the requested one, else the configured one) before any
// of that is written, so the operation can be retried without --json. A launcher it cannot
// resolve is left to prepareLaunch, which reports it.
func RefuseTerminalLauncher(requested string) error {
	if !command.JSON() {
		return nil
	}
	cfg, err := loadEffective()
	if err != nil {
		return nil
	}
	return refuseConfiguredLauncher(cfg, requested)
}

func refuseConfiguredLauncher(cfg *config.Config, requested string) error {
	launcher := strings.TrimSpace(requested)
	if launcher == "" {
		launcher = cfg.Launcher
	}
	if !config.ValidLauncherName(launcher) {
		return nil
	}
	resolved, err := resolveStartLauncher(launcher, "")
	if err != nil {
		return nil
	}
	return refuseTerminalLauncher(LaunchPlan{Launcher: resolved})
}

// unresolvedFailure is the error of a late launch failure that left an executor behind, with
// the next steps in its text.
func unresolvedFailure(unresolved *UnresolvedError) error {
	header := "launch.unresolved_next_steps"
	if unresolved.adoptBlocked() {
		header = "launch.unresolved_unmarked_next_steps"
	}
	lines := []string{unresolved.Error(), t(header)}
	for _, step := range unresolved.NextSteps() {
		lines = append(lines, "  "+step)
	}
	return &command.Failure{Code: "launch_unresolved", Status: exitUnresolved, Err: errors.New(strings.Join(lines, "\n"))}
}
