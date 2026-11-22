package launch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/terminal"
	"github.com/dualface/kander/internal/terminal/builtin"
	"github.com/dualface/kander/internal/terminal/terminaltest"
)

// A control character in any value of the typed line fails the launch before
// hc is asked for a terminal.
func TestWindowsHerdrCoreControlChars(t *testing.T) {
	resetLang(t)
	saved := runtimeWindows
	runtimeWindows = func() bool { return true }
	t.Cleanup(func() { runtimeWindows = saved })
	root := filepath.Join(t.TempDir(), "kanban")
	for _, where := range append([]string{"argv", "cmd variable"}, terminal.ForwardedEnv()...) {
		t.Run(where, func(t *testing.T) {
			for _, name := range terminal.ForwardedEnv() {
				t.Setenv(name, "")
			}
			const bad = "x\x1bcalc.exe #"
			invocation := process.ProcessInvocation{Argv: []string{"agent"}}
			switch where {
			case "argv":
				invocation.Argv = append(invocation.Argv, bad)
			case "cmd variable":
				invocation.ShellEnv = map[string]string{"KDR_0": bad}
			default:
				if err := os.Setenv(where, bad); err != nil {
					t.Fatal(err)
				}
			}
			fake := terminaltest.New(t)
			plan := LaunchPlan{Launcher: builtin.HerdrCore, Target: terminal.Target{Program: fake.Program, Windows: true}}
			if _, err := launchAgent(plan, root, "kb-task", invocation, nil, nil, nil); err == nil {
				t.Fatal("the control character was accepted")
			}
			if calls := fake.Calls(t); len(calls) != 0 {
				t.Fatalf("hc ran before the refusal: %q", calls)
			}
		})
	}
}
