package notify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/launch"
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

// useHerdrCoreRecovery configures herdr-core as the recovery launcher for an
// agent that declares it does not report its program status.
func useHerdrCoreRecovery(t *testing.T) {
	t.Helper()
	cfg, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Launcher = "herdr-core"
	cfg.Agents = map[string]config.AgentDefinition{"claude": {ProgramStatus: &config.ProgramStatus{Reports: false}}}
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
}

// A durable notify whose executor stopped would end in a recovery the
// configured launcher refuses; it is refused before the dispatch exists. A
// live executor still gets direct delivery.
func TestNotifyRefusesUnrunnableRecoveryBeforeDispatch(t *testing.T) {
	root, _ := setupBoard(t)
	useHerdrCoreRecovery(t)
	taskID, path := makeReview(t, root, "pstatus-notify")
	setWindow(t, path, "herdr:w1:t9:w1:p9")
	t.Setenv("KANBAN_HERDR_STALE_PANE", "w1:p9")
	t.Setenv("KANBAN_HERDR_LIST_JSON", `{"id":"cli:pane:list","result":{"type":"pane_list","panes":[]}}`)
	before, err := board.ReadSnapshot(root, taskID)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = capture(t, func() error {
		return commandNotify(root, taskID, "sync", "", "", true, 61, launch.DispatchOptions{Kind: "sync", Base: strings.Repeat("a", 40)})
	})
	if err == nil || err.Error() != config.Text("program_status.agent_not_reporting", "herdr-core", "claude") {
		t.Fatalf("err=%v", err)
	}
	after, err := board.ReadSnapshot(root, taskID)
	if err != nil || after.Revision != before.Revision || board.MetadataFrom(after.Text, "DISPATCH_ID") != "" {
		t.Fatalf("dispatch written before the refusal: %v\n%s", err, after.Text)
	}
	// An explicit pane override names the live executor: it is validated and
	// delivered to without observing the stopped recorded address.
	if _, _, err = capture(t, func() error { return commandNotify(root, taskID, "x", "", "w1:p8", true, 61) }); err != nil {
		t.Fatalf("pane override not delivered to: %v", err)
	}
	// The fake herdr reports an agentless pane once a prompt was delivered.
	if err := os.Remove(filepath.Join(root, "herdr.log.prompt")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANBAN_HERDR_STALE_PANE", "")
	if _, _, err = capture(t, func() error { return commandNotify(root, taskID, "x", "", "", true, 61) }); err != nil {
		t.Fatalf("live executor not delivered to: %v", err)
	}
}
