package liveness

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/dualface/kander/internal/terminal"
)

// fakeAgentBackend answers agent lists and pane facts from memory and counts
// the calls. Unused Backend methods stay nil and would panic if called.
type fakeAgentBackend struct {
	terminal.Backend
	name     string
	identity bool
	lister   bool
	listErr  error
	panes    []terminal.AgentPane
	facts    map[string]terminal.PaneFacts

	mu         sync.Mutex
	listCalls  int
	factsCalls []string
}

func (b *fakeAgentBackend) Name() string       { return b.name }
func (b *fakeAgentBackend) Executable() string { return b.name }
func (b *fakeAgentBackend) Capabilities() terminal.Capabilities {
	return terminal.Capabilities{Container: true, AgentIdentity: b.identity}
}

func (b *fakeAgentBackend) PaneFacts(_ context.Context, _ terminal.Conn, pane string) (terminal.PaneFacts, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.factsCalls = append(b.factsCalls, pane)
	facts, ok := b.facts[pane]
	if !ok {
		return terminal.PaneFacts{Gone: true}, nil
	}
	return facts, nil
}

func (b *fakeAgentBackend) AgentList(context.Context, terminal.Conn) ([]terminal.AgentPane, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listCalls++
	if !b.lister {
		return nil, terminal.ErrUnsupported
	}
	return b.panes, b.listErr
}

// useFakeBackends routes WINDOW values "<name>:<pane>" to the given backends.
func useFakeBackends(t *testing.T, backends ...*fakeAgentBackend) {
	t.Helper()
	resetLang(t)
	byName := map[string]*fakeAgentBackend{}
	for _, backend := range backends {
		byName[backend.name] = backend
	}
	originalResolve, originalLook := resolveWindow, lookPath
	resolveWindow = func(window string) (terminal.Backend, terminal.Address, bool) {
		name, pane, ok := strings.Cut(window, ":")
		backend, known := byName[name]
		if !ok || !known {
			return nil, terminal.Address{}, false
		}
		return backend, terminal.Address{Container: "tab", Pane: pane}, true
	}
	lookPath = func(name string) (string, error) { return name, nil }
	t.Cleanup(func() { resolveWindow, lookPath = originalResolve, originalLook })
}

func TestObserveAgentStatusListsEachBackendOnce(t *testing.T) {
	herdr := &fakeAgentBackend{name: "herdr", identity: true, lister: true, panes: []terminal.AgentPane{
		{Pane: "p1", Agent: "claude", AgentStatus: "working", AgentSession: "s1", AgentSessionKind: "id"},
		{Pane: "p2", Agent: "claude", AgentStatus: "blocked", AgentSession: "s2", AgentSessionKind: "id"},
		{Pane: "p3", Agent: "claude", AgentStatus: "idle"},
		{Pane: "p4", Agent: "claude", AgentStatus: "done", AgentSession: "other", AgentSessionKind: "id"},
		{Pane: "p5", Agent: "codex", AgentStatus: "working"},
	}}
	useFakeBackends(t, herdr)
	reports := ObserveAgentStatus(context.Background(), []AgentStatusInput{
		{TaskID: "working", Session: "claude s1", Window: "herdr:p1"},
		{TaskID: "blocked", Session: "claude s2", Window: "herdr:p2"},
		{TaskID: "no-session-report", Session: "claude s3", Window: "herdr:p3"},
		{TaskID: "session-differs", Session: "claude s4", Window: "herdr:p4"},
		{TaskID: "agent-differs", Session: "claude s5", Window: "herdr:p5"},
	}, BatchOptions{})
	want := []struct{ status, runtime string }{
		{Alive, "working"}, {Alive, "blocked"}, {Alive, "idle"}, {Stopped, Unknown}, {Stopped, Unknown},
	}
	for index, rep := range reports {
		if rep.Status != want[index].status || rep.RuntimeState != want[index].runtime || rep.ObservedAt.IsZero() {
			t.Fatalf("%s: %+v; want %+v", rep.TaskID, rep, want[index])
		}
	}
	if herdr.listCalls != 1 || len(herdr.factsCalls) != 0 {
		t.Fatalf("list=%d facts=%v", herdr.listCalls, herdr.factsCalls)
	}
}

func TestObserveAgentStatusFallsBackToPaneFacts(t *testing.T) {
	plain := &fakeAgentBackend{name: "plain", identity: true, facts: map[string]terminal.PaneFacts{
		"p1": {Agent: "claude", AgentStatus: "done"},
	}}
	listed := &fakeAgentBackend{name: "listed", identity: true, lister: true, facts: map[string]terminal.PaneFacts{
		"moved": {Agent: "claude", AgentStatus: "working"},
	}}
	useFakeBackends(t, plain, listed)
	reports := ObserveAgentStatus(context.Background(), []AgentStatusInput{
		{TaskID: "unsupported", Session: "claude", Window: "plain:p1"},
		{TaskID: "not-listed", Session: "claude", Window: "listed:moved"},
		{TaskID: "gone", Session: "claude", Window: "listed:gone"},
	}, BatchOptions{})
	if reports[0].Status != Alive || reports[0].RuntimeState != "done" {
		t.Fatalf("unsupported list: %+v", reports[0])
	}
	if reports[1].Status != Alive || reports[1].RuntimeState != "working" {
		t.Fatalf("pane missing from list: %+v", reports[1])
	}
	if reports[2].Status != Stopped {
		t.Fatalf("gone pane: %+v", reports[2])
	}
	if len(plain.factsCalls) != 1 || listed.listCalls != 1 || len(listed.factsCalls) != 2 {
		t.Fatalf("plain=%v listed list=%d facts=%v", plain.factsCalls, listed.listCalls, listed.factsCalls)
	}
}

func TestObserveAgentStatusIsolatesFailedList(t *testing.T) {
	broken := &fakeAgentBackend{name: "broken", identity: true, lister: true, listErr: errors.New("socket refused")}
	healthy := &fakeAgentBackend{name: "healthy", identity: true, lister: true, panes: []terminal.AgentPane{
		{Pane: "p1", Agent: "claude", AgentStatus: "working"},
	}}
	useFakeBackends(t, broken, healthy)
	reports := ObserveAgentStatus(context.Background(), []AgentStatusInput{
		{TaskID: "broken", Session: "claude", Window: "broken:p1"},
		{TaskID: "healthy", Session: "claude", Window: "healthy:p1"},
	}, BatchOptions{})
	if reports[0].Status != Unknown || !strings.Contains(reports[0].Detail, "socket refused") || len(broken.factsCalls) != 0 {
		t.Fatalf("broken: %+v facts=%v", reports[0], broken.factsCalls)
	}
	if reports[1].Status != Alive || reports[1].RuntimeState != "working" {
		t.Fatalf("healthy: %+v", reports[1])
	}
}

func TestObserveAgentStatusRejectsUnobservableInputs(t *testing.T) {
	tmux := &fakeAgentBackend{name: "tmux", lister: true}
	useFakeBackends(t, tmux)
	reports := ObserveAgentStatus(context.Background(), []AgentStatusInput{
		{TaskID: "no-session", Window: "tmux:p1"},
		{TaskID: "bad-window", Session: "claude", Window: "nowhere"},
		{TaskID: "no-identity", Session: "claude", Window: "tmux:p1"},
	}, BatchOptions{})
	for _, rep := range reports {
		if rep.Status != Unknown || rep.ObservationValid {
			t.Fatalf("%s: %+v", rep.TaskID, rep)
		}
	}
	if tmux.listCalls != 0 || len(tmux.factsCalls) != 0 {
		t.Fatalf("probed an unobservable card: list=%d facts=%v", tmux.listCalls, tmux.factsCalls)
	}
}

func TestObserveAgentStatusSkipsProbesAfterDeadline(t *testing.T) {
	plain := &fakeAgentBackend{name: "plain", identity: true}
	useFakeBackends(t, plain)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reports := ObserveAgentStatus(ctx, []AgentStatusInput{{TaskID: "late", Session: "claude", Window: "plain:p1"}}, BatchOptions{})
	if reports[0].Status != Unknown || len(plain.factsCalls) != 0 {
		t.Fatalf("report=%+v facts=%v", reports[0], plain.factsCalls)
	}
}
