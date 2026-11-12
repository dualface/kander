package takeover

import (
	"github.com/dualface/kander/internal/launch"
	"github.com/dualface/kander/internal/terminal"
	"os"
	"strings"
	"testing"
)

func TestWorkerStopsBusySessionAndPreservesItsContainer(t *testing.T) {
	setupBoard(t)
	t.Setenv("KANBAN_HERDR_AGENT", "claude")
	t.Setenv("KANBAN_HERDR_STATUS", "working")
	backend, address, ok := terminal.ParseWindow("herdr:w1:t8:w1:p8")
	if !ok {
		t.Fatal("fixture address")
	}
	program, err := lookPath(backend.Executable())
	if err != nil {
		t.Fatal(err)
	}
	// Hide the optional process inspector to exercise the supported exit-command
	// fallback of a backend with session facts alone.
	plain := struct{ terminal.Backend }{backend}
	if err := stopWorkerPane(t.Context(), plain, program, address, launch.AgentSession{Agent: "claude", Reference: "session-1"}); err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(os.Getenv("KANBAN_HERDR_LOG") + ".prompt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "/exit") {
		t.Fatalf("missing exit delivery: %s", log)
	}
	if _, err := os.Stat(os.Getenv("KANBAN_HERDR_LOG") + ".close"); !os.IsNotExist(err) {
		t.Fatal("worker closed the retained container")
	}

}
func TestWorkerRefusesAReplacedTerminalSession(t *testing.T) {
	setupBoard(t)
	t.Setenv("KANBAN_HERDR_AGENT", "claude")
	t.Setenv("KANBAN_HERDR_SESSION", "replacement")
	if err := stopWorkerAgent(t.Context(), "herdr:w1:t8:w1:p8", "claude session-1"); err == nil {
		t.Fatal("stopped another session")
	}
	if _, err := os.Stat(os.Getenv("KANBAN_HERDR_LOG") + ".prompt"); !os.IsNotExist(err) {
		t.Fatal("delivered an exit to another session")
	}
}
