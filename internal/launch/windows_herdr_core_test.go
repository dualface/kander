package launch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/process"
	"github.com/dualface/kander/v2/internal/terminal"
	"github.com/dualface/kander/v2/internal/terminal/builtin"
	"github.com/dualface/kander/v2/internal/terminal/terminaltest"
)

// A control character in any value of the typed line fails the launch before
// hc is asked for a terminal.
func TestWindowsHerdrCoreControlChars(t *testing.T) {
	resetLang(t)
	saved := runtimeWindows
	runtimeWindows = func() bool { return true }
	t.Cleanup(func() { runtimeWindows = saved })
	savedDirs := terminal.DefinitionDirs
	terminal.DefinitionDirs = func() []terminal.DefinitionDir { return nil }
	terminal.ReloadDefinitions()
	t.Cleanup(func() {
		terminal.DefinitionDirs = savedDirs
		terminal.ReloadDefinitions()
	})
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

// A start refused by a POSIX-only override never advises the launcher it refuses.
func TestWindowsHerdrCoreOverrideLaunchMessages(t *testing.T) {
	resetLang(t)
	saved := runtimeWindows
	runtimeWindows = func() bool { return true }
	t.Cleanup(func() { runtimeWindows = saved })
	data, err := os.ReadFile(filepath.Join("..", "terminal", "builtin", "definitions", "herdr-core.json"))
	if err != nil {
		t.Fatal(err)
	}
	share := t.TempDir()
	dir := filepath.Join(share, terminal.DefinitionsDirName)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := strings.Replace(string(data), `"platform": "any"`, `"platform": "posix"`, 1)
	if err := os.WriteFile(filepath.Join(dir, "herdr-core.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	savedDirs := terminal.DefinitionDirs
	terminal.DefinitionDirs = func() []terminal.DefinitionDir {
		return []terminal.DefinitionDir{{Source: terminal.SourceGlobal, Root: share, Path: dir}}
	}
	terminal.ReloadDefinitions()
	t.Cleanup(func() {
		terminal.DefinitionDirs = savedDirs
		terminal.ReloadDefinitions()
	})
	t.Setenv("HC_TERMINAL_ID", "t3")
	t.Setenv("HERDR_ENV", "")
	for launcher, want := range map[string]error{
		terminal.Auto:     launchError("launch.auto_cannot_be_resolved_only_herdr_on_windows"),
		builtin.HerdrCore: launchError("terminal.platform_unsupported_posix", builtin.HerdrCore),
	} {
		if _, err := prepareLaunch(launcher, t.TempDir(), "start"); err == nil || err.Error() != want.Error() {
			t.Errorf("%s: err=%v, want %v", launcher, err, want)
		}
	}
}
