package launch

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/terminal"
)

const renameCompletionBudget = 3 * time.Second

type sessionRename struct {
	command string
	config.RenameDelivery
}

func resolveSessionRename(agent config.AgentDefinition, prompt config.PromptDelivery) sessionRename {
	if agent.RenameCommand == nil || *agent.RenameCommand == "" {
		return sessionRename{}
	}
	policy := config.RenameDelivery{Ready: prompt.Ready, Blocked: prompt.Blocked}
	if agent.RenameDelivery != nil {
		policy = *agent.RenameDelivery
	}
	return sessionRename{command: *agent.RenameCommand, RenameDelivery: policy}
}

func (plan LaunchPlan) renameMovesPrompt() bool {
	return plan.rename.command != "" && !plan.rename.AfterArgv &&
		plan.PromptDelivery.Mode != "pane" && plan.capabilities().Container
}

func (plan LaunchPlan) usesPanePrompt() bool {
	return plan.PromptDelivery.Mode == "pane" || plan.renameMovesPrompt()
}

func sessionName(label string) string {
	var name strings.Builder
	dash := false
	for _, r := range label {
		switch {
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			continue
		case unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsDigit(r) || r == '_' || r == '.':
			if dash && name.Len() > 0 {
				name.WriteByte('-')
			}
			name.WriteRune(r)
			dash = false
		default:
			dash = true
		}
	}
	return name.String()
}

type renameProbe struct {
	plan     LaunchPlan
	outcome  LaunchOutcome
	ready    func(string) bool
	blocked  []blockedRule
	complete func(string) bool
}

func newRenameProbe(plan LaunchPlan, outcome LaunchOutcome, name string) (renameProbe, error) {
	p := renameProbe{plan: plan, outcome: outcome}
	var err error
	if plan.rename.Ready != nil {
		p.ready, err = compileDeliveryMatch(plan.rename.Ready.Match)
		if err != nil {
			return p, err
		}
	}
	p.blocked, err = compileBlockedRules(plan.rename.Blocked)
	if err != nil {
		return p, err
	}
	if match := plan.rename.Complete; match != "" {
		if strings.HasPrefix(match, "regex:") {
			name = regexp.QuoteMeta(name)
		}
		p.complete, err = compileDeliveryMatch(strings.ReplaceAll(match, "{name}", name))
	}
	return p, err
}

func renameAgentSession(plan LaunchPlan, outcome LaunchOutcome, label string) error {
	name := sessionName(label)
	probe, err := newRenameProbe(plan, outcome, name)
	if err == nil {
		err = probe.waitReady(plan.renameMovesPrompt())
	}
	if err != nil {
		if plan.renameMovesPrompt() {
			return err
		}
		plan.warn(t("launch.rename_not_ready"))
		return nil
	}
	command := strings.ReplaceAll(plan.rename.command, "{name}", name)
	if name == "" || rejectTypedControlChars(command) != nil {
		plan.warn(t("launch.rename_unconfirmed"))
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), terminal.DefaultReadyTimeout)
	err = plan.backend().DeliverText(ctx, boundedSpawnConn(plan), outcome.Pane, command)
	cancel()
	if err != nil {
		plan.warn(t("launch.rename_unconfirmed"))
		return nil
	}
	confirmed, blocked := probe.waitComplete(command)
	if !confirmed {
		plan.warn(t("launch.rename_unconfirmed"))
	}
	if blocked != "" && plan.renameMovesPrompt() {
		// A dialog can also appear as the rename completes. Do not send the
		// task prompt into it while waiting for an operator to dismiss it.
		return probe.waitReady(true)
	}
	return nil
}

func (p renameProbe) waitReady(waitBlocked bool) error {
	timeout := terminal.DefaultReadyTimeout
	if ready := p.plan.rename.Ready; ready != nil {
		timeout = time.Duration(ready.TimeoutMS) * time.Millisecond
	} else if !p.plan.capabilities().ProgramStatus {
		return launchError("launch.pane_ready_timeout", "")
	}
	timeout = p.plan.launchTimeout(timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	deadline := nowFn().Add(timeout)
	var output, blocked string
	for {
		if view, ready, blocker, ok := p.observe(ctx); ok {
			output, blocked = view, blocker
			if blocked != "" && !waitBlocked {
				break
			}
			if ready && ctx.Err() == nil && deadline.After(nowFn()) {
				return nil
			}
		}
		if !pauseRename(ctx, deadline) {
			break
		}
	}
	if blocked != "" {
		return launchError("launch.pane_ready_blocked", blocked, output)
	}
	return launchError("launch.pane_ready_timeout", output)
}

// The fallback gives the UI a short moment to handle Enter, then looks for a
// ready composer without the command still sitting in its draft. Built-ins
// can instead match their specific acknowledgement.
func (p renameProbe) waitComplete(command string) (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), renameCompletionBudget)
	defer cancel()
	deadline := nowFn().Add(renameCompletionBudget)
	if p.complete == nil {
		sleepFn(2 * notifyPollInterval)
	}
	var blocked string
	for {
		if view, ready, blocker, ok := p.observe(ctx); ok {
			blocked = blocker
			if ready && ctx.Err() == nil && deadline.After(nowFn()) && ((p.complete != nil && p.complete(view)) ||
				(p.complete == nil && !strings.Contains(view, command))) {
				return true, ""
			}
		}
		if !pauseRename(ctx, deadline) {
			return false, blocked
		}
	}
}

func (p renameProbe) observe(ctx context.Context) (view string, ready bool, blocked string, ok bool) {
	backend := p.plan.backend()
	conn := boundedSpawnConn(p.plan)
	output, err := backend.ReadOutput(ctx, conn, p.outcome.Pane)
	if err != nil {
		return "", false, "", false
	}
	for _, rule := range p.blocked {
		if rule.match(output) {
			return output, false, rule.reason, true
		}
	}
	if p.ready != nil && !p.ready(output) {
		return output, false, "", true
	}
	if backend.Capabilities().ProgramStatus {
		facts, err := backend.PaneFacts(ctx, conn, p.outcome.Pane)
		ready = err == nil && !facts.Gone && p.plan.programStatusApp != "" &&
			facts.AgentApp == p.plan.programStatusApp &&
			(facts.AgentStatus == "idle" || facts.AgentStatus == "done" ||
				(p.plan.rename.AfterArgv && facts.AgentStatus == "working"))
		return output, ready, "", true
	}
	return output, p.ready != nil, "", true
}

func pauseRename(ctx context.Context, deadline time.Time) bool {
	remaining := deadline.Sub(nowFn())
	if ctx.Err() != nil || remaining <= 0 {
		return false
	}
	sleepFn(min(notifyPollInterval, remaining))
	return true
}
