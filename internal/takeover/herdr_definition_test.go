package takeover

import (
	"context"
	"os"
	"testing"

	"github.com/dualface/kander/internal/launch"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
	"github.com/dualface/kander/internal/terminal/builtin"
)

// exitResponseBackend makes the malformed response arrive after the exit prompt.
type exitResponseBackend struct {
	terminal.Backend
	failure          error
	prompted, closed bool
}

func (b *exitResponseBackend) PaneFacts(context.Context, terminal.Conn, string) (terminal.PaneFacts, error) {
	if b.prompted {
		return terminal.PaneFacts{}, b.failure
	}
	return terminal.PaneFacts{Agent: "claude", AgentStatus: "idle", AgentSession: "s1", Container: "w1:t1"}, nil
}
func (b *exitResponseBackend) Topology(context.Context, terminal.Conn, terminal.Address) (terminal.Topology, error) {
	return terminal.Topology{Container: "w1:t1", Panes: []string{"w1:p1"}}, nil
}
func (b *exitResponseBackend) DeliverText(context.Context, terminal.Conn, string, string) error {
	b.prompted = true
	return nil
}
func (b *exitResponseBackend) CloseContainer(context.Context, terminal.Conn, terminal.Address) error {
	b.closed = true
	return nil
}

func TestHerdrStringResultStillRetainsContainer(t *testing.T) {
	setupBoard(t)
	backend, err := builtin.DefinitionBackend("herdr", "herdr", os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	conn := terminal.Conn{Run: func(context.Context, string, []string) (probe.Result, error) {
		return probe.Result{Stdout: `{"result":"invalid"}`}, nil
	}}
	_, err = backend.PaneFacts(context.Background(), conn, "w1:p1")
	command, ok := terminal.AsCommandError(err)
	if !ok || command.Kind != terminal.KindInvalidResponse {
		t.Fatalf("error=%v", err)
	}
	// The former backend used KindMissingResult. Both consumer branches must
	// return an error before closing, though their user-facing diagnostics differ.
	for _, kind := range []terminal.ErrorKind{terminal.KindMissingResult, command.Kind} {
		original := *command
		original.Kind = kind
		fake := &exitResponseBackend{Backend: backend, failure: &original}
		_, err := cleanupAgentContainer(fake, terminal.Address{Container: "w1:t1", Pane: "w1:p1"}, "herdr:w1:t1:w1:p1", launch.AgentSession{Agent: "claude", Reference: "s1"}, 61)
		if err == nil || !fake.prompted || fake.closed {
			t.Fatalf("kind=%v error=%v prompted=%v closed=%v", kind, err, fake.prompted, fake.closed)
		}
		t.Logf("kind=%v retains container: %v", kind, err)
	}
}
