package launch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/liveness"
	"github.com/dualface/kander/internal/process"
)

// paneMarker reads the session marker the fake tmux holds for the pane.
func paneMarker(t *testing.T, root string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "tmux.log.pane-session"))
	if errors.Is(err, os.ErrNotExist) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data)), true
}

// markerWrites counts the pane option writes the fake tmux received.
func markerWrites(t *testing.T, root string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "tmux.log.order"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return strings.Count(string(data), "set-option\n")
}

// forbidSessionScan fails the test when adoption looks for a session.
func forbidSessionScan(t *testing.T) {
	t.Helper()
	previous := findAdoptSessionsFn
	t.Cleanup(func() { findAdoptSessionsFn = previous })
	findAdoptSessionsFn = func(string, time.Time, string) ([]string, error) {
		t.Error("adoption scanned for a session")
		return nil, errors.New("unexpected session scan")
	}
}

// unmarkedStart starts agent on tmux and fails the marker write after the
// executor wrote progress, so the launch ends unresolved with an unmarked pane.
// during runs while the attempt is still pending and the pane exists.
func unmarkedStart(t *testing.T, root, agent, task string, during func()) *UnresolvedError {
	t.Helper()
	t.Setenv("KANBAN_TMUX_PANE_SETOPT_FAIL", "1")
	previous := readStandingFn
	readStandingFn = func(string, board.Entry) (board.ExecutionStanding, error) {
		if during != nil {
			during()
		}
		return board.ExecutionStanding{Evidence: true}, nil
	}
	_, _, err := capture(t, func() error { return commandStart(root, agent, "tmux", task) })
	readStandingFn = previous
	t.Setenv("KANBAN_TMUX_PANE_SETOPT_FAIL", "")
	unresolved := requireUnresolved(t, err)
	if !unresolved.Unmarked {
		t.Fatalf("unresolved=%+v", unresolved)
	}
	if _, ok := paneMarker(t, root); ok {
		t.Fatal("the failed launch left a marker")
	}
	return unresolved
}

// rolloutPath is the rollout the fake tmux writes for a Codex launch of task.
func rolloutPath(task string) string {
	return filepath.Join(codexSessionsRoot(), time.Now().UTC().Format("2006/01/02"), "rollout-"+task+".jsonl")
}

// writeRollout writes a Codex rollout of task with the given session, launch
// directory and creation time.
func writeRollout(t *testing.T, task, id, cwd string, created time.Time) {
	t.Helper()
	dir := filepath.Join(codexSessionsRoot(), created.UTC().Format("2006/01/02"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	prompt := codexPromptPrefixes(task)[0] + " full instructions are in the task file."
	text := fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q,"cwd":%q,"timestamp":%q}}
{"type":"event_msg","payload":{"type":"user_message","message":%q}}
`, id, cwd, created.UTC().Format("2006-01-02T15:04:05.000Z"), prompt)
	if err := os.WriteFile(filepath.Join(dir, "rollout-"+id+".jsonl"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// requireAdoptRefused checks that a refused adoption wrote no marker and changed
// neither the card nor its start attempt.
func requireAdoptRefused(t *testing.T, root, task string, before board.Snapshot, record board.ExecutionRecord) {
	t.Helper()
	if marker, ok := paneMarker(t, root); ok {
		t.Fatalf("a refused adoption wrote marker %q", marker)
	}
	after := readSnapshot(t, root, task)
	if after.Text != before.Text || after.Revision != before.Revision {
		t.Fatal("a refused adoption changed the card")
	}
	if current := requireRecord(t, root, task, board.ExecutionUnresolved); current != record {
		t.Fatalf("record=%+v, want %+v", current, record)
	}
}

// requireStopFirst checks a refusal that keeps the hint of v1.1.1.
func requireStopFirst(t *testing.T, task, window string, err error) {
	t.Helper()
	requireTakeoverOnly(t, task, err, config.Text("launch.unmarked_executor_stop_first", window))
}

// adoptMarked runs an adoption that must write want as the marker and leave the
// card byte for byte unchanged.
func adoptMarked(t *testing.T, root, task, want string) {
	t.Helper()
	before := readSnapshot(t, root, task)
	out, _, err := capture(t, func() error { return commandAdopt(root, task, "user") })
	if err != nil || !strings.Contains(out, "adopted: "+task) || !strings.Contains(out, "session-marker-written: "+task) {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if marker, _ := paneMarker(t, root); marker != want {
		t.Fatalf("marker=%q, want %q", marker, want)
	}
	requireRecord(t, root, task, board.ExecutionRunning)
	if after := readSnapshot(t, root, task); after.Text != before.Text || after.Revision != before.Revision {
		t.Fatal("adoption changed the card")
	}
}

func TestAdoptMarksAKnownSession(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "adopt-known")
	forbidSessionScan(t)
	unmarkedStart(t, root, "claude", task, nil)
	session := parseTaskSession(readSnapshot(t, root, task).Text)
	if session == nil || session.Reference == "" {
		t.Fatalf("session=%+v", session)
	}
	adoptMarked(t, root, task, session.Reference)
}

func TestAdoptMarksTheOnlyCodexSessionOfTheAttempt(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "adopt-codex")
	unmarkedStart(t, root, "codex", task, nil)
	if got := board.MetadataFrom(readSnapshot(t, root, task).Text, board.FieldSession); got != "codex" {
		t.Fatalf("SESSION=%q", got)
	}
	adoptMarked(t, root, task, "fake-codex-session")
}

// A rollout of the same task that is older than the attempt belongs to an
// earlier launch and is never a candidate.
func TestAdoptIgnoresCodexSessionsBeforeTheAttempt(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "adopt-codex-old")
	unresolved := unmarkedStart(t, root, "codex", task, nil)
	record := requireRecord(t, root, task, board.ExecutionUnresolved)
	floor, err := board.ExecutionStartFloor(root, record)
	if err != nil {
		t.Fatal(err)
	}
	writeRollout(t, task, "earlier-codex-session", filepath.Dir(root), floor.Add(-time.Second))
	current, err := os.ReadFile(rolloutPath(task))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(rolloutPath(task)); err != nil {
		t.Fatal(err)
	}
	before := readSnapshot(t, root, task)
	_, _, err = capture(t, func() error { return commandAdopt(root, task, "user") })
	requireStopFirst(t, task, unresolved.Window, err)
	if !strings.Contains(err.Error(), config.Text("launch.adopt_session_unproven", task, "0", config.Text("launch.unmarked_executor_stop_first", unresolved.Window))) {
		t.Fatalf("err=%v", err)
	}
	requireAdoptRefused(t, root, task, before, record)
	if err = os.WriteFile(rolloutPath(task), current, 0o600); err != nil {
		t.Fatal(err)
	}
	adoptMarked(t, root, task, "fake-codex-session")
}

func TestAdoptRefusesTwoCodexSessions(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "adopt-codex-two")
	unresolved := unmarkedStart(t, root, "codex", task, nil)
	writeRollout(t, task, "second-codex-session", filepath.Dir(root), time.Now())
	record := requireRecord(t, root, task, board.ExecutionUnresolved)
	before := readSnapshot(t, root, task)
	_, _, err := capture(t, func() error { return commandAdopt(root, task, "user") })
	requireStopFirst(t, task, unresolved.Window, err)
	if !strings.Contains(err.Error(), config.Text("launch.adopt_session_unproven", task, "2", config.Text("launch.unmarked_executor_stop_first", unresolved.Window))) {
		t.Fatalf("err=%v", err)
	}
	requireAdoptRefused(t, root, task, before, record)
}

func TestAdoptRefusesCodexSessionsOfAnotherTaskOrDirectory(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "adopt-codex-none")
	other, _ := makeTodo(t, root, "adopt-codex-other")
	unresolved := unmarkedStart(t, root, "codex", task, nil)
	record := requireRecord(t, root, task, board.ExecutionUnresolved)
	if err := os.Remove(rolloutPath(task)); err != nil {
		t.Fatal(err)
	}
	for i, plant := range []func(){
		func() {},
		func() { writeRollout(t, other, "other-task-session", filepath.Dir(root), time.Now()) },
		func() { writeRollout(t, task, "other-directory-session", t.TempDir(), time.Now()) },
	} {
		plant()
		before := readSnapshot(t, root, task)
		_, _, err := capture(t, func() error { return commandAdopt(root, task, "user") })
		requireStopFirst(t, task, unresolved.Window, err)
		if !strings.Contains(err.Error(), config.Text("launch.adopt_session_unproven", task, "0", config.Text("launch.unmarked_executor_stop_first", unresolved.Window))) {
			t.Fatalf("case %d: err=%v", i, err)
		}
		requireAdoptRefused(t, root, task, before, record)
	}
}

// Agents whose sessions cannot be tied to one launch keep the refusal of
// v1.1.1, and adoption runs no discovery for them.
func TestAdoptRefusesAgentsWithoutProvableSessions(t *testing.T) {
	skipOnWindows(t)
	for _, agent := range []string{"droid", "devin"} {
		t.Run(agent, func(t *testing.T) {
			root, _, _ := setupBoard(t)
			task, _ := makeTodo(t, root, "adopt-"+agent)
			forbidSessionScan(t)
			previous := enumerateSessionsFn
			t.Cleanup(func() { enumerateSessionsFn = previous })
			enumerateSessionsFn = func(context.Context, *config.SessionDiscovery, *process.AgentProgram, string, string) ([]string, error) {
				t.Error("adoption enumerated sessions")
				return nil, errors.New("unexpected enumeration")
			}
			window := "tmux:$42:@1:%1"
			s := readSnapshot(t, root, task)
			if _, err := board.BeginTaskStart(s.Entry, root, board.TaskStartInput{Owner: agent, Session: agent, Window: window}); err != nil {
				t.Fatal(err)
			}
			definition, err := config.LoadAgent(agent)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("KANBAN_TMUX_CURRENT_COMMAND", definition.ProcessName)
			record := requireRecord(t, root, task, board.ExecutionPending)
			before := readSnapshot(t, root, task)
			_, _, err = capture(t, func() error { return commandAdopt(root, task, "user") })
			requireStopFirst(t, task, window, err)
			if !strings.Contains(err.Error(), config.Text("launch.adopt_unmarked_pane", task, config.Text("launch.unmarked_executor_stop_first", window))) {
				t.Fatalf("err=%v", err)
			}
			if _, ok := paneMarker(t, root); ok || markerWrites(t, root) != 0 {
				t.Fatal("adoption wrote a marker")
			}
			if after := readSnapshot(t, root, task); after.Text != before.Text || requireRecord(t, root, task, board.ExecutionPending) != record {
				t.Fatal("a refused adoption changed the card or the attempt")
			}
		})
	}
}

func TestAdoptRefusesAPaneThatRunsAnotherCommand(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "adopt-other-command")
	unmarkedStart(t, root, "claude", task, nil)
	t.Setenv("KANBAN_TMUX_CURRENT_COMMAND", "bash")
	record := requireRecord(t, root, task, board.ExecutionUnresolved)
	before := readSnapshot(t, root, task)
	_, _, err := capture(t, func() error { return commandAdopt(root, task, "user") })
	if err == nil || !strings.Contains(err.Error(), liveness.Stopped) {
		t.Fatalf("err=%v", err)
	}
	requireAdoptRefused(t, root, task, before, record)
}

// A takeover between the probe and the marker write ends the attempt, so the
// marker is not written.
func TestAdoptRefusesATakeoverBeforeTheMarker(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "adopt-takeover-race")
	unmarkedStart(t, root, "claude", task, nil)
	previous := adoptLivenessFn
	t.Cleanup(func() { adoptLivenessFn = previous })
	adoptLivenessFn = func(s board.Snapshot) liveness.Report {
		report := previous(s)
		adoptLivenessFn = previous
		takeoverFixture(t, root, task)
		return report
	}
	_, _, err := capture(t, func() error { return commandAdopt(root, task, "user") })
	if err == nil || err.Error() != launchError("launch.adopt_changed_before_marker", task).Error() {
		t.Fatalf("err=%v", err)
	}
	if _, ok := paneMarker(t, root); ok {
		t.Fatal("adoption marked the pane of a taken over attempt")
	}
	requireRecord(t, root, task, board.ExecutionPending)
}

func TestAdoptRefusesAFailedMarkerWrite(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "adopt-marker-fails")
	unresolved := unmarkedStart(t, root, "claude", task, nil)
	t.Setenv("KANBAN_TMUX_PANE_SETOPT_FAIL", "1")
	record := requireRecord(t, root, task, board.ExecutionUnresolved)
	before := readSnapshot(t, root, task)
	_, _, err := capture(t, func() error { return commandAdopt(root, task, "user") })
	if err == nil || !strings.Contains(err.Error(), "fake pane setopt failure") || !strings.Contains(err.Error(), config.Text("launch.unmarked_executor_stop_first", unresolved.Window)) {
		t.Fatalf("err=%v", err)
	}
	requireAdoptRefused(t, root, task, before, record)
}

// A marker written before a failed ledger write leaves the attempt unfinished:
// resume and notify still refuse, and adoption runs again.
func TestAdoptRetriesAfterTheLedgerWriteFails(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "adopt-ledger-fails")
	unmarkedStart(t, root, "claude", task, nil)
	session := parseTaskSession(readSnapshot(t, root, task).Text)
	previous := adoptExecutionFn
	t.Cleanup(func() { adoptExecutionFn = previous })
	adoptExecutionFn = func(string, board.Snapshot, string) (board.ExecutionResult, error) {
		return board.ExecutionResult{}, errors.New("ledger write failed")
	}
	if _, _, err := capture(t, func() error { return commandAdopt(root, task, "user") }); err == nil || err.Error() != "ledger write failed" {
		t.Fatalf("err=%v", err)
	}
	if marker, _ := paneMarker(t, root); marker != session.Reference {
		t.Fatalf("marker=%q", marker)
	}
	requireRecord(t, root, task, board.ExecutionUnresolved)
	unfinished := launchError("board.dispatch_start_unfinished", task, board.ExecutionUnresolved).Error()
	if err := RequireResumable(root, readSnapshot(t, root, task).Entry); err == nil || err.Error() != unfinished {
		t.Fatalf("resume and notify: err=%v", err)
	}
	if _, _, err := capture(t, func() error { return commandResume(root, nil, "", task, "go", "", true, 61) }); err == nil || err.Error() != unfinished {
		t.Fatalf("resume: err=%v", err)
	}
	adoptExecutionFn = previous
	forbidSessionScan(t)
	writes := markerWrites(t, root)
	out, _, err := capture(t, func() error { return commandAdopt(root, task, "user") })
	if err != nil || !strings.Contains(out, "adopted: "+task) {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if markerWrites(t, root) != writes {
		t.Fatal("the retry wrote the marker again")
	}
	requireRecord(t, root, task, board.ExecutionRunning)
}

// Every recovery hint lists adoption for an unmarked pane that adoption can
// mark, and keeps the hint of v1.1.1 for every other agent. No hint runs session
// discovery or writes a marker.
func TestUnmarkedHintsFollowStaticRepairability(t *testing.T) {
	skipOnWindows(t)
	for _, tc := range []struct {
		agent string
		adopt bool
	}{{"claude", true}, {"codex", true}, {"devin", false}} {
		t.Run(tc.agent, func(t *testing.T) {
			root, _, _ := setupBoard(t)
			task, _ := makeTodo(t, root, "hints-"+tc.agent)
			forbidSessionScan(t)
			if tc.agent == "devin" {
				stubDiscovery(t, func(call int) []string {
					if call == 1 {
						return []string{"existing"}
					}
					return []string{"existing", "devin-new"}
				})
			}
			check := func(err error) {
				t.Helper()
				if tc.adopt {
					if err == nil || !strings.Contains(err.Error(), "resume "+task+" --adopt") {
						t.Fatalf("err=%v", err)
					}
					return
				}
				window := board.MetadataFrom(readSnapshot(t, root, task).Text, board.FieldWindow)
				requireTakeoverOnly(t, task, err, config.Text("launch.unmarked_pane_cannot_adopt", window))
			}
			hints := func() {
				writes := markerWrites(t, root)
				s := readSnapshot(t, root, task)
				check(RequireResumable(root, s.Entry))
				_, err := RepairMissingSession(context.Background(), root, s, "")
				check(err)
				if markerWrites(t, root) != writes {
					t.Fatal("a hint wrote a marker")
				}
			}
			unresolved := unmarkedStart(t, root, tc.agent, task, func() {
				requireRecord(t, root, task, board.ExecutionPending)
				hints()
				_, err := RecoverCoordinator(context.Background(), root, board.CoordinatorRecover{Member: task})
				check(err)
			})
			if unresolved.Repairable != tc.adopt || unresolved.adoptBlocked() == tc.adopt {
				t.Fatalf("unresolved=%+v", unresolved)
			}
			steps := unresolved.NextSteps()
			if got := strings.Contains(steps[0], "resume "+task+" --adopt --decision <reference>"); got != tc.adopt {
				t.Fatalf("steps=%q", steps)
			}
			hints()
		})
	}
}

// A marked tmux pane and a herdr pane are adopted as before: no marker is
// written and no session is looked up.
func TestAdoptOfProvablePanesIsUnchanged(t *testing.T) {
	skipOnWindows(t)
	for _, launcher := range []string{"tmux", "herdr"} {
		t.Run(launcher, func(t *testing.T) {
			root, _, bin := setupBoard(t)
			task, _ := makeTodo(t, root, "adopt-provable-"+launcher)
			if launcher == "herdr" {
				installHerdr(t, root, bin)
			}
			forbidSessionScan(t)
			previous := confirmStartFn
			t.Cleanup(func() { confirmStartFn = previous })
			confirmStartFn = func(string, board.Entry) error {
				executorWrite(t, root, task)
				return errors.New("confirm failed")
			}
			_, _, err := capture(t, func() error { return commandStart(root, "claude", launcher, task) })
			unresolved := requireUnresolved(t, err)
			if unresolved.Unmarked || unresolved.Repairable {
				t.Fatalf("unresolved=%+v", unresolved)
			}
			if steps := unresolved.NextSteps(); !strings.Contains(steps[0], "resume "+task+" --adopt --decision <reference>") {
				t.Fatalf("steps=%q", steps)
			}
			writes := markerWrites(t, root)
			before := readSnapshot(t, root, task)
			out, _, err := capture(t, func() error { return commandAdopt(root, task, "user") })
			if err != nil || !strings.Contains(out, "adopted: "+task) || strings.Contains(out, "session-marker-written") {
				t.Fatalf("out=%q err=%v", out, err)
			}
			if markerWrites(t, root) != writes {
				t.Fatal("adoption wrote a marker")
			}
			if after := readSnapshot(t, root, task); after.Text != before.Text {
				t.Fatal("adoption changed the card")
			}
			requireRecord(t, root, task, board.ExecutionRunning)
		})
	}
}
