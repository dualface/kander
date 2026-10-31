package launch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/terminal"
)

// startupDirectoryCases are directories a container cannot start in. Each one
// returns the root whose parent is that directory.
var startupDirectoryCases = []struct {
	name string
	root func(t *testing.T) string
	// reason is the catalog ID of the refusal; empty means the stat error.
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
				outcome, err := launchAgent(plan, tc.root(t), "task", invocation, nil, nil, nil)
				want := "no such file"
				if tc.reason != "" {
					want = launchError(tc.reason).Error()
				}
				if err == nil || !strings.Contains(err.Error(), want) {
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
	replies []terminal.PaneFacts
	err     error
	polls   int
}

// scriptedPaneFacts replays one PaneFacts answer per poll and repeats the last.
type scriptedPaneFacts struct{ terminal.Backend }

func (scriptedPaneFacts) Name() string { return "pane-entry-test" }

func (scriptedPaneFacts) PaneFacts(context.Context, terminal.Conn, string) (terminal.PaneFacts, error) {
	script := &paneEntryScript
	reply := script.replies[min(script.polls, len(script.replies)-1)]
	script.polls++
	return reply, script.err
}

func TestPaneEndedWatch(t *testing.T) {
	resetLang(t)
	terminal.Register(scriptedPaneFacts{})
	freezeClock(t)
	shell := terminal.PaneFacts{Command: "sh", Dead: "0"}
	agent := terminal.PaneFacts{Command: "agent", Dead: "0"}
	for _, tc := range []struct {
		name      string
		replies   []terminal.PaneFacts
		err       error
		process   string
		wantEnded bool
		wantGone  bool
		wantPolls int
	}{
		{name: "agent on the first poll", replies: []terminal.PaneFacts{agent}, process: "agent", wantPolls: 1},
		{name: "shell then agent", replies: []terminal.PaneFacts{shell, agent}, process: "agent", wantPolls: 2},
		{name: "dead pane", replies: []terminal.PaneFacts{shell, {Command: "sh", Dead: "1"}}, process: "agent", wantEnded: true, wantPolls: 2},
		{name: "gone pane", replies: []terminal.PaneFacts{shell, {Gone: true}}, process: "agent", wantEnded: true, wantGone: true, wantPolls: 2},
		{name: "open pane until the bound", replies: []terminal.PaneFacts{shell}, process: "agent", wantPolls: paneEntryPolls},
		{name: "no process name", replies: []terminal.PaneFacts{agent}, wantPolls: paneEntryPolls},
		{name: "probe errors until the bound", replies: []terminal.PaneFacts{{}}, err: errors.New("probe failed"), process: "agent", wantPolls: paneEntryPolls},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paneEntryScript.replies, paneEntryScript.err, paneEntryScript.polls = tc.replies, tc.err, 0
			ended, gone := paneEnded(LaunchPlan{Launcher: "pane-entry-test", processName: tc.process}, "%1")
			if ended != tc.wantEnded || gone != tc.wantGone || paneEntryScript.polls != tc.wantPolls {
				t.Fatalf("ended=%t gone=%t polls=%d", ended, gone, paneEntryScript.polls)
			}
		})
	}
}
