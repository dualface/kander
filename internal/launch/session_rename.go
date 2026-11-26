package launch

import (
	"context"
	"regexp"
	"slices"
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
	complete *regexp.Regexp
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
		if pattern, ok := strings.CutPrefix(match, "regex:"); ok {
			match = strings.ReplaceAll(pattern, "{name}", regexp.QuoteMeta(name))
		} else {
			match = regexp.QuoteMeta(strings.ReplaceAll(match, "{name}", name))
		}
		p.complete, err = regexp.Compile(match)
	}
	return p, err
}

func renameAgentSession(plan LaunchPlan, outcome LaunchOutcome, label string) error {
	name := sessionName(label)
	probe, err := newRenameProbe(plan, outcome, name)
	var before string
	if err == nil {
		before, err = probe.waitReady(plan.renameMovesPrompt(), nil)
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
	confirmed, clean, output, blocked := probe.waitComplete(command, before, err == nil)
	if !confirmed {
		plan.warn(t("launch.rename_unconfirmed"))
	}
	if !plan.usesPanePrompt() || clean {
		return nil
	}
	if blocked != "" && plan.renameMovesPrompt() {
		// A dialog can also appear as the rename completes. Do not send the
		// task prompt into it while waiting for an operator to dismiss it.
		_, err := probe.waitReady(true, func(view string) bool { return !renameDraftVisible(before, view, command) })
		return err
	}
	if blocked != "" {
		return launchError("launch.pane_ready_blocked", blocked, trimNL(output))
	}
	return launchError("launch.pane_ready_timeout", trimNL(output))
}

func (p renameProbe) waitReady(waitBlocked bool, clean func(string) bool) (string, error) {
	timeout := terminal.DefaultReadyTimeout
	if ready := p.plan.rename.Ready; ready != nil {
		timeout = time.Duration(ready.TimeoutMS) * time.Millisecond
	} else if !p.plan.capabilities().ProgramStatus {
		return "", launchError("launch.pane_ready_timeout", "")
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
			if ready && (clean == nil || clean(view)) && ctx.Err() == nil && deadline.After(nowFn()) {
				return view, nil
			}
		}
		if !pauseRename(ctx, deadline) {
			break
		}
	}
	if blocked != "" {
		return "", launchError("launch.pane_ready_blocked", blocked, trimNL(output))
	}
	return "", launchError("launch.pane_ready_timeout", trimNL(output))
}

// A delivery error can leave the command in the composer (for example, tmux
// types the text and then sends Enter). Never append the task prompt to that
// draft. A new acknowledgement can explain an adjacent echo of the executed
// command, but it cannot make the rest of the screen safe on its own.
func (p renameProbe) waitComplete(command, before string, sent bool) (confirmed, clean bool, output, blocked string) {
	ctx, cancel := context.WithTimeout(context.Background(), renameCompletionBudget)
	defer cancel()
	deadline := nowFn().Add(renameCompletionBudget)
	if p.complete == nil || !sent {
		sleepFn(2 * notifyPollInterval)
	}
	var oldAcknowledgements []renameAcknowledgement
	if sent {
		oldAcknowledgements = p.acknowledgements(before, command)
	}
	for deadline.After(nowFn()) && ctx.Err() == nil {
		clean = false
		if view, ready, blocker, ok := p.observe(ctx); ok {
			output, blocked = view, blocker
			if ready && ctx.Err() == nil && deadline.After(nowFn()) {
				fresh := false
				if sent && p.complete != nil {
					var ack renameAcknowledgement
					ack, fresh = newRenameAcknowledgement(before, view, oldAcknowledgements, p.acknowledgements(view, command))
					if fresh && ack.hasEcho() {
						view = view[:ack.echoStart] + view[ack.lineStart:]
					}
				}
				clean = !renameDraftVisible(before, view, command)
				if clean && (!sent || p.complete == nil || fresh) {
					return sent, true, output, ""
				}
			}
		}
		if !pauseRename(ctx, deadline) {
			break
		}
	}
	return false, clean, output, blocked
}

type renameAcknowledgement struct {
	text      string
	echoStart int
	lineStart int
	row       int
}

func (ack renameAcknowledgement) hasEcho() bool { return ack.echoStart < ack.lineStart }

func (p renameProbe) acknowledgements(view, command string) []renameAcknowledgement {
	if p.complete == nil {
		return nil
	}
	var result []renameAcknowledgement
	for _, bounds := range p.complete.FindAllStringIndex(view, -1) {
		raw := view[bounds[0]:bounds[1]]
		content := strings.TrimLeftFunc(raw, unicode.IsSpace)
		if content == "" {
			continue
		}
		// A regular expression may include the preceding line break. Locate
		// the acknowledgement by its content, not by that delimiter.
		start := bounds[1] - len(content)
		line := strings.LastIndexByte(view[:start], '\n') + 1
		ack := renameAcknowledgement{
			text: strings.TrimSpace(content), echoStart: line, lineStart: line,
			row: strings.Count(view[:line], "\n"),
		}
		if line > 0 {
			previous := strings.LastIndexByte(view[:line-1], '\n') + 1
			if echo := strings.TrimSpace(view[previous : line-1]); strings.HasSuffix(echo, command) {
				ack.echoStart = previous
			}
		}
		result = append(result, ack)
	}
	return result
}

// A stationary acknowledgement cannot distinguish an adjacent draft from an
// echo. When a pair follows retained screen lines, the older acknowledgement
// may have scrolled out or moved to an earlier row in the retained region.
// At most one echo is excluded from the draft check.
func newRenameAcknowledgement(before, after string, old, current []renameAcknowledgement) (renameAcknowledgement, bool) {
	if len(current) == 0 {
		return renameAcknowledgement{}, false
	}
	texts := make(map[string]int)
	firstRows := make(map[string]int)
	type position struct {
		text string
		row  int
	}
	positions := make(map[position]bool)
	for _, ack := range old {
		if texts[ack.text] == 0 {
			firstRows[ack.text] = ack.row
		}
		texts[ack.text]++
		positions[position{ack.text, ack.row}] = true
	}
	var dropped, retained int
	var comparedScroll bool
	var result renameAcknowledgement
	for _, ack := range current {
		texts[ack.text]--
		fresh := texts[ack.text] < 0
		if sameRow := positions[position{ack.text, ack.row}]; ack.hasEcho() && (!fresh || sameRow) {
			if !comparedScroll {
				dropped, retained = renameScrollOverlap(before, after)
				comparedScroll = true
			}
			appended := retained > 0 && ack.row > retained
			replacesDropped := firstRows[ack.text] < dropped
			if sameRow {
				shifted := ack.row - dropped
				fresh = (fresh || replacesDropped) && appended && shifted >= 0 && shifted < retained &&
					slices.ContainsFunc(current, func(oldMatch renameAcknowledgement) bool {
						return oldMatch.row == shifted && oldMatch.text == ack.text
					})
			} else {
				fresh = appended && replacesDropped
			}
		}
		if fresh && (!result.hasEcho() || ack.hasEcho()) {
			result = ack
		}
	}
	return result, result.text != ""
}

// Match the longest nonblank suffix of the old viewport against the start of
// the new one. Require lines to leave the top and new lines to enter the
// bottom. Normalize line endings only for this comparison, not screen rules.
func renameScrollOverlap(before, after string) (dropped, retained int) {
	lines := func(view string) []string {
		return strings.Split(strings.TrimRight(strings.ReplaceAll(view, "\r\n", "\n"), "\n"), "\n")
	}
	old, current := lines(before), lines(after)
	for first := 1; first < len(old); first++ {
		kept := old[first:]
		if len(kept) < len(current) && slices.Equal(kept, current[:len(kept)]) &&
			slices.ContainsFunc(kept, func(line string) bool { return strings.TrimSpace(line) != "" }) {
			return first, len(kept)
		}
	}
	return 0, 0
}

func renameDraftVisible(before, current, command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	old := renameCommandLines(before, fields[0], command)
	for line, count := range renameCommandLines(current, fields[0], command) {
		if count > old[line] {
			return true
		}
	}
	return false
}

// Compare command-bearing lines with the pre-send view so unchanged help
// text or history is not mistaken for a draft. An exact old echo also binds
// the next line, which distinguishes an echo that scrolled away from an
// identical command now visible in the composer.
func renameCommandLines(output, word, command string) map[[2]string]int {
	lines := strings.Split(output, "\n")
	found := make(map[[2]string]int)
	for i, line := range lines {
		if !strings.Contains(line, word) {
			continue
		}
		key := [2]string{line, ""}
		if strings.Contains(line, command) && i+1 < len(lines) {
			key[1] = lines[i+1]
		}
		found[key]++
	}
	return found
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
			(facts.AgentStatus == "idle" || facts.AgentStatus == "done")
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
