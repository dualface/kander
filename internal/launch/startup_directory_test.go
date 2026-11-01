package launch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/terminal"
)

// startupDirectoryCases are directories a container cannot start in. Each one
// returns the root whose parent is that directory.
var startupDirectoryCases = []struct {
	name string
	root func(t *testing.T) string
	// reason is the catalog ID of the refusal; empty means the platform's stat
	// error, whose text is not asserted.
	reason string
}{
	{"missing", func(t *testing.T) string {
		return filepath.Join(t.TempDir(), "absent", "kanban")
	}, ""},
	{"deleted", func(t *testing.T) string {
		dir := filepath.Join(t.TempDir(), "project")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(dir); err != nil {
			t.Fatal(err)
		}
		return filepath.Join(dir, "kanban")
	}, ""},
	{"regular file", func(t *testing.T) string {
		file := filepath.Join(t.TempDir(), "project")
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		return filepath.Join(file, "kanban")
	}, "launch.startup_directory_is_not_a_directory"},
	{"line break", func(t *testing.T) string {
		dir := filepath.Join(t.TempDir(), "pro\nject")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Skipf("file system rejects a line break: %v", err)
		}
		return filepath.Join(dir, "kanban")
	}, "launch.agent_command_contains_a_control_character"},
}

func TestLaunchAgentRefusesAnUnusableStartupDirectoryBeforeTheContainer(t *testing.T) {
	resetLang(t)
	root, _, bin := setupBoard(t)
	log := filepath.Join(root, "tmux.log")
	tmux := filepath.Join(bin, "tmux")
	for _, launcher := range []string{"tmux", "tmux-session"} {
		for _, tc := range startupDirectoryCases {
			t.Run(launcher+"/"+tc.name, func(t *testing.T) {
				plan := LaunchPlan{Launcher: launcher, Target: terminal.Target{Program: tmux, Session: "test"}}
				invocation := process.ProcessInvocation{Argv: []string{"agent"}}
				root := tc.root(t)
				outcome, err := launchAgent(plan, root, "task", invocation, nil, nil, nil)
				want := launchError("launch.startup_directory_unusable", strconv.Quote(filepath.Dir(root)), "").Error()
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("err=%v", err)
				}
				if tc.reason != "" && !strings.Contains(err.Error(), launchError(tc.reason).Error()) {
					t.Fatalf("err=%v", err)
				}
				if outcome.Container != "" || outcome.Pane != "" {
					t.Fatalf("outcome=%+v", outcome)
				}
				if _, statErr := os.Stat(log); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("container created: %v", statErr)
				}
			})
		}
	}
}

func TestDirectLaunchersSkipTheStartupDirectoryCheck(t *testing.T) {
	resetLang(t)
	oldStart := startProcessFn
	t.Cleanup(func() { startProcessFn = oldStart })
	for _, launcher := range []string{"foreground", "console"} {
		t.Run(launcher, func(t *testing.T) {
			var gotCwd string
			startProcessFn = func(_ []string, _ map[string]string, cwd string, _ bool) (*startedProc, error) {
				gotCwd = cwd
				return nil, errors.New("spawn reached")
			}
			absent := filepath.Join(t.TempDir(), "absent")
			_, err := launchAgent(LaunchPlan{Launcher: launcher}, filepath.Join(absent, "kanban"), "task", process.ProcessInvocation{Argv: []string{"agent"}}, nil, nil, nil)
			if err == nil || !strings.Contains(err.Error(), "spawn reached") || gotCwd != absent {
				t.Fatalf("err=%v cwd=%q", err, gotCwd)
			}
		})
	}
}

// paneEntryScript is package state because the registry keeps the first
// backend of a name for the whole process.
var paneEntryScript struct {
	directories []string
	facts       []terminal.PaneFacts
	factsErr    error
	polls       int
	blocked     bool
}

// scriptedPane replays one answer per poll and repeats the last.
type scriptedPane struct{ terminal.Backend }

func (scriptedPane) Name() string { return "pane-entry-test" }

// A registered backend stays for the test binary, so ParseWindow reaches it later.
func (scriptedPane) Capabilities() terminal.Capabilities { return terminal.Capabilities{} }

func (scriptedPane) PaneDirectory(ctx context.Context, _ terminal.Conn, _ string) (string, error) {
	script := &paneEntryScript
	if script.blocked {
		<-ctx.Done()
		return "", ctx.Err()
	}
	script.polls++
	return script.directories[min(script.polls, len(script.directories))-1], nil
}

func (scriptedPane) PaneFacts(ctx context.Context, _ terminal.Conn, _ string) (terminal.PaneFacts, error) {
	script := &paneEntryScript
	if script.blocked {
		<-ctx.Done()
		return terminal.PaneFacts{}, ctx.Err()
	}
	return script.facts[min(script.polls, len(script.facts))-1], script.factsErr
}

func shortPaneEntryTimeout(t *testing.T, timeout time.Duration) {
	t.Helper()
	old := paneEntryTimeout
	paneEntryTimeout = timeout
	t.Cleanup(func() { paneEntryTimeout = old })
}

func TestWatchPaneEntry(t *testing.T) {
	resetLang(t)
	terminal.Register(scriptedPane{})
	shortPaneEntryTimeout(t, 300*time.Millisecond)
	dir := t.TempDir()
	other := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	shell := terminal.PaneFacts{Command: "sh", Dead: "0"}
	agent := terminal.PaneFacts{Command: "agent", Dead: "0"}
	for _, tc := range []struct {
		name        string
		directories []string
		facts       []terminal.PaneFacts
		factsErr    error
		want        paneEntry
		wantPolls   int
	}{
		{name: "entered", directories: []string{dir}, facts: []terminal.PaneFacts{shell}, want: paneEntered, wantPolls: 1},
		{name: "entered through a symbolic link", directories: []string{link}, facts: []terminal.PaneFacts{shell}, want: paneEntered, wantPolls: 1},
		{name: "shell reaches cd late", directories: []string{"", "/", dir}, facts: []terminal.PaneFacts{shell}, want: paneEntered, wantPolls: 3},
		{name: "agent seen elsewhere", directories: []string{other}, facts: []terminal.PaneFacts{agent}, want: paneEntered, wantPolls: 1},
		{name: "dead pane", directories: []string{""}, facts: []terminal.PaneFacts{shell, {Command: "sh", Dead: "1"}}, want: paneDead, wantPolls: 2},
		{name: "gone pane", directories: []string{""}, facts: []terminal.PaneFacts{shell, {Gone: true}}, want: paneGone, wantPolls: 2},
		{name: "open pane is not entry", directories: []string{other}, facts: []terminal.PaneFacts{shell}, want: paneUnconfirmed},
		{name: "probe errors stay unconfirmed", directories: []string{""}, facts: []terminal.PaneFacts{{}}, factsErr: errors.New("probe failed"), want: paneUnconfirmed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paneEntryScript.directories, paneEntryScript.facts, paneEntryScript.factsErr = tc.directories, tc.facts, tc.factsErr
			paneEntryScript.polls, paneEntryScript.blocked = 0, false
			got := watchPaneEntry(LaunchPlan{Launcher: "pane-entry-test", processName: "agent"}, "%1", dir)
			if got != tc.want || (tc.wantPolls != 0 && paneEntryScript.polls != tc.wantPolls) {
				t.Fatalf("entry=%d polls=%d", got, paneEntryScript.polls)
			}
		})
	}
}

// One deadline covers the probes too: a hung terminal must not spend the probe
// budget of each poll in turn.
func TestWatchPaneEntryStaysWithinOneBudget(t *testing.T) {
	resetLang(t)
	terminal.Register(scriptedPane{})
	shortPaneEntryTimeout(t, 100*time.Millisecond)
	paneEntryScript.blocked = true
	t.Cleanup(func() { paneEntryScript.blocked = false })
	started := time.Now()
	if got := watchPaneEntry(LaunchPlan{Launcher: "pane-entry-test"}, "%1", t.TempDir()); got != paneUnconfirmed {
		t.Fatalf("entry=%d", got)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("watch took %s", elapsed)
	}
}

// An unconfirmed pane may hold a running agent: a durable dispatch keeps it and
// stays unknown, and any other launch closes it like a failed start.
func TestUnconfirmedPaneKeepsADurableDelivery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX terminal fakes")
	}
	resetLang(t)
	root, _, bin := setupBoard(t)
	shortPaneEntryTimeout(t, 200*time.Millisecond)
	t.Setenv("KANBAN_TMUX_PANE_CWD", "/")
	t.Setenv("KANBAN_TMUX_CURRENT_COMMAND", "sh")
	kill := filepath.Join(root, "tmux.log.kill")
	plan := LaunchPlan{Launcher: "tmux", processName: "agent", Target: terminal.Target{Program: filepath.Join(bin, "tmux"), Session: "test"}}
	invocation := process.ProcessInvocation{Argv: []string{"agent"}}
	projectRoot := filepath.Join(t.TempDir(), "kanban")

	_, err := launchAgent(plan, projectRoot, "task", invocation, nil, nil, nil, true)
	if failure := asLaunchFailure(err); err == nil || !failure.DeliveryUnknown {
		t.Fatalf("durable err=%v", err)
	}
	if _, statErr := os.Stat(kill); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("durable launch closed the window: %v", statErr)
	}

	_, err = launchAgent(plan, projectRoot, "task", invocation, nil, nil, nil)
	if failure := asLaunchFailure(err); err == nil || failure.DeliveryUnknown {
		t.Fatalf("err=%v", err)
	}
	if _, statErr := os.Stat(kill); statErr != nil {
		t.Fatalf("window not closed: %v", statErr)
	}
}
