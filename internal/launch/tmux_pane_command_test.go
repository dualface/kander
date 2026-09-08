package launch

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/dualface/kander/internal/process"
)

func TestLaunchAgentSendsShellSpecificCommandToTmux(t *testing.T) {
	resetLang(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	// Record argv without shell interpretation, including on Windows hosts.
	const fakeTmux = `package main
import ("encoding/json"; "fmt"; "os")
func main() {
	args := os.Args[1:]
	switch args[0] {
	case "new-session", "new-window":
		fmt.Print("@9\t%9\n")
	case "respawn-pane":
		data, _ := json.Marshal(args)
		if err := os.WriteFile(os.Getenv("FAKE_TMUX_RUN_LOG"), data, 0600); err != nil { panic(err) }
	case "set-option":
	default:
		os.Exit(1)
	}
}
`
	if err := os.WriteFile(source, []byte(fakeTmux), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "tmux")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", binary, source).CombinedOutput(); err != nil {
		t.Fatalf("build fake tmux: %v %s", err, out)
	}
	log := filepath.Join(dir, "run.json")
	t.Setenv("FAKE_TMUX_RUN_LOG", log)
	oldRuntimeWindows := runtimeWindows
	t.Cleanup(func() { runtimeWindows = oldRuntimeWindows })
	invocation := process.ProcessInvocation{
		Argv: []string{"/tools/agent name", "", "a'b", "$(touch forbidden); & | 中文"},
	}
	for _, launcher := range []string{"tmux", "tmux-session"} {
		for _, windows := range []bool{false, true} {
			name := launcher + "/posix"
			want := "exec " + posixJoin(invocation.Argv)
			if windows {
				name = launcher + "/windows"
				want = powershellJoin(invocation.Argv, invocation.ShellEnv)
			}
			t.Run(name, func(t *testing.T) {
				runtimeWindows = func() bool { return windows }
				plan := LaunchPlan{Launcher: launcher, Tmux: binary, Session: "test"}
				if _, err := launchAgent(plan, filepath.Join(dir, "kanban"), "task", invocation, nil, nil, nil); err != nil {
					t.Fatal(err)
				}
				var got []string
				if err := json.Unmarshal([]byte(readRunLog(t, log)), &got); err != nil {
					t.Fatal(err)
				}
				wantArgs := []string{"respawn-pane", "-k", "-t", "%9", want}
				if !reflect.DeepEqual(got, wantArgs) {
					t.Fatalf("respawn argv\n got %q\nwant %q", got, wantArgs)
				}
			})
		}
	}
}
