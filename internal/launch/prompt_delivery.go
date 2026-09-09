package launch

import (
	"regexp"
	"strings"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
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
	if plan.Launcher == "foreground" || plan.Launcher == "console" {
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
		if plan.Launcher == "herdr" {
			ms := int(slice / time.Millisecond)
			if ms < 1 {
				ms = 1
			}
			_ = herdrWaitRecent(plan.HerdrBin, outcome.Pane, delivery.Ready.Match, ms)
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
	if plan.Launcher == "herdr" && outcome.Pane != "" {
		res, err := herdrCapture(plan.HerdrBin, []string{"pane", "read", outcome.Pane}, probe.DefaultCommandTimeout)
		if err != nil {
			return "", err
		}
		if res.Code != 0 {
			return "", launchError("launch.herdr_failed", "pane read", herdrFailureDetail(res))
		}
		return trimNL(res.Stdout), nil
	}
	if (plan.Launcher == "tmux" || plan.Launcher == "tmux-session") && outcome.Pane != "" {
		res, err := runCaptured(plan.Tmux, []string{"capture-pane", "-p", "-t", outcome.Pane}, probe.DefaultCommandTimeout)
		if err != nil {
			return "", err
		}
		if res.Code != 0 {
			return "", launchError("launch.tmux_failed", "capture-pane", orExit(trimNL(res.Stderr), res.Code))
		}
		return trimNL(res.Stdout), nil
	}
	return "", launchError("launch.pane_delivery_requires_tmux_or_herdr")
}

func herdrWaitRecent(herdr, pane, match string, timeoutMS int) error {
	args := []string{"pane", "wait-output", pane}
	if strings.HasPrefix(match, "regex:") {
		args = append(args, "--regex", strings.TrimPrefix(match, "regex:"), "--source", "recent", "--timeout", itoa(timeoutMS))
	} else {
		args = append(args, "--match", match, "--source", "recent", "--timeout", itoa(timeoutMS))
	}
	res, err := herdrCapture(herdr, args, 0)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return launchError("launch.herdr_pane_is_not_ready", herdrFailureDetail(res))
	}
	return nil
}

func deliverPromptToPane(plan LaunchPlan, outcome LaunchOutcome) error {
	if plan.Launcher == "herdr" {
		return herdrAgentPrompt(plan.HerdrBin, outcome.Pane, plan.Prompt)
	}
	return tmuxSendPrompt(plan.Tmux, outcome.Pane, plan.Prompt)
}

func herdrAgentPrompt(herdr, pane, text string) error {
	res, err := herdrCapture(herdr, []string{"agent", "prompt", pane, text}, 0)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return launchError("launch.herdr_agent_prompt_failed", herdrFailureDetail(res))
	}
	return nil
}

func tmuxSendPrompt(tmux, pane, text string) error {
	for _, args := range [][]string{
		{"send-keys", "-t", pane, "-l", text},
		{"send-keys", "-t", pane, "Enter"},
	} {
		res, err := runCaptured(tmux, args, probe.DefaultCommandTimeout)
		if err != nil {
			return launchError("launch.tmux_send_keys_failed", err.Error())
		}
		if res.Code != 0 {
			detail := strings.TrimSpace(res.Stderr)
			if detail == "" {
				detail = "exit " + itoa(res.Code)
			}
			return launchError("launch.tmux_send_keys_failed", detail)
		}
	}
	return nil
}
