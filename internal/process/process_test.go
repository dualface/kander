package process

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func withWindows(t *testing.T) {
	t.Helper()
	orig := isWindows
	isWindows = func() bool { return true }
	t.Cleanup(func() { isWindows = orig })
}

func TestPosixProgramUsesNativeArgv(t *testing.T) {
	program := AgentProgram{Path: "/usr/bin/codex"}
	invocation, err := NewProcessInvocation(program, []string{"--model", "model with spaces"}, map[string]string{"PATH": "/usr/bin"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/usr/bin/codex", "--model", "model with spaces"}
	if strings.Join(invocation.Argv, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("argv = %#v, want %#v", invocation.Argv, want)
	}
	if invocation.Env["PATH"] != "/usr/bin" {
		t.Fatalf("PATH = %q", invocation.Env["PATH"])
	}
}

func TestPosixResolveUsesLookPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX LookPath 解析在 Windows 上 skip")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "cursor-agent")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	got := ResolveAgentProgram("cursor-agent")
	if got == nil {
		t.Fatal("expected program")
	}
	if got.Batch {
		t.Fatal("posix program must not be batch")
	}
	abs, err := filepath.Abs(bin)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != abs && got.Path != bin {
		resolved, _ := filepath.EvalSymlinks(abs)
		if got.Path != resolved {
			t.Fatalf("path = %q, want %q", got.Path, abs)
		}
	}
	if ResolveAgentProgram("missing-kander-agent") != nil {
		t.Fatal("missing program must be nil")
	}
}

func TestWindowsLookupSkipsEmptyExeAndAcceptsBatch(t *testing.T) {
	withWindows(t)
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	if err := os.Mkdir(first, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(second, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first, "claude.exe"), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	batch := filepath.Join(second, "claude.cmd")
	if err := os.WriteFile(batch, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", first+";"+second)
	got := ResolveAgentProgram("claude")
	if got == nil {
		t.Fatal("expected batch program")
	}
	if !got.Batch || got.Path != batch {
		t.Fatalf("got %#v, want batch %q", got, batch)
	}
}

func TestWindowsLookupPrefersLaterNativeExe(t *testing.T) {
	withWindows(t)
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	if err := os.Mkdir(first, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(second, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first, "codex.cmd"), []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	native := filepath.Join(second, "codex.exe")
	if err := os.WriteFile(native, []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", first+";"+second)
	got := ResolveAgentProgram("codex")
	if got == nil {
		t.Fatal("expected native program")
	}
	if got.Batch || got.Path != native {
		t.Fatalf("got %#v, want native %q", got, native)
	}
}

func TestBatchCommandContainsOnlyGeneratedReferences(t *testing.T) {
	withWindows(t)
	program := AgentProgram{Path: `C:\Agents & Tools\codex.cmd`, Batch: true}
	argument := `model %NAME%! & echo "unexpected"`
	environment := map[string]string{"COMSPEC": `C:\Windows\System32\cmd.exe`}
	invocation, err := NewProcessInvocation(program, []string{"--model", argument}, environment)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := []string{`C:\Windows\System32\cmd.exe`, "/d", "/s", "/v:off", "/c"}
	if len(invocation.Argv) != 6 {
		t.Fatalf("argv = %#v", invocation.Argv)
	}
	for i, part := range wantPrefix {
		if invocation.Argv[i] != part {
			t.Fatalf("argv[%d] = %q, want %q", i, invocation.Argv[i], part)
		}
	}
	command := invocation.Argv[5]
	if strings.Contains(command, program.Path) {
		t.Fatalf("command contains program path: %q", command)
	}
	if strings.Contains(command, argument) {
		t.Fatalf("command contains raw argument: %q", command)
	}
	percentPairs := strings.Count(command, "%") / 2
	if percentPairs != 3 {
		t.Fatalf("%% pairs = %d in %q", percentPairs, command)
	}
	var encoded []string
	for key, value := range invocation.Env {
		if strings.HasPrefix(key, cmdEnvPrefix) {
			encoded = append(encoded, value)
		}
	}
	if len(encoded) != 3 {
		t.Fatalf("encoded vars = %#v", encoded)
	}
	found := false
	want := `"model %NAME%! & echo ""unexpected"""`
	for _, value := range encoded {
		if value == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing encoded argument in %#v", encoded)
	}
}

func TestBatchQuoteHandlesEmptyQuotesAndTrailingBackslash(t *testing.T) {
	got, err := quoteWindowsBatchArgument("")
	if err != nil || got != `""` {
		t.Fatalf("empty: %q %v", got, err)
	}
	got, err = quoteWindowsBatchArgument(`a"b`)
	if err != nil || got != `"a""b"` {
		t.Fatalf("quote: %q %v", got, err)
	}
	got, err = quoteWindowsBatchArgument("hello world\\")
	if err != nil || got != `"hello world\\"` {
		t.Fatalf("backslash: %q %v", got, err)
	}
}

func TestBatchIgnoresRelativeComspec(t *testing.T) {
	withWindows(t)
	program := AgentProgram{Path: `C:\Agents\codex.cmd`, Batch: true}
	environment := map[string]string{"COMSPEC": "cmd.exe", "SystemRoot": `D:\Windows`}
	invocation, err := NewProcessInvocation(program, nil, environment)
	if err != nil {
		t.Fatal(err)
	}
	if invocation.Argv[0] != `D:\Windows\System32\cmd.exe` {
		t.Fatalf("interpreter = %q", invocation.Argv[0])
	}
}

func TestTaskFileContainsBodyAndBestEffortDeleteInstruction(t *testing.T) {
	path, err := CreateTaskFile("line one\nline two", "kander-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.HasPrefix(text, "line one\nline two\n\n") {
		t.Fatalf("prefix mismatch: %q", text)
	}
	if !strings.Contains(text, path) {
		t.Fatalf("missing path in %q", text)
	}
	if !strings.Contains(text, "删除失败或文件遗留不影响任务结果") {
		t.Fatalf("missing delete instruction in %q", text)
	}
}

func TestWriteTaskFileUsesSamePayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task.md")
	if err := WriteTaskFile(filepath.Dir(path), path, "hello"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.HasPrefix(text, "hello\n\n") {
		t.Fatalf("payload = %q", text)
	}
	if !strings.Contains(text, path) {
		t.Fatalf("missing path in %q", text)
	}
}

func TestTaskFilePointerIsOneShortInstruction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task.md")
	instruction := TaskFileInstruction("Execute this task.", path)
	if strings.Count(instruction, ";") != 2 {
		t.Fatalf("semicolons: %q", instruction)
	}
	if strings.Contains(instruction, "\n") {
		t.Fatalf("contains newline: %q", instruction)
	}
	if !strings.Contains(instruction, path) {
		t.Fatalf("missing path: %q", instruction)
	}
	if !strings.HasSuffix(instruction, "exactly.") {
		t.Fatalf("suffix: %q", instruction)
	}
}

func TestBatchQuoteRejectsNUL(t *testing.T) {
	_, err := quoteWindowsBatchArgument("a\x00b")
	if err != ErrNUL {
		t.Fatalf("err = %v, want ErrNUL", err)
	}
}

// Windows environment variable names are case-insensitive, but containers spell
// them differently: a herdr pane gives ComSpec and SYSTEMROOT. Looking the map
// up under one fixed spelling misses both, and batch agents then fail with
// "could not resolve an absolute Windows cmd.exe path".
func TestWindowsCommandInterpreterIgnoresEnvNameCase(t *testing.T) {
	for _, environment := range []map[string]string{
		{"ComSpec": `C:\WINDOWS\system32\cmd.exe`},
		{"comspec": `C:\WINDOWS\system32\cmd.exe`},
		{"SYSTEMROOT": `C:\WINDOWS`},
		{"systemroot": `C:\WINDOWS`},
	} {
		got, err := windowsCommandInterpreter(environment)
		if err != nil {
			t.Fatalf("%v: %v", environment, err)
		}
		if !strings.EqualFold(windowsPathName(got), "cmd.exe") {
			t.Fatalf("%v: got %q", environment, got)
		}
	}
	if _, err := windowsCommandInterpreter(map[string]string{"PATH": "x"}); err == nil {
		t.Fatal("expected ErrNoCmd without ComSpec or SystemRoot")
	}
}

// A batch agent's argv hides behind %VAR% references. A terminal container
// (herdr pane) receives a single shell command rather than argv+env, so
// ShellEnv is what lets those variables be assigned back inside the container.
func TestBatchInvocationExposesShellEnv(t *testing.T) {
	withWindows(t)
	program := AgentProgram{Path: `C:\tools\codex.cmd`, Batch: true}
	invocation, err := NewProcessInvocation(program, []string{"exec", "任务 & 提示"}, map[string]string{
		"ComSpec": `C:\WINDOWS\system32\cmd.exe`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(invocation.ShellEnv) != 3 {
		t.Fatalf("ShellEnv = %#v", invocation.ShellEnv)
	}
	// Every reference in the /c text must have a matching ShellEnv entry that agrees with Env.
	references := invocation.Argv[len(invocation.Argv)-1]
	for name, value := range invocation.ShellEnv {
		if !strings.Contains(references, "%"+name+"%") {
			t.Fatalf("%s is not referenced by %q", name, references)
		}
		if invocation.Env[name] != value {
			t.Fatalf("%s: Env=%q ShellEnv=%q", name, invocation.Env[name], value)
		}
	}
	if got := strings.Count(references, "%"); got != 2*len(invocation.ShellEnv) {
		t.Fatalf("references = %q", references)
	}
	if !strings.Contains(invocation.ShellEnv[shellEnvName(t, invocation, 0)], "codex.cmd") {
		t.Fatalf("program is not the first reference: %#v", invocation.ShellEnv)
	}
}

func shellEnvName(t *testing.T, invocation ProcessInvocation, index int) string {
	t.Helper()
	suffix := "_" + strconv.Itoa(index)
	for name := range invocation.ShellEnv {
		if strings.HasSuffix(name, suffix) {
			return name
		}
	}
	t.Fatalf("no ShellEnv entry with suffix %q", suffix)
	return ""
}

// On Windows an invocation bound for a terminal container always takes cmd.exe's
// %VAR% form, batch or not: when PowerShell hands arguments to a native exe it
// rebuilds the command line and swallows embedded double quotes.
// Directly spawned invocations must keep native argv, or the console launcher
// would report cmd.exe's PID instead of the agent's.
func TestShellInvocationUsesVariableFormForNonBatch(t *testing.T) {
	withWindows(t)
	program := AgentProgram{Path: `C:\tools\codex.exe`}
	arguments := []string{"--config", `model_reasoning_effort="medium"`}
	environment := map[string]string{"ComSpec": `C:\WINDOWS\system32\cmd.exe`}

	shell, err := NewShellInvocation(program, arguments, environment)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(windowsPathName(shell.Argv[0]), "cmd.exe") {
		t.Fatalf("argv[0] = %q", shell.Argv[0])
	}
	references := shell.Argv[len(shell.Argv)-1]
	if strings.Contains(references, "codex") || strings.Contains(references, "medium") {
		t.Fatalf("/c 文本必须只含变量引用: %q", references)
	}
	if len(shell.ShellEnv) != 3 {
		t.Fatalf("ShellEnv = %#v", shell.ShellEnv)
	}
	for _, want := range []string{"codex.exe", `model_reasoning_effort=`} {
		found := false
		for _, value := range shell.ShellEnv {
			if strings.Contains(value, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("%q 没有出现在 ShellEnv 里: %#v", want, shell.ShellEnv)
		}
	}

	// The same input must still be native argv when it is spawned directly.
	direct, err := NewProcessInvocation(program, arguments, environment)
	if err != nil {
		t.Fatal(err)
	}
	if direct.ShellEnv != nil {
		t.Fatalf("直接 spawn 不该产出 ShellEnv: %#v", direct.ShellEnv)
	}
	if direct.Argv[0] != program.Path || direct.Argv[2] != arguments[1] {
		t.Fatalf("直接 spawn 的 argv 被改写: %#v", direct.Argv)
	}
}
