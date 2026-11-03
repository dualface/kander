package notify

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/liveness"
	"github.com/dualface/kander/internal/terminal"
	"github.com/dualface/kander/internal/terminal/builtin"
)

func writeFakeHCStatus(t *testing.T, fakeBin, status string) string {
	t.Helper()
	program := filepath.Join(fakeBin, "hc")
	body := `{"address":"t8","labels":{"kander-session":"s1"},"state":"live","status":` + status + `}`
	if err := os.WriteFile(program, []byte("#!/bin/sh\nprintf '%s\\n' '"+body+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return program
}

// Nothing is delivered to a herdr-core terminal before it reports its program
// status: an unreported terminal is busy (retried), another app is stale, and
// the declared app in an accepting state is ready.
func TestHerdrCoreNotifyProbeWaitsForProgramStatus(t *testing.T) {
	_, fakeBin := setupBoard(t)
	backend, ok := terminal.Lookup(builtin.HerdrCore)
	if !ok {
		t.Fatal("herdr-core is not registered")
	}
	session := liveness.TaskSession{Agent: "claude", Reference: "s1"}
	for status, want := range map[string]string{
		"null":                                  "busy",
		`{"app":"codex","state":"idle"}`:        "stale",
		`{"app":"claude-code","state":"idle"}`:  "ready",
		`{"app":"claude-code","state":"error"}`: "busy",
	} {
		program := writeFakeHCStatus(t, fakeBin, status)
		if got := AgentNotifyProbe(backend, program, "t8", session, 0); got.State != want {
			t.Fatalf("status %s: probe=%+v, want %s", status, got, want)
		}
	}
}

// A --pane override names a pane of the card's own agent-aware terminal; a
// card without such a WINDOW keeps the herdr fallback.
func TestOverrideBackendPrefersTheWindowBackend(t *testing.T) {
	core, _, _ := terminal.ParseWindow("herdr-core:t8")
	if got, ok := overrideBackend(core, true); !ok || got.Name() != builtin.HerdrCore {
		t.Fatalf("herdr-core window: %v %v", got, ok)
	}
	tmux, _, _ := terminal.ParseWindow("tmux:$1:@1:%1")
	for _, tc := range []struct {
		backend terminal.Backend
		parsed  bool
	}{{tmux, true}, {nil, false}} {
		if got, ok := overrideBackend(tc.backend, tc.parsed); !ok || got.Name() != builtin.Herdr {
			t.Fatalf("fallback: %v %v", got, ok)
		}
	}
}
