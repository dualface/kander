package launch

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/dualface/kander/internal/config"
)

func applyAgentDelivery(plan *LaunchPlan, cfg *config.Config, agent string) error {
	definition := config.AgentFor(cfg, agent)
	if definition.PromptDelivery != nil {
		plan.PromptDelivery = *clonePlanDelivery(definition.PromptDelivery)
	} else {
		plan.PromptDelivery = config.PromptDelivery{Mode: "argv"}
	}
	return rejectPaneDirectLauncher(*plan)
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
	if err := waitAgentTUI(plan, outcome); err != nil {
		return err
	}
	if err := deliverPromptToPane(plan, outcome); err != nil {
		output := resumedAgentFailureOutput(plan, outcome)
		return launchError("launch.pane_prompt_rejected", err.Error(), output)
	}
	return nil
}

func waitAgentTUI(plan LaunchPlan, outcome LaunchOutcome) error {
	delivery := plan.PromptDelivery
	if delivery.Ready == nil {
		return launchError("launch.pane_ready_timeout", "")
	}
	timeout := time.Duration(delivery.Ready.TimeoutMS) * time.Millisecond
	deadline := nowFn().Add(timeout)
	readyFn, err := compileDeliveryMatch(delivery.Ready.Match)
	if err != nil {
		return launchError("launch.pane_ready_timeout", err.Error())
	}
	type blockedRule struct {
		match  func(string) bool
		reason string
	}
	var blocked []blockedRule
	for _, item := range delivery.Blocked {
		fn, err := compileDeliveryMatch(item.Match)
		if err != nil {
			return launchError("launch.pane_ready_timeout", err.Error())
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
					return launchError("launch.pane_ready_blocked", rule.reason, output)
				}
			}
			if readyFn(output) {
				return nil
			}
		}
		if !nowFn().Before(deadline) {
			return launchError("launch.pane_ready_timeout", lastOutput)
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
