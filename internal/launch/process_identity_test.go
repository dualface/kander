package launch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/liveness"
	"github.com/dualface/kander/internal/process"
)

func requireIdentity(t *testing.T, root, task string) board.ExecutionProcess {
	t.Helper()
	record, _, err := board.CurrentExecution(root, task)
	if err != nil || record.Process == nil {
		t.Fatalf("record=%+v err=%v", record, err)
	}
	return *record.Process
}

func observeCard(t *testing.T, root, task string) liveness.Report {
	t.Helper()
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	return liveness.ClassifyTaskContext(context.Background(), s.Entry, s.Text)
}

// foregroundClaude installs a fake claude: it needs no session discovery.
func foregroundClaude(t *testing.T, bin, script string) {
	t.Helper()
	foregroundAgent(t, bin)
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// startForeground launches a shim: the configured claude execs sleep.
func startForeground(t *testing.T, root, bin, slug string) (string, StartResult) {
	t.Helper()
	foregroundClaude(t, bin, "#!/bin/sh\nexec sleep 30\n")
	task, _ := makeTodo(t, root, slug)
	// capture would wait for the pipe the live agent inherits.
	result, err := Start(root, "claude", "foreground", task)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = result.Outcome.Process.Kill(); _, _ = result.Outcome.Wait() })
	return task, result
}

// The configured agent is a shim. The identity holds what the kernel reports, so
// the healthy process passes the check; the agent name appears nowhere in it.
func TestForegroundStartRecordsTheKernelIdentity(t *testing.T) {
	skipOnWindows(t)
	root, _, bin := setupBoard(t)
	task, result := startForeground(t, root, bin, "identity-start")
	identity := requireIdentity(t, root, task)
	self, err := process.ObserveProcess(context.Background(), os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if identity.Launcher != "foreground" || identity.AgentPID != result.Outcome.Process.Pid || identity.Executable != "sleep" || identity.AgentStart == "" || identity.BootID == "" || identity.KanderPID != self.PID || identity.KanderStart != self.Start || identity.RecordedAt == "" {
		t.Fatalf("identity=%+v", identity)
	}
	if got := observeCard(t, root, task); got.Status != liveness.Alive {
		t.Fatalf("report=%+v", got)
	}
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s.Text, identity.AgentStart) {
		t.Fatal("the process identity reached the card body")
	}
	_ = result.Outcome.Process.Kill()
	_, _ = result.Outcome.Wait()
	if got := observeCard(t, root, task); got.Status != liveness.Stopped {
		t.Fatalf("report=%+v", got)
	}
}

// A process that is gone before its identity can be read leaves no record, and the
// check names the reason.
func TestStartWithoutAProcessIdentityRecordsNothing(t *testing.T) {
	skipOnWindows(t)
	root, _, bin := setupBoard(t)
	foregroundClaude(t, bin, "#!/bin/sh\nexit 0\n")
	task, _ := makeTodo(t, root, "identity-gone")
	oldObserve := observeSettledFn
	observeSettledFn = func(context.Context, int) (process.ProcessFacts, error) {
		return process.ProcessFacts{}, process.ErrProcessGone
	}
	t.Cleanup(func() { observeSettledFn = oldObserve })
	result, err := Start(root, "claude", "foreground", task)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = result.Outcome.Wait()
	record, _, err := board.CurrentExecution(root, task)
	if err != nil || record.Process != nil {
		t.Fatalf("record=%+v err=%v", record, err)
	}
	matches, err := filepath.Glob(filepath.Join(root, ".kander", "groups", "*", task, record.ID, "process.json"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("process.json=%v err=%v", matches, err)
	}
	if len(result.Warnings) == 0 || !strings.Contains(strings.Join(result.Warnings, ""), task) {
		t.Fatalf("warnings=%v", result.Warnings)
	}
	if got := observeCard(t, root, task); got.Status != liveness.Unknown || got.Detail == "" {
		t.Fatalf("report=%+v", got)
	}
}

// On a platform without process identity the launcher records the ID alone, so the
// check reaches its platform step and says unprobeable instead of unknown.
func TestUnsupportedPlatformRecordsThePIDAlone(t *testing.T) {
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "identity-unsupported")
	oldObserve := observeSettledFn
	observeSettledFn = func(context.Context, int) (process.ProcessFacts, error) {
		return process.ProcessFacts{}, process.ErrProcessUnsupported
	}
	t.Cleanup(func() { observeSettledFn = oldObserve })
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := board.BeginTaskStart(s.Entry, root, board.TaskStartInput{Owner: "devin", Session: "devin", Window: "foreground"})
	if err != nil {
		t.Fatal(err)
	}
	processRecorder(root, moved, LaunchPlan{Launcher: "console"})(1234)
	identity := requireIdentity(t, root, task)
	if identity != (board.ExecutionProcess{Launcher: "console", AgentPID: 1234, RecordedAt: identity.RecordedAt}) {
		t.Fatalf("identity=%+v", identity)
	}
}

// console has no waiting Kander, so the starter fields stay empty.
func TestConsoleIdentityHasNoStarter(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "identity-console")
	sleeper := exec.Command("sleep", "30")
	if err := sleeper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sleeper.Process.Kill(); _ = sleeper.Wait() })
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := board.BeginTaskStart(s.Entry, root, board.TaskStartInput{Owner: "devin", Session: "devin", Window: "console"})
	if err != nil {
		t.Fatal(err)
	}
	processRecorder(root, moved, LaunchPlan{Launcher: "console"})(sleeper.Process.Pid)
	identity := requireIdentity(t, root, task)
	if identity.Launcher != "console" || identity.KanderPID != 0 || identity.KanderStart != "" || identity.Executable != "sleep" {
		t.Fatalf("identity=%+v", identity)
	}
	if got := observeCard(t, root, task); got.Status != liveness.Alive {
		t.Fatalf("report=%+v", got)
	}
}

func TestAdoptAcceptsOnlyAProvenExecutor(t *testing.T) {
	for status, accepted := range map[string]bool{
		liveness.Alive:            true,
		liveness.AliveOrphaned:    true,
		liveness.Stopped:          false,
		liveness.IdentityMismatch: false,
		liveness.Unprobeable:      false,
		liveness.Unknown:          false,
	} {
		t.Run(status, func(t *testing.T) {
			root, _, _ := setupBoard(t)
			task, _ := makeTodo(t, root, "adopt-status")
			s, err := board.ReadSnapshot(root, task)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = board.BeginTaskStart(s.Entry, root, board.TaskStartInput{Owner: "devin", Session: "devin", Window: "foreground"}); err != nil {
				t.Fatal(err)
			}
			oldLiveness := adoptLivenessFn
			adoptLivenessFn = func(board.Snapshot) liveness.Report { return liveness.Report{Status: status} }
			t.Cleanup(func() { adoptLivenessFn = oldLiveness })
			_, _, err = capture(t, func() error { return commandAdopt(root, task, "user") })
			if accepted != (err == nil) {
				t.Fatalf("err=%v", err)
			}
			want := board.ExecutionPending
			if accepted {
				want = board.ExecutionRunning
			}
			requireRecord(t, root, task, want)
		})
	}
}

// The real observation adopts a foreground executor whose launcher is gone and
// refuses it once it stopped.
func TestAdoptUsesTheProcessIdentity(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "adopt-identity")
	sleeper := exec.Command("sleep", "30")
	if err := sleeper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sleeper.Process.Kill(); _ = sleeper.Wait() })
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := board.BeginTaskStart(s.Entry, root, board.TaskStartInput{Owner: "devin", Session: "devin", Window: "foreground"})
	if err != nil {
		t.Fatal(err)
	}
	processRecorder(root, moved, LaunchPlan{Launcher: "foreground"})(sleeper.Process.Pid)
	_ = sleeper.Process.Kill()
	_ = sleeper.Wait()
	if _, _, err = capture(t, func() error { return commandAdopt(root, task, "user") }); err == nil || !strings.Contains(err.Error(), liveness.Stopped) {
		t.Fatalf("err=%v", err)
	}
	requireRecord(t, root, task, board.ExecutionPending)
}

// A starter the identity no longer matches is a starter that has gone: the killed
// launcher adoption exists for.
func TestAdoptAcceptsAnOrphanedForegroundExecutor(t *testing.T) {
	skipOnWindows(t)
	root, _, _ := setupBoard(t)
	task, _ := makeTodo(t, root, "adopt-orphan")
	sleeper := exec.Command("sleep", "30")
	if err := sleeper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sleeper.Process.Kill(); _ = sleeper.Wait() })
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := board.BeginTaskStart(s.Entry, root, board.TaskStartInput{Owner: "devin", Session: "devin", Window: "foreground"})
	if err != nil {
		t.Fatal(err)
	}
	identity, err := observeIdentity(context.Background(), LaunchPlan{Launcher: "foreground"}, sleeper.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	identity.KanderStart = "another-start"
	if _, err = board.RecordExecutionProcess(root, moved, identity); err != nil {
		t.Fatal(err)
	}
	if _, _, err = capture(t, func() error { return commandAdopt(root, task, "user") }); err != nil {
		t.Fatal(err)
	}
	requireRecord(t, root, task, board.ExecutionRunning)
}

func TestRefuseNativeResume(t *testing.T) {
	identity := &board.ExecutionProcess{Launcher: "foreground", AgentPID: 7}
	pane, direct := LaunchPlan{Launcher: "tmux-session"}, LaunchPlan{Launcher: "foreground"}
	for _, c := range []struct {
		name    string
		window  string
		process *board.ExecutionProcess
		plan    LaunchPlan
		refused bool
	}{
		{"foreground card", "foreground", identity, pane, true},
		{"console card", "console", identity, direct, true},
		{"card moved to a pane, relaunch without one", "tmux-session:$1:@1:%1", identity, direct, true},
		{"card moved to a pane, relaunch in a pane", "tmux-session:$1:@1:%1", identity, pane, false},
		{"foreground card without identity", "foreground", nil, direct, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			text := "- WINDOW: " + c.window + "\n"
			err := refuseNativeResume("task", text, board.ExecutionRecord{Process: c.process}, c.plan)
			if c.refused != (err != nil) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestForegroundHasNoNativeResume(t *testing.T) {
	skipOnWindows(t)
	root, _, bin := setupBoard(t)
	task, _ := startForeground(t, root, bin, "identity-no-resume")
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(root, "resumed")
	script := "#!/bin/sh\n: > " + created + "\nexec sleep 30\n"
	if err = os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err = capture(t, func() error {
		return commandResumeLegacy(root, nil, "foreground", task, "go on", "", true, 61, resumeBinding{})
	}); err == nil || !strings.Contains(err.Error(), "--adopt") {
		t.Fatalf("resume err=%v", err)
	}
	if _, err = NotifyViaResume(root, s.Entry, s.Text, "go on", 61); err == nil || !strings.Contains(err.Error(), "--adopt") {
		t.Fatalf("notify err=%v", err)
	}
	if _, err = os.Stat(created); !os.IsNotExist(err) {
		t.Fatalf("a second executor started: %v", err)
	}
}
