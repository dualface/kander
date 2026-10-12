package tui

import (
	"context"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/internal/liveness"
	"github.com/dualface/kander/internal/terminal"
)

// agentStatusInterval is the fixed period of the automatic agent status
// observation. It is independent of tui.refresh: board reads default to 30
// seconds, which is too slow to notice a blocked agent.
const agentStatusInterval = 5 * time.Second

// agentClass is the display class of an observed executing agent.
type agentClass int

const (
	agentUnknown agentClass = iota
	agentWorking
	agentBlocked
	agentWaiting
	agentLost
)

// agentClassGlyphs keep every class distinguishable without color.
var agentClassGlyphs = map[agentClass]string{
	agentWorking: "◐",
	agentBlocked: "!",
	agentWaiting: "○",
	agentLost:    "×",
	agentUnknown: "?",
}

var agentClassLabels = map[agentClass]string{
	agentWorking: "tui.agent_status_working",
	agentBlocked: "tui.agent_status_blocked",
	agentWaiting: "tui.agent_status_waiting",
	agentLost:    "tui.agent_status_lost",
	agentUnknown: "tui.agent_status_unknown",
}

// agentStatus is the last observation of one card. window ties it to the
// WINDOW it was observed at, so a restarted card shows nothing stale.
type agentStatus struct {
	class    agentClass
	window   string
	detail   string
	observed time.Time
}

// agentStatusTracker schedules the observation of the cards whose executing
// agent runs in an AgentIdentity terminal. At most one observation is in
// flight; a result whose sequence is not the in-flight one is dropped.
type agentStatusTracker struct {
	statuses map[string]agentStatus
	seq      uint64
	inFlight uint64
	cancel   context.CancelFunc
	queued   bool
	// last is the start of the latest observation; automatic observations
	// start at least agentStatusInterval apart.
	last time.Time
}

type agentStatusResult struct {
	seq     uint64
	inputs  []liveness.AgentStatusInput
	reports []liveness.Report
}

// observeAgentStatus is the production observer bound by Run.
func observeAgentStatus(ctx context.Context, inputs []liveness.AgentStatusInput) []liveness.Report {
	return liveness.ObserveAgentStatus(ctx, inputs, liveness.BatchOptions{})
}

func agentClassOf(report liveness.Report) agentClass {
	switch report.Status {
	case liveness.Stopped:
		return agentLost
	case liveness.Alive:
		switch report.RuntimeState {
		case "working":
			return agentWorking
		case "blocked":
			return agentBlocked
		case "idle", "done":
			// herdr "done" means the agent finished its turn, not the card.
			return agentWaiting
		}
	}
	return agentUnknown
}

// agentObservable reports whether a card's executing agent can be observed:
// it is in flight and its WINDOW names an AgentIdentity terminal.
func agentObservable(task Task) bool {
	if task.State != "working" && task.State != "review" {
		return false
	}
	backend, _, ok := terminal.ParseWindow(task.Window)
	return ok && backend.Capabilities().AgentIdentity
}

// agentStatusTick queues an automatic observation once the interval passed.
func (a *App) agentStatusTick() {
	if a.Now().Sub(a.agents.last) >= agentStatusInterval {
		a.agents.queued = true
	}
}

// requestAgentStatus queues an immediate observation (the r key).
func (a *App) requestAgentStatus() {
	a.agents.queued = true
}

func (a *App) agentStatusInputs() []liveness.AgentStatusInput {
	var inputs []liveness.AgentStatusInput
	for _, task := range a.Model.Tasks {
		if agentObservable(task) {
			inputs = append(inputs, liveness.AgentStatusInput{TaskID: task.TaskID, Session: task.Session, Window: task.Window})
		}
	}
	return inputs
}

// takeAgentStatus starts the queued observation. Without observable cards it
// starts no terminal process and forgets earlier statuses.
func (a *App) takeAgentStatus() tea.Cmd {
	if !a.agents.queued || a.agents.inFlight != 0 || a.ObserveAgents == nil {
		return nil
	}
	a.agents.queued = false
	inputs := a.agentStatusInputs()
	if len(inputs) == 0 {
		a.agents.statuses = nil
		return nil
	}
	a.agents.last = a.Now()
	a.agents.seq++
	seq := a.agents.seq
	a.agents.inFlight = seq
	ctx, cancel := context.WithCancel(context.Background())
	a.agents.cancel = cancel
	observe := a.ObserveAgents
	return func() tea.Msg {
		return workMsg{payload: agentStatusResult{seq: seq, inputs: inputs, reports: observe(ctx, inputs)}}
	}
}

func (a *App) applyAgentStatus(result agentStatusResult) {
	if result.seq != a.agents.inFlight {
		return
	}
	a.agents.inFlight = 0
	a.agents.cancel = nil
	statuses := make(map[string]agentStatus, len(result.inputs))
	for index, input := range result.inputs {
		if index >= len(result.reports) {
			break
		}
		report := result.reports[index]
		statuses[input.TaskID] = agentStatus{
			class: agentClassOf(report), window: input.Window,
			detail: sanitizeAgentDetail(report.Detail), observed: report.ObservedAt,
		}
	}
	a.agents.statuses = statuses
}

// cancelAgentStatus cancels the in-flight observation and drops its result.
func (a *App) cancelAgentStatus() {
	if a.agents.cancel != nil {
		a.agents.cancel()
		a.agents.cancel = nil
	}
	a.agents.inFlight = 0
	a.agents.queued = false
	a.agents.seq++
}

// sanitizeAgentDetail keeps terminal-reported text on one plain line.
func sanitizeAgentDetail(detail string) string {
	plain := ansi.Strip(detail)
	plain = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, plain)
	return strings.Join(strings.Fields(plain), " ")
}

// agentStatusFor returns the observation of a card still observed at the
// same WINDOW.
func (a *App) agentStatusFor(task Task) (agentStatus, bool) {
	status, ok := a.agents.statuses[task.TaskID]
	if !ok || status.window != task.Window || (task.State != "working" && task.State != "review") {
		return agentStatus{}, false
	}
	return status, true
}

// agentMarkText is the card title prefix, empty for an unobserved card.
func (a *App) agentMarkText(task Task) string {
	status, ok := a.agentStatusFor(task)
	if !ok {
		return ""
	}
	return agentClassGlyphs[status.class] + " "
}

func agentClassColor(p palette, class agentClass) lipgloss.Color {
	switch class {
	case agentWorking:
		return p.OK
	case agentBlocked, agentLost:
		return p.Warn
	case agentWaiting:
		return p.Accent
	}
	return p.Dim
}

// renderAgentMark colors the mark at the start of a card title line; body is
// the padded plain line.
func (a *App) renderAgentMark(p palette, task Task, body string, style lipgloss.Style) (string, bool) {
	status, ok := a.agentStatusFor(task)
	if !ok {
		return "", false
	}
	glyph := agentClassGlyphs[status.class]
	rest, found := strings.CutPrefix(body, glyph)
	if !found {
		return "", false
	}
	mark := style.Foreground(agentClassColor(p, status.class)).Bold(true)
	return mark.Render(glyph) + style.Render(rest), true
}

// blockedCount counts the cards of a panel whose agent is blocked.
func (a *App) blockedCount(tasks []Task) int {
	count := 0
	for _, task := range tasks {
		if status, ok := a.agentStatusFor(task); ok && status.class == agentBlocked {
			count++
		}
	}
	return count
}

// agentStatusLine is the detail view description of the observation.
func (a *App) agentStatusLine(task Task) string {
	status, ok := a.agentStatusFor(task)
	if !ok {
		return ""
	}
	observed := ""
	if !status.observed.IsZero() {
		observed = status.observed.Local().Format("15:04:05")
	}
	return t("tui.agent_status_line", agentClassGlyphs[status.class], t(agentClassLabels[status.class]), observed, status.detail)
}

// agentStatusHelpEntries is the help legend of the card marks.
func agentStatusHelpEntries() []helpEntry {
	entries := make([]helpEntry, 0, len(agentClassGlyphs))
	for _, class := range []agentClass{agentWorking, agentBlocked, agentWaiting, agentLost, agentUnknown} {
		entries = append(entries, helpEntry{agentClassGlyphs[class], t(agentClassLabels[class])})
	}
	return entries
}
