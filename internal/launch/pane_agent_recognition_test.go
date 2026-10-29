package launch

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/terminal"
)

// recognitionBackend reports the agent TUI as ready at once but names the pane
// agent only after unrecognized polls, like herdr while it detects a new agent.
type recognitionBackend struct {
	terminal.Backend
	name         string
	unrecognized int
	polls        int
	delivered    []string
	pollsAtSend  int
}

func (b *recognitionBackend) Name() string                        { return b.name }
func (b *recognitionBackend) Executable() string                  { return os.Args[0] }
func (b *recognitionBackend) AutoDetect(func(string) string) bool { return false }
func (b *recognitionBackend) Capabilities() terminal.Capabilities {
	return terminal.Capabilities{Container: true, AgentIdentity: true}
}

// ParseAddress claims only this backend's own addresses: the registry is
// global, so later tests probe every registered backend.
func (b *recognitionBackend) ParseAddress(value string) (terminal.Address, bool) {
	pane, ok := strings.CutPrefix(value, b.name+":")
	return terminal.Address{Pane: pane, Container: "tab"}, ok
}
func (b *recognitionBackend) ReadOutput(context.Context, terminal.Conn, string) (string, error) {
	return "? for help", nil
}
func (b *recognitionBackend) PaneFacts(context.Context, terminal.Conn, string) (terminal.PaneFacts, error) {
	b.polls++
	if b.polls <= b.unrecognized {
		return terminal.PaneFacts{}, nil
	}
	return terminal.PaneFacts{Agent: "droid", AgentStatus: "idle"}, nil
}
func (b *recognitionBackend) DeliverText(_ context.Context, _ terminal.Conn, _ string, text string) error {
	b.delivered = append(b.delivered, text)
	b.pollsAtSend = b.polls
	return nil
}

func recognitionPlan(t *testing.T, backend *recognitionBackend) LaunchPlan {
	t.Helper()
	terminal.Register(backend)
	previousNow, previousSleep := nowFn, sleepFn
	t.Cleanup(func() { nowFn, sleepFn = previousNow, previousSleep })
	clock := time.Unix(1_800_000_000, 0)
	nowFn = func() time.Time { return clock }
	sleepFn = func(d time.Duration) { clock = clock.Add(d) }
	return LaunchPlan{
		Launcher: backend.name,
		Prompt:   "Execute Kanban task",
		PromptDelivery: config.PromptDelivery{
			Mode:  "pane",
			Ready: &config.PromptReady{Match: "? for help", TimeoutMS: 2000},
		},
	}
}

func TestPaneDeliveryWaitsForTheBackendToRecognizeTheAgent(t *testing.T) {
	backend := &recognitionBackend{name: "recognition-late-" + terminal.ProjectKey(t.TempDir()), unrecognized: 3}
	plan := recognitionPlan(t, backend)
	if err := completePaneDelivery(plan, LaunchOutcome{Container: "tab", Pane: "pane"}); err != nil {
		t.Fatal(err)
	}
	if len(backend.delivered) != 1 || backend.delivered[0] != plan.Prompt {
		t.Fatalf("delivered=%q", backend.delivered)
	}
	if backend.pollsAtSend != 4 {
		t.Fatalf("prompt sent after %d pane polls, want the first recognized poll", backend.pollsAtSend)
	}
}

func TestPaneDeliveryStillSendsWhenTheAgentStaysUnrecognized(t *testing.T) {
	backend := &recognitionBackend{name: "recognition-never-" + terminal.ProjectKey(t.TempDir()), unrecognized: 1 << 30}
	plan := recognitionPlan(t, backend)
	if err := completePaneDelivery(plan, LaunchOutcome{Container: "tab", Pane: "pane"}); err != nil {
		t.Fatal(err)
	}
	if len(backend.delivered) != 1 {
		t.Fatalf("an unrecognized pane must still get the prompt at the deadline: %q", backend.delivered)
	}
	if want := int(2*time.Second/notifyPollInterval) + 1; backend.pollsAtSend != want {
		t.Fatalf("polled %d times, want %d within the ready deadline", backend.pollsAtSend, want)
	}
}
