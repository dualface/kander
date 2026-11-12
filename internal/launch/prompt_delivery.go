package launch

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/terminal"
)

func applyAgentDelivery(plan *LaunchPlan, cfg *config.Config, agent string) error {
	definition := config.AgentFor(cfg, agent)
	plan.sessionFileDeclared = definition.Session != nil && definition.Session.File != nil
	plan.processName = definition.ProcessName
	plan.agent = agent
	plan.extendedTimeouts = cfg.ExtendedLaunchTimeouts
	if definition.PromptDelivery != nil {
		plan.PromptDelivery = *clonePlanDelivery(definition.PromptDelivery)
	} else {
		plan.PromptDelivery = config.PromptDelivery{Mode: "argv"}
	}
	if err := rejectUnreportedAgent(plan, definition); err != nil {
		return err
	}
	return rejectPaneDirectLauncher(*plan)
}

// rejectUnreportedAgent refuses, before any side effect, an agent that a
// program status terminal cannot identify: the terminal names the agent only
// through its OSC 7501 reports, so an agent without a reporting declaration
// could never be observed, notified or dismissed there.
func rejectUnreportedAgent(plan *LaunchPlan, definition config.AgentDefinition) error {
	if !plan.capabilities().ProgramStatus {
		return nil
	}
	if definition.ProgramStatus == nil {
		return launchError("program_status.agent_undeclared", plan.Launcher, plan.agent)
	}
	app, ok := config.ProgramStatusApp(definition)
	if !ok {
		return launchError("program_status.agent_not_reporting", plan.Launcher, plan.agent)
	}
	plan.programStatusApp = app
	return nil
}

func clonePlanDelivery(src *config.PromptDelivery) *config.PromptDelivery {
	if src == nil {
		return nil
	}
	out := *src
	if src.Ready != nil {
		ready := *src.Ready
		out.Ready = &ready
	}
	if src.Blocked != nil {
		out.Blocked = append([]config.PromptBlocked{}, src.Blocked...)
	}
	return &out
}

func rejectPaneDirectLauncher(plan LaunchPlan) error {
	if plan.PromptDelivery.Mode != "pane" {
		return nil
	}
	if !plan.capabilities().Container {
		return launchError("launch.pane_delivery_requires_tmux_or_herdr")
	}
	return nil
}

func attachPrompt(plan *LaunchPlan, args []string, prompt string) []string {
	plan.Prompt = prompt
	if plan.PromptDelivery.Mode == "pane" {
		return args
	}
	return append(args, prompt)
}

func completePaneDelivery(plan LaunchPlan, outcome LaunchOutcome) error {
	if plan.capabilities().ProgramStatus {
		if err := waitProgramStatus(plan, outcome); err != nil {
			return err
		}
	} else {
		deadline, err := waitAgentTUI(plan, outcome)
		if err != nil {
			return err
		}
		waitAgentRecognized(plan, outcome, deadline)
	}
	if plan.beforePrompt != nil {
		if err := plan.beforePrompt(); err != nil {
			return err
		}
	}
	if err := deliverPromptToPane(plan, outcome); err != nil {
		output := resumedAgentFailureOutput(plan, outcome)
		return launchError("launch.pane_prompt_rejected", err.Error(), output)
	}
	return nil
}

// waitAgentRecognized waits, within the ready deadline, until a backend that
// reports agent identity names an agent in the pane. herdr routes `agent
// prompt` only to panes it has recognized, which can lag behind the TUI ready
// mark. A pane that stays unrecognized is delivered to anyway, so the delivery
// primitive reports its own error.
func waitAgentRecognized(plan LaunchPlan, outcome LaunchOutcome, deadline time.Time) {
	if !plan.capabilities().AgentIdentity {
		return
	}
	for {
		facts, err := plan.backend().PaneFacts(context.Background(), boundedSpawnConn(plan), outcome.Pane)
		if err == nil && (facts.Gone || facts.Agent != "") {
			return
		}
		remaining := deadline.Sub(nowFn())
		if remaining <= 0 {
			return
		}
		slice := notifyPollInterval
		if remaining < slice {
			slice = remaining
		}
		sleepFn(slice)
	}
}

// waitProgramStatus is the delivery readiness of a program status terminal:
// the pane has reported once (OSC 7501) and the reported app is the one the
// agent declares. The screen is never read for readiness. A pane that stays
// unreported within the ready budget, or reports another app, fails the
// delivery before the prompt is sent.
func waitProgramStatus(plan LaunchPlan, outcome LaunchOutcome) error {
	timeout := terminal.DefaultReadyTimeout
	if ready := plan.PromptDelivery.Ready; ready != nil {
		timeout = time.Duration(ready.TimeoutMS) * time.Millisecond
	}
	deadline := nowFn().Add(plan.launchTimeout(timeout))
	for {
		facts, err := plan.backend().PaneFacts(context.Background(), boundedSpawnConn(plan), outcome.Pane)
		switch {
		case err != nil:
		case facts.Gone:
			return launchError("program_status.terminal_ended", outcome.Pane, facts.GoneDetail)
		case facts.AgentApp == plan.programStatusApp:
			return nil
		case facts.AgentApp != "":
			return launchError("program_status.app_mismatch", outcome.Pane, facts.AgentApp, plan.agent, plan.programStatusApp)
		}
		remaining := deadline.Sub(nowFn())
		if remaining <= 0 {
			return launchError("program_status.ready_timeout", outcome.Pane, plan.agent)
		}
		sleepFn(min(notifyPollInterval, remaining))
	}
}

// waitAgentTUI waits for the agent TUI ready mark and returns the ready
// deadline, which the rest of the delivery shares.
func waitAgentTUI(plan LaunchPlan, outcome LaunchOutcome) (time.Time, error) {
	delivery := plan.PromptDelivery
	if delivery.Ready == nil {
		return time.Time{}, launchError("launch.pane_ready_timeout", "")
	}
	timeout := plan.launchTimeout(time.Duration(delivery.Ready.TimeoutMS) * time.Millisecond)
	deadline := nowFn().Add(timeout)
	readyFn, err := compileDeliveryMatch(delivery.Ready.Match)
	if err != nil {
		return time.Time{}, launchError("launch.pane_ready_timeout", err.Error())
	}
	type blockedRule struct {
		match  func(string) bool
		reason string
	}
	var blocked []blockedRule
	for _, item := range delivery.Blocked {
		fn, err := compileDeliveryMatch(item.Match)
		if err != nil {
			return time.Time{}, launchError("launch.pane_ready_timeout", err.Error())
		}
		blocked = append(blocked, blockedRule{match: fn, reason: item.Reason})
	}
	var lastOutput string
	for {
		output, readErr := readPaneOutput(plan, outcome)
		if readErr == nil {
			lastOutput = output
			for _, rule := range blocked {
				if rule.match(output) {
					return time.Time{}, launchError("launch.pane_ready_blocked", rule.reason, output)
				}
			}
			if readyFn(output) {
				return deadline, nil
			}
		}
		if !nowFn().Before(deadline) {
			return time.Time{}, launchError("launch.pane_ready_timeout", lastOutput)
		}
		remaining := deadline.Sub(nowFn())
		slice := notifyPollInterval
		if remaining < slice {
			slice = remaining
		}
		if plan.capabilities().WaitOutput {
			ms := int(slice / time.Millisecond)
			if ms < 1 {
				ms = 1
			}
			_ = plan.backend().WaitOutput(context.Background(), spawnConn(plan), outcome.Pane, delivery.Ready.Match, ms)
		}
		sleepFn(slice)
	}
}

func compileDeliveryMatch(match string) (func(string) bool, error) {
	if strings.HasPrefix(match, "regex:") {
		re, err := regexp.Compile(strings.TrimPrefix(match, "regex:"))
		if err != nil {
			return nil, err
		}
		return re.MatchString, nil
	}
	return func(s string) bool { return strings.Contains(s, match) }, nil
}

func readPaneOutput(plan LaunchPlan, outcome LaunchOutcome) (string, error) {
	if !plan.capabilities().Container || outcome.Pane == "" {
		return "", launchError("launch.pane_delivery_requires_tmux_or_herdr")
	}
	output, err := plan.backend().ReadOutput(context.Background(), boundedSpawnConn(plan), outcome.Pane)
	if err != nil {
		return "", err
	}
	return trimNL(output), nil
}

func deliverPromptToPane(plan LaunchPlan, outcome LaunchOutcome) error {
	// A backend that waits on the agent TUI itself runs unbounded; typed keys
	// are bounded per command.
	conn := boundedSpawnConn(plan)
	if plan.capabilities().WaitOutput {
		conn = spawnConn(plan)
	}
	return plan.backend().DeliverText(context.Background(), conn, outcome.Pane, plan.Prompt)
}
