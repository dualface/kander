package tui

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/internal/liveness"
)

const (
	agentWindowOne = "herdr:w1:t1:w1:p1"
	agentWindowTwo = "herdr:w1:t2:w1:p2"
)

// agentObserver is a fake ObserveAgents that records every call and answers
// with the reports its reply function builds.
type agentObserver struct {
	mu    sync.Mutex
	calls [][]liveness.AgentStatusInput
	reply func(liveness.AgentStatusInput) liveness.Report
}

func (o *agentObserver) observe(_ context.Context, inputs []liveness.AgentStatusInput) []liveness.Report {
	o.mu.Lock()
	o.calls = append(o.calls, inputs)
	o.mu.Unlock()
	reports := make([]liveness.Report, len(inputs))
	for index, input := range inputs {
		reports[index] = o.reply(input)
	}
	return reports
}

func (o *agentObserver) callCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.calls)
}

func aliveReport(runtime string) liveness.Report {
	return liveness.Report{Status: liveness.Alive, RuntimeState: runtime, Detail: "Agent status=" + runtime, ObservedAt: time.Unix(1_700_000_000, 0)}
}

// agentTestApp shows the given cards and observes them with fake reports.
func agentTestApp(tasks []Task, reply func(liveness.AgentStatusInput) liveness.Report) (*App, *agentObserver, *time.Time) {
	app := refreshTestApp(func() (BoardPayload, error) { return BoardPayload{Tasks: tasks}, nil }, nil)
	app.Model.SetBoard(BoardPayload{Tasks: tasks})
	app.LastRefresh = app.Now()
	now := app.Now()
	app.Now = func() time.Time { return now }
	observer := &agentObserver{reply: reply}
	app.ObserveAgents = observer.observe
	return app, observer, &now
}

// working answers every input with a working agent.
func working(liveness.AgentStatusInput) liveness.Report { return aliveReport("working") }

func inFlightTasks() []Task {
	return []Task{
		{TaskID: "20260915-working-task", Title: "working", State: "working", Session: "claude s1", Window: agentWindowOne},
		{TaskID: "20260915-review-task", Title: "review", State: "review", Session: "claude s2", Window: agentWindowTwo},
	}
}

func TestAgentStatusClassifiesReports(t *testing.T) {
	for _, tc := range []struct {
		report liveness.Report
		want   agentClass
	}{
		{aliveReport("working"), agentWorking},
		{aliveReport("blocked"), agentBlocked},
		{aliveReport("idle"), agentWaiting},
		{aliveReport("done"), agentWaiting},
		{aliveReport("unknown"), agentUnknown},
		{liveness.Report{Status: liveness.Stopped}, agentLost},
		{liveness.Report{Status: liveness.Unknown}, agentUnknown},
	} {
		if got := agentClassOf(tc.report); got != tc.want {
			t.Fatalf("%+v: class %d, want %d", tc.report, got, tc.want)
		}
	}
	glyphs := map[string]bool{}
	for _, glyph := range agentClassGlyphs {
		glyphs[glyph] = true
	}
	if len(glyphs) != 5 {
		t.Fatalf("display classes share glyphs: %v", agentClassGlyphs)
	}
}

func TestAgentStatusSkipsCardsWithoutObservableAgent(t *testing.T) {
	app, observer, _ := agentTestApp([]Task{
		{TaskID: "20260915-todo-task", State: "todo", Session: "claude s1", Window: agentWindowOne},
		{TaskID: "20260915-tmux-task", State: "working", Session: "claude s1", Window: "tmux:$1:@1:%1"},
		{TaskID: "20260915-self-task", State: "working"},
	}, working)
	program{app: app}.Update(tickMsg(app.Now()))
	finishQueuedWork(t, app)
	if observer.callCount() != 0 {
		t.Fatalf("observed without observable cards: %v", observer.calls)
	}
}

func TestAgentStatusObservesOnIntervalAndShowsMarks(t *testing.T) {
	app, observer, now := agentTestApp(inFlightTasks(), func(input liveness.AgentStatusInput) liveness.Report {
		if input.TaskID == "20260915-review-task" {
			return aliveReport("blocked")
		}
		return aliveReport("working")
	})
	program{app: app}.Update(tickMsg(*now))
	if app.agents.inFlight == 0 {
		t.Fatal("tick Update did not start an observation")
	}
	app.cancelAgentStatus()
	app.agents.last = time.Time{}
	app.agentStatusTick()
	finishQueuedWork(t, app)
	if observer.callCount() != 1 || len(observer.calls[0]) != 2 {
		t.Fatalf("calls=%v", observer.calls)
	}
	if got := observer.calls[0][0]; got.Session != "claude s1" || got.Window != agentWindowOne {
		t.Fatalf("input=%+v", got)
	}
	for _, step := range []time.Duration{time.Second, 3 * time.Second} {
		*now = now.Add(step)
		app.agentStatusTick()
		finishQueuedWork(t, app)
	}
	if observer.callCount() != 1 {
		t.Fatalf("observed before the interval: %d", observer.callCount())
	}
	*now = now.Add(time.Second)
	app.agentStatusTick()
	finishQueuedWork(t, app)
	if observer.callCount() != 2 {
		t.Fatalf("did not observe after the interval: %d", observer.callCount())
	}

	working, review := app.Model.Tasks[0], app.Model.Tasks[1]
	if !strings.HasPrefix(app.boardCardLines(working, 40)[0], agentClassGlyphs[agentWorking]+" working") {
		t.Fatalf("title=%q", app.boardCardLines(working, 40)[0])
	}
	if app.blockedCount([]Task{working, review}) != 1 {
		t.Fatal("blocked card not counted")
	}
	app.Model.FocusState("review")
	if view := ansi.Strip(app.View()); !strings.Contains(view, "1 "+agentClassGlyphs[agentBlocked]+"1") {
		t.Fatalf("panel title lacks the blocked count:\n%s", view)
	}
}

func TestAgentStatusRefreshKeyAndInFlightLimit(t *testing.T) {
	started, release := make(chan struct{}, 4), make(chan struct{})
	app, observer, now := agentTestApp(inFlightTasks(), working)
	app.ObserveAgents = func(ctx context.Context, inputs []liveness.AgentStatusInput) []liveness.Report {
		started <- struct{}{}
		<-release
		return observer.observe(ctx, inputs)
	}
	p := program{app: app}
	app.agentStatusTick()
	cmd := app.takeAgentStatus()
	if cmd == nil {
		t.Fatal("tick did not start an observation")
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	<-started
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !app.Searching {
		t.Fatal("observation blocked Update")
	}
	p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*now = now.Add(agentStatusInterval)
	app.agentStatusTick()
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if app.takeAgentStatus() != nil {
		t.Fatal("a second observation started while one was in flight")
	}
	first := app.agents.inFlight
	close(release)
	p.Update(<-done)
	if _, ok := app.agentStatusFor(app.Model.Tasks[0]); !ok {
		t.Fatal("result was not applied")
	}
	if app.agents.inFlight == 0 || app.agents.inFlight == first {
		t.Fatal("r did not start an observation after the in-flight one")
	}
}

func TestAgentStatusDropsStaleAndCancelsOnQuit(t *testing.T) {
	app, _, _ := agentTestApp(inFlightTasks(), working)
	canceled := make(chan struct{})
	app.ObserveAgents = func(ctx context.Context, inputs []liveness.AgentStatusInput) []liveness.Report {
		<-ctx.Done()
		close(canceled)
		return []liveness.Report{aliveReport("working"), aliveReport("working")}
	}
	app.requestAgentStatus()
	cmd := app.takeAgentStatus()
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	app.requestQuit()
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("quit left the observation running")
	}
	app.applyWork((<-result).(workMsg).payload)
	if len(app.agents.statuses) != 0 {
		t.Fatal("a canceled observation was applied")
	}
}

func TestAgentStatusMarkFollowsWindowAndKeepsGeometry(t *testing.T) {
	app, _, _ := agentTestApp(inFlightTasks(), working)
	app.requestAgentStatus()
	finishQueuedWork(t, app)
	task := app.Model.Tasks[0]
	p := themePalette(app.themeName())
	marked := app.renderCard(p, task.State, task, 30, false, groupRail{})
	plain := task
	plain.Window = agentWindowTwo
	unmarked := app.renderCard(p, plain.State, plain, 30, false, groupRail{})
	if strings.Contains(ansi.Strip(unmarked[0]), agentClassGlyphs[agentWorking]) {
		t.Fatal("a status observed at another WINDOW was shown")
	}
	for index := range marked {
		if displayWidth(ansi.Strip(marked[index])) != displayWidth(ansi.Strip(unmarked[index])) {
			t.Fatalf("line %d width %q vs %q", index, ansi.Strip(marked[index]), ansi.Strip(unmarked[index]))
		}
	}
	if line := ansi.Strip(marked[0]); !strings.HasPrefix(line, " "+agentClassGlyphs[agentWorking]+" ") {
		t.Fatalf("mark line %q", line)
	}
	if narrow := app.boardCardLines(task, 1)[0]; displayWidth(narrow) > 1 {
		t.Fatalf("narrow card overflowed: %q", narrow)
	}
}

func TestAgentStatusDetailLineIsSanitized(t *testing.T) {
	app, _, _ := agentTestApp(inFlightTasks(), func(liveness.AgentStatusInput) liveness.Report {
		report := aliveReport("idle")
		report.Detail = "pane\x1b[31m red\x1b[0m\x07 gone\nnext"
		return report
	})
	app.requestAgentStatus()
	finishQueuedWork(t, app)
	line := app.agentStatusLine(app.Model.Tasks[0])
	if !strings.Contains(line, "pane red gone next") || strings.ContainsAny(line, "\x1b\x07\n") {
		t.Fatalf("line=%q", line)
	}
	rule := ansi.Strip(app.detailRule(themePalette(app.themeName()), app.Model.Tasks[0], 60))
	if displayWidth(rule) != 60 || !strings.Contains(rule, agentClassGlyphs[agentWaiting]) {
		t.Fatalf("rule=%q", rule)
	}
}

func TestAgentStatusHelpLegendListsEveryMark(t *testing.T) {
	app, _, _ := agentTestApp(inFlightTasks(), working)
	groups := app.boardHelpGroups()
	legend := groups[len(groups)-1].Entries
	if len(legend) != len(agentClassGlyphs) {
		t.Fatalf("legend=%v", legend)
	}
	for index, class := range []agentClass{agentWorking, agentBlocked, agentWaiting, agentLost, agentUnknown} {
		if legend[index].Keys != agentClassGlyphs[class] || legend[index].Desc == "" {
			t.Fatalf("entry %d=%+v", index, legend[index])
		}
	}
}
