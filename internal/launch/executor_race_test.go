package launch

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/liveness"
	"github.com/dualface/kander/internal/process"
)

// A pane-delivery executor runs before WINDOW is written. Its card write must not
// fail the start (DE-16), and the entrance must report it. The agents come from
// their definitions: each one that delivers through a pane runs this case.
func TestPaneDeliveryExecutorWriteKeepsTheStart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	covered := map[string]bool{}
	root, _, _ := setupBoard(t)
	cfg, err := loadEffective()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range config.AgentNames(cfg) {
		definition := config.AgentFor(cfg, name)
		if definition.PromptDelivery == nil || definition.PromptDelivery.Mode != "pane" {
			continue
		}
		covered[name] = true
		t.Run(name, func(t *testing.T) {
			t.Setenv("KANBAN_TMUX_PANE_OUTPUT", definition.PromptDelivery.Ready.Match)
			taskID, _ := makeTodo(t, root, "pane-"+name)
			stub, ok := paneDiscoveryStubs[name]
			if !ok {
				t.Fatalf("%s delivers through a pane and has no discovery stub", name)
			}
			sessionAppears := stub(t, root)
			wrote, advanced := false, false
			previous := recordWindowFn
			t.Cleanup(func() { recordWindowFn = previous })
			recordWindowFn = func(root string, entry board.Entry, window, resolved string) (board.ExecutionResult, error) {
				if !wrote {
					wrote = true
					sessionAppears()
					executorWrite(t, root, taskID)
				}
				result, err := previous(root, entry, window, resolved)
				advanced = result.Advanced
				return result, err
			}
			_, _, err := capture(t, func() error { return commandStart(root, name, "tmux", taskID) })
			if err != nil {
				t.Fatal(err)
			}
			if !wrote || !advanced {
				t.Fatalf("wrote=%v advanced=%v", wrote, advanced)
			}
			s, err := board.ReadSnapshot(root, taskID)
			if err != nil {
				t.Fatal(err)
			}
			if s.Entry.State != "working" || !strings.Contains(s.Text, "executor progress") || board.MetadataFrom(s.Text, board.FieldWindow) == "" {
				t.Fatalf("state=%s window=%q", s.Entry.State, board.MetadataFrom(s.Text, board.FieldWindow))
			}
		})
	}
	for _, name := range []string{"kimi", "droid"} {
		if !covered[name] {
			t.Errorf("%s no longer delivers through a pane: review DE-16 for it", name)
		}
	}
}

// A takeover's session write-back goes through the entrance, so a new executor
// that wrote first does not cost the takeover its session (DE-17).
func TestTakeoverDiscoverySurvivesExecutorWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	taskID, path := makeTodo(t, root, "takeover-race")
	startThenReview(t, root, "claude", taskID, path)
	stubDiscovery(t, func(call int) []string {
		if call == 1 {
			return []string{"existing"}
		}
		if call == 2 {
			executorWrite(t, root, taskID)
		}
		return []string{"existing", "devin-new-session"}
	})
	agent := "devin"
	if _, _, err := capture(t, func() error { return commandResume(root, &agent, "tmux", taskID, "take over", "", true, 61) }); err != nil {
		t.Fatal(err)
	}
	s, err := board.ReadSnapshot(root, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if got := board.MetadataFrom(s.Text, board.FieldSession); !strings.Contains(got, "devin-new-session") || !strings.Contains(s.Text, "executor progress") {
		t.Fatalf("session=%q", got)
	}
	record, _, err := board.CurrentExecution(root, taskID)
	if err != nil || record.Kind != board.ExecutionTakeover || record.Status != board.ExecutionRunning {
		t.Fatalf("record=%+v err=%v", record, err)
	}
}

// DE-02: a fast executor's write no longer fails the session confirmation.
func TestStartConfirmsSessionOverExecutorWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	taskID, _ := makeTodo(t, root, "confirm-race")
	stubDiscovery(t, func(call int) []string {
		if call == 1 {
			return []string{"existing"}
		}
		if call == 2 {
			executorWrite(t, root, taskID)
		}
		return []string{"existing", "devin-new-session"}
	})
	if _, _, err := capture(t, func() error { return commandStart(root, "devin", "tmux", taskID) }); err != nil {
		t.Fatal(err)
	}
	record, _, err := board.CurrentExecution(root, taskID)
	if err != nil || record.Status != board.ExecutionRunning || !strings.Contains(record.Session, "devin-new-session") {
		t.Fatalf("record=%+v err=%v", record, err)
	}
	if _, err := os.Stat(filepath.Join(root, "tmux.log.kill")); err == nil {
		t.Fatal("the confirmed launch closed its container")
	}
}

// DE-02 reaches every agent whose session a launcher writes back after start. The
// list comes from the definitions, so a new agent cannot miss the fix.
func TestSessionConfirmationAppliesBySessionDefinition(t *testing.T) {
	setupBoard(t)
	cfg, err := loadEffective()
	if err != nil {
		t.Fatal(err)
	}
	applies := map[string]bool{}
	for _, name := range config.AgentNames(cfg) {
		if config.SessionPersistsAfterStart(config.AgentFor(cfg, name).Session) {
			applies[name] = true
		}
	}
	for _, name := range []string{"devin", "droid", "kimi", "opencode"} {
		if !applies[name] {
			t.Errorf("%s no longer writes its session back after start: review DE-02 for it", name)
		}
	}
}

// executorWrite stands in for the executor publishing progress on the card.
func executorWrite(t *testing.T, root, taskID string) {
	t.Helper()
	s, err := board.ReadSnapshot(root, taskID)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.TrimRight(s.Text, "\n") + "\n\n- executor progress\n"
	if err := board.UpdateDocument(root, taskID, board.UpdateOptions{Document: "spec.md", Text: text, ExpectedRevision: s.Revision}); err != nil {
		t.Fatalf("executor write: %v", err)
	}
}

// stubDiscovery lets poll run on each session enumeration of a launch.
func stubDiscovery(t *testing.T, poll func(call int) []string) {
	t.Helper()
	calls := 0
	previous := enumerateSessionsFn
	enumerateSessionsFn = func(context.Context, *config.SessionDiscovery, *process.AgentProgram, string, string) ([]string, error) {
		calls++
		return poll(calls), nil
	}
	t.Cleanup(func() { enumerateSessionsFn = previous })
}

// paneDiscoveryStubs prepares the session discovery of each pane agent. The
// function it returns runs once the prompt is delivered.
var paneDiscoveryStubs = map[string]func(t *testing.T, root string) func(){
	"kimi": func(t *testing.T, _ string) func() {
		stubKimiDiscovery(t, "kimi-race")
		return func() {}
	},
	"droid": func(t *testing.T, root string) func() {
		sessions := droidHome(t)
		return func() {
			writeDroidSession(t, sessions, "race", "droid-race", droidHeader("droid-race", filepath.Dir(root)))
		}
	},
}

// A self-claimed card has no session. Resume refuses it before any dispatch binds,
// and a takeover still replaces the claiming session.
func TestSelfClaimedCardRefusesResumeBeforeBinding(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	taskID, _ := makeTodo(t, root, "self-claimed")
	s, err := board.ReadSnapshot(root, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = board.MoveWithOptions(s.Entry, root, "working", board.MoveOptions{Owner: "claude"}); err != nil {
		t.Fatal(err)
	}
	_, _, err = capture(t, func() error {
		return commandResume(root, nil, "tmux", taskID, "continue", "", true, 61, DispatchOptions{Kind: "sync"})
	})
	if err == nil || !strings.Contains(err.Error(), "failure-recovery.md") {
		t.Fatalf("err=%v", err)
	}
	after, err := board.ReadSnapshot(root, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if board.MetadataFrom(after.Text, "DISPATCH_ID") != "" || board.MetadataFrom(after.Text, board.FieldSession) != "" {
		t.Fatal("the refused resume bound a dispatch or wrote a session")
	}
	agent := "claude"
	if _, _, err = capture(t, func() error { return commandResume(root, &agent, "tmux", taskID, "take over", "", true, 61) }); err != nil {
		t.Fatal(err)
	}
}

// start, reclaim and resume --agent work in a row; the reclaim blocks a plain resume.
func TestReclaimThenTakeover(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	taskID, path := makeTodo(t, root, "reclaim-takeover")
	startThenReview(t, root, "claude", taskID, path)
	s, err := board.ReadSnapshot(root, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = board.MoveWithOptions(s.Entry, root, "working", board.MoveOptions{Owner: "claude", Decision: "user"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = capture(t, func() error { return commandResume(root, nil, "tmux", taskID, "continue", "", true, 61) }); err == nil {
		t.Fatal("a reclaimed card accepted a plain resume")
	}
	agent := "claude"
	if _, _, err = capture(t, func() error { return commandResume(root, &agent, "tmux", taskID, "take over", "", true, 61) }); err != nil {
		t.Fatal(err)
	}
	record, _, err := board.CurrentExecution(root, taskID)
	if err != nil || record.Kind != board.ExecutionTakeover || record.Status != board.ExecutionRunning {
		t.Fatalf("record=%+v err=%v", record, err)
	}
}

// A takeover that lands after commandAdopt read the card must not be adopted on
// the strength of the old executor's liveness.
func TestAdoptRefusesARecordNewerThanItsSnapshot(t *testing.T) {
	root, _, _ := setupBoard(t)
	taskID, _ := makeTodo(t, root, "adopt-race")
	s, err := board.ReadSnapshot(root, taskID)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := board.BeginTaskStart(s.Entry, root, board.TaskStartInput{Owner: "claude", Session: "claude old", Window: "foreground"})
	if err != nil {
		t.Fatal(err)
	}
	previous := loadEffective
	t.Cleanup(func() { loadEffective = previous })
	loadEffective = func() (*config.Config, error) {
		if _, err := board.BeginTakeover(root, moved, board.TakeoverInput{Owner: "claude", Session: "claude new", Decision: "user"}); err != nil {
			t.Fatal(err)
		}
		return previous()
	}
	adoptLiveness := adoptLivenessFn
	t.Cleanup(func() { adoptLivenessFn = adoptLiveness })
	adoptLivenessFn = func(board.Snapshot) liveness.Report {
		t.Fatal("the probe ran for a record the snapshot did not read")
		return liveness.Report{}
	}
	if err = commandAdopt(root, taskID, "user"); err == nil {
		t.Fatal("the adoption bound the successor record")
	}
	record, _, err := board.CurrentExecution(root, taskID)
	if err != nil || record.Status != board.ExecutionPending || record.Kind != board.ExecutionTakeover {
		t.Fatalf("record=%+v err=%v", record, err)
	}
}
