package launch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/process"
)

// fakeHerdrLaunchSource answers only the three subcommands launchAgent uses and
// writes whatever `pane run` receives verbatim to FAKE_HERDR_RUN_LOG. It is
// compiled to a binary rather than written as a /bin/sh script so this path runs
// on Windows too.
const fakeHerdrLaunchSource = `package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	args := os.Args[1:]
	if len(args) >= 2 && args[0] == "tab" && args[1] == "create" {
		payload, _ := json.Marshal(map[string]any{
			"result": map[string]any{
				"tab":       map[string]any{"tab_id": "w1:t7"},
				"root_pane": map[string]any{"pane_id": "w1:p7"},
			},
		})
		fmt.Println(string(payload))
		return
	}
	if len(args) >= 2 && args[0] == "pane" && args[1] == "wait-output" {
		return
	}
	if len(args) >= 4 && args[0] == "pane" && args[1] == "run" {
		os.WriteFile(os.Getenv("FAKE_HERDR_RUN_LOG"), []byte(args[3]), 0o644)
		return
	}
	fmt.Fprintln(os.Stderr, "unexpected herdr args")
	os.Exit(1)
}
`

func buildFakeHerdrLauncher(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	if err := os.WriteFile(source, []byte(fakeHerdrLaunchSource), 0o644); err != nil {
		t.Fatal(err)
	}
	name := "herdr"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(dir, name)
	build := exec.Command("go", "build", "-o", binary, source)
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake herdr: %v %s", err, out)
	}
	return binary
}

// launchAgent must hand the exact line paneCommand rendered to herdr pane run.
// A Windows pane runs PowerShell, where POSIX-style quoting is printed back
// verbatim and the agent never starts.
func TestLaunchAgentSendsShellSpecificCommandToHerdr(t *testing.T) {
	resetLang(t)
	binary := buildFakeHerdrLauncher(t)
	log := filepath.Join(t.TempDir(), "run.txt")
	t.Setenv("FAKE_HERDR_RUN_LOG", log)
	t.Cleanup(func() { runtimeWindows = func() bool { return isWindowsGOOS() } })

	plan := LaunchPlan{Launcher: "herdr", HerdrBin: binary, HerdrWorkspace: "w1"}
	invocation := process.ProcessInvocation{
		Argv:     []string{`C:\tools\cmd.exe`, "/c", "%KDR_0%"},
		ShellEnv: map[string]string{"KDR_0": `"C:\tools\codex.cmd"`},
	}

	runtimeWindows = func() bool { return true }
	outcome, err := launchAgent(plan, filepath.Join(t.TempDir(), "kanban"), "tab-label", invocation, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Tab != "w1:t7" || outcome.Pane != "w1:p7" {
		t.Fatalf("outcome=%+v", outcome)
	}
	got := readRunLog(t, log)
	want := `$env:KDR_0='"C:\tools\codex.cmd"'; & 'C:\tools\cmd.exe' '/c' '%KDR_0%'`
	if got != want {
		t.Fatalf("windows command\n got %q\nwant %q", got, want)
	}

	runtimeWindows = func() bool { return false }
	if _, err := launchAgent(plan, filepath.Join(t.TempDir(), "kanban"), "tab-label", invocation, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := readRunLog(t, log); strings.HasPrefix(got, "$env:") || strings.Contains(got, "& '") {
		t.Fatalf("posix command must not use PowerShell syntax: %q", got)
	}
}

func readRunLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// writeResolvableAgent writes a fake agent that this platform's ResolveAgentProgram can actually find.
func writeResolvableAgent(t *testing.T, dir, name string) {
	t.Helper()
	body := "#!/bin/sh\nexit 0\n"
	if runtime.GOOS == "windows" {
		name += ".cmd"
		body = "@echo off\r\n"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// start through herdr must build the invocation in shell form: on Windows only
// cmd.exe's %VAR% form restores argv verbatim, and handing argv straight to
// PowerShell loses embedded double quotes.
// This case watches whether the real call sites use launchInvocation, not
// launchInvocation itself.
func TestCommandStartUsesShellInvocationForHerdr(t *testing.T) {
	root, _, fakeBin := setupBoard(t)
	writeResolvableAgent(t, fakeBin, "codex")
	herdr := buildFakeHerdrLauncher(t)
	t.Setenv("FAKE_HERDR_RUN_LOG", filepath.Join(t.TempDir(), "run.txt"))
	t.Setenv("PATH", filepath.Dir(herdr)+string(os.PathListSeparator)+fakeBin)
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_WORKSPACE_ID", "w1")

	t.Cleanup(func() {
		newInvocation = process.NewProcessInvocation
		newShellInvocation = process.NewShellInvocation
	})
	var shell, direct int
	newShellInvocation = func(p process.AgentProgram, a []string, e map[string]string) (process.ProcessInvocation, error) {
		shell++
		return process.NewShellInvocation(p, a, e)
	}
	newInvocation = func(p process.AgentProgram, a []string, e map[string]string) (process.ProcessInvocation, error) {
		direct++
		return process.NewProcessInvocation(p, a, e)
	}

	taskID, _ := makeTodo(t, root, "shell-invocation")
	if _, _, err := capture(t, func() error { return commandStart(root, "", "herdr", taskID) }); err != nil {
		t.Fatalf("start: %v", err)
	}
	if shell != 1 || direct != 0 {
		t.Fatalf("shell=%d direct=%d", shell, direct)
	}
}

// launchInvocation is the only place allowed to call newInvocation directly: the
// three agent launch points (start / resume / notify recovery) must route through
// it so the form follows the launcher, or a Windows herdr pane goes back to
// receiving native argv that PowerShell cannot reassemble.
// Watching that invariant with the AST is cheaper than writing one integration
// case per call site.
func TestOnlyLaunchInvocationCallsNewInvocation(t *testing.T) {
	files := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(files, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			ast.Inspect(fn, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				var name string
				switch fun := call.Fun.(type) {
				case *ast.Ident:
					name = fun.Name
				case *ast.SelectorExpr:
					// Bypassing the package variable and calling process.NewProcessInvocation directly breaks the invariant too.
					if qualifier, ok := fun.X.(*ast.Ident); ok && qualifier.Name == "process" {
						name = fun.Sel.Name
					}
				}
				if name != "newInvocation" && name != "NewProcessInvocation" {
					return true
				}
				found++
				// cursorCreateChat spawns create-chat in place and reads its output; it
				// never goes through a terminal container, so native argv is correct.
				if fn.Name.Name != "launchInvocation" && fn.Name.Name != "cursorCreateChat" {
					t.Errorf("%s: %s 直接调用了 newInvocation, 应改用 launchInvocation",
						files.Position(call.Pos()), fn.Name.Name)
				}
				return true
			})
		}
	}
	if found == 0 {
		t.Fatal("没有找到 newInvocation 的调用, 断言失效")
	}
}
