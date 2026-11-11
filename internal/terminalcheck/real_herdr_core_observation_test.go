//go:build unix

package terminalcheck

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/terminal"
)

// Exercise OSC reports, labels, foreground facts and delivery against hc's
// real public JSON. The program is a local shell, not a paid agent session.
func TestHerdrCoreObservedAgentLifecycle(t *testing.T) {
	backend, conn := herdrCoreE2E(t, nil)
	cwd := t.TempDir()
	script := filepath.Join(cwd, "agent.sh")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
printf '\033]7501;app=claude-code:state=idle\007'
while IFS= read -r line; do
  printf 'received:%s\n' "$line"
done
`), 0o600); err != nil {
		t.Fatal(err)
	}
	address, err := backend.CreateContainer(conn, terminal.Target{}, cwd, "kander-observation-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := backend.CloseContainer(context.Background(), conn, address); err != nil {
			t.Errorf("close test terminal: %v", err)
		}
	})
	if err := backend.WaitReady(conn, address.Pane, int(terminal.DefaultReadyTimeout/time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := backend.RunCommand(conn, address.Pane, "'"+cwd+"'", "/bin/sh '"+script+"'", true); err != nil {
		t.Fatal(err)
	}
	if err := backend.SetSessionMarker(conn, address.Pane, "test-session"); err != nil {
		t.Fatal(err)
	}
	// Labels can share names with terminal fields without changing them.
	labels, err := conn.Run(t.Context(), conn.Program, []string{"label", "--json", address.Pane, "status=ready", "address=remote/t8", "state=lost"})
	if err != nil || labels.Code != 0 {
		t.Fatalf("set same-name labels: %+v, %v", labels, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		panes, err := backend.(terminal.AgentLister).AgentList(t.Context(), conn)
		if err != nil {
			t.Fatal(err)
		}
		if len(panes) == 1 && panes[0].Facts().AgentApp == "claude-code" {
			facts := panes[0].Facts()
			if facts.AgentStatus != "idle" || facts.AgentSession != "test-session" || !facts.ProgramStatus {
				t.Fatalf("batch facts=%+v", facts)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("OSC report did not arrive: %+v", panes)
		}
		time.Sleep(100 * time.Millisecond)
	}
	got, err := backend.ReverseLookup(t.Context(), conn, terminal.Identity{Agent: "claude", Reference: "test-session"})
	if err != nil || got != address {
		t.Fatalf("lookup=%+v err=%v", got, err)
	}
	processes, err := backend.(terminal.ProcessInspector).ProcessFacts(t.Context(), conn, address.Pane)
	if err != nil || len(processes) != 1 || processes[0].PID <= 0 || processes[0].Name == "" {
		t.Fatalf("foreground=%+v err=%v", processes, err)
	}
	if err := backend.DeliverText(t.Context(), conn, address.Pane, "kander-smoke-message"); err != nil {
		t.Fatal(err)
	}
	output, err := backend.ReadOutput(t.Context(), conn, address.Pane)
	if err != nil || !strings.Contains(output, "received:kander-smoke-message") {
		t.Fatalf("delivered output=%q err=%v", output, err)
	}
}
