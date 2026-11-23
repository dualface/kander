package launch

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/process"
	"github.com/dualface/kander/internal/terminal"
	"github.com/dualface/kander/internal/terminal/builtin"
)

const (
	lineAgentOutEnv  = "KANDER_LINE_AGENT_OUT"
	lineAgentExitEnv = "KANDER_LINE_AGENT_EXIT"
	lineAgentArg     = "line-agent"
)

type lineAgentReport struct {
	Args []string
	Env  map[string]*string
}

// serveLineAgent turns the test binary into the agent of a PowerShell line
// test: it records its arguments and the forwarded variables and exits with
// the requested code.
func serveLineAgent() {
	out := os.Getenv(lineAgentOutEnv)
	if out == "" || len(os.Args) < 2 || os.Args[1] != lineAgentArg {
		return
	}
	report := lineAgentReport{Args: os.Args[2:], Env: map[string]*string{}}
	for _, name := range terminal.ForwardedEnv() {
		if value, ok := os.LookupEnv(name); ok {
			report.Env[name] = &value
		}
	}
	data, err := json.Marshal(report)
	if err != nil {
		os.Exit(90)
	}
	if err := os.WriteFile(out, data, 0o600); err != nil {
		os.Exit(91)
	}
	code, err := strconv.Atoi(os.Getenv(lineAgentExitEnv))
	if err != nil {
		os.Exit(92)
	}
	os.Exit(code)
}

// linePowerShell is the container shell: powershell.exe on Windows, where its
// absence fails, and pwsh elsewhere when it is installed.
func linePowerShell(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		path, err := exec.LookPath("powershell.exe")
		if err != nil {
			t.Fatalf("powershell.exe is the herdr-core container shell: %v", err)
		}
		return path
	}
	path, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("pwsh is not installed")
	}
	return path
}

// typedLine is the line the herdr-core definition sends for invocation, and
// the command part of it.
func typedLine(t *testing.T, invocation process.ProcessInvocation, env map[string]string) (line, command string) {
	t.Helper()
	saved := runtimeWindows
	runtimeWindows = func() bool { return true }
	defer func() { runtimeWindows = saved }()
	command, err := paneCommand(invocation)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := builtin.DefinitionBackend("herdr-core", builtin.HerdrCore, func(name string) string { return env[name] })
	if err != nil {
		t.Fatal(err)
	}
	conn := terminal.Conn{Program: "hc", Run: func(_ context.Context, _ string, args []string) (probe.Result, error) {
		line = args[len(args)-1]
		return probe.Result{Stdout: "{}"}, nil
	}}
	if err := backend.RunCommand(conn, "t8", "", command, false); err != nil {
		t.Fatal(err)
	}
	return line, command
}

type openShell struct {
	done chan error
}

// startShell types line into a shell whose stdin stays open, as in a terminal.
// Windows PowerShell 5.1 decodes a redirected stdin by the console code page,
// so the line travels as ASCII and is restored, checked and then run.
func startShell(t *testing.T, shell, line string) *openShell {
	t.Helper()
	received := filepath.Join(t.TempDir(), "received")
	cmd := exec.Command(shell, "-NoLogo", "-NoProfile", "-Command", "-")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	s := &openShell{done: make(chan error, 1)}
	go func() { s.done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = stdin.Close()
	})
	encode := func(text string) string {
		return "[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('" + base64.StdEncoding.EncodeToString([]byte(text)) + "'))"
	}
	typed := fmt.Sprintf("$kdrLine=%s; [IO.File]::WriteAllText(%s, $kdrLine); . ([scriptblock]::Create($kdrLine))\n", encode(line), encode(received))
	if _, err := io.WriteString(stdin, typed); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if data, err := os.ReadFile(received); err == nil && string(data) == line {
			return s
		}
		if time.Now().After(deadline) {
			data, err := os.ReadFile(received)
			t.Fatalf("the shell received %q (%v), want %q", data, err, line)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (s *openShell) exitCode(t *testing.T) int {
	t.Helper()
	select {
	case err := <-s.done:
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		if err != nil {
			t.Fatal(err)
		}
		return 0
	case <-time.After(30 * time.Second):
		t.Fatal("the shell stayed open after the agent ended")
		return -1
	}
}

func lineInvocation(t *testing.T, program process.AgentProgram, args ...string) process.ProcessInvocation {
	t.Helper()
	invocation, err := process.NewShellInvocation(program, append([]string{lineAgentArg}, args...), nil)
	if err != nil {
		t.Fatal(err)
	}
	return invocation
}

func readLineReport(t *testing.T, out string) lineAgentReport {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		var report lineAgentReport
		data, err := os.ReadFile(out)
		if err == nil && json.Unmarshal(data, &report) == nil {
			return report
		}
		if time.Now().After(deadline) {
			t.Fatalf("the agent wrote no report: %q %v", data, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestPowerShellLine(t *testing.T) {
	shell := linePowerShell(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	agent := process.AgentProgram{Path: self}
	setup := func(t *testing.T, code int) string {
		out := filepath.Join(t.TempDir(), "report.json")
		t.Setenv(lineAgentOutEnv, out)
		t.Setenv(lineAgentExitEnv, strconv.Itoa(code))
		for _, name := range terminal.ForwardedEnv() {
			t.Setenv(name, "")
		}
		return out
	}

	t.Run("values arrive verbatim", func(t *testing.T) {
		out := setup(t, 0)
		args := []string{
			"with space", "中文 路径", "O'Brien", "a\u2018b", "a\u2019b", "a\u201ab", "a\u201bb", "",
			`say "hi"`, "100% %PATH%", "!PATH!", "a&b", "a|b", "a^b", `C:\dir with space\`, `back\"quote`, "<in>", "(p)", "$env:PATH", "`tick",
		}
		env := map[string]string{
			"KANBAN_DIR":         "C:\\看板 dir\\O'Brien \u2018\u2019\u201a\u201b",
			"KANDER_LANG":        "cn",
			"KANDER_CLIENT_HOME": "C:\\work\\client O\u2019Brien",
		}
		// A stale value of the terminal service must not survive.
		t.Setenv("KANDER_CONFIG", "stale")
		line, _ := typedLine(t, lineInvocation(t, agent, args...), env)
		if code := startShell(t, shell, line).exitCode(t); code != 0 {
			t.Fatalf("exit=%d", code)
		}
		report := readLineReport(t, out)
		if !slices.Equal(report.Args, args) {
			t.Fatalf("args=%q\nwant %q", report.Args, args)
		}
		for name, want := range env {
			if got := report.Env[name]; got == nil || *got != want {
				t.Fatalf("%s=%v, want %q", name, got, want)
			}
		}
		if got := report.Env["KANDER_CONFIG"]; got != nil {
			t.Fatalf("an unset variable kept %q", *got)
		}
	})

	t.Run("a typographic quote runs nothing", func(t *testing.T) {
		out := setup(t, 0)
		marker := filepath.Join(t.TempDir(), "injected")
		payload := "x\u2019; [IO.File]::WriteAllText('" + marker + "','x'); \u2019"
		line, _ := typedLine(t, lineInvocation(t, agent, payload), map[string]string{"KANBAN_DIR": payload})
		startShell(t, shell, line).exitCode(t)
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("the value ran as a command: %v", err)
		}
		if report := readLineReport(t, out); !slices.Equal(report.Args, []string{payload}) {
			t.Fatalf("args=%q", report.Args)
		}
	})

	for _, code := range []int{0, 7} {
		t.Run("exit "+strconv.Itoa(code), func(t *testing.T) {
			setup(t, code)
			line, _ := typedLine(t, lineInvocation(t, agent), nil)
			if got := startShell(t, shell, line).exitCode(t); got != code {
				t.Fatalf("exit=%d, want %d", got, code)
			}
		})
	}

	t.Run("control without finally stays open", func(t *testing.T) {
		out := setup(t, 7)
		_, command := typedLine(t, lineInvocation(t, agent), nil)
		s := startShell(t, shell, command)
		readLineReport(t, out)
		select {
		case err := <-s.done:
			t.Fatalf("the shell ended without the finally block: %v", err)
		case <-time.After(2 * time.Second):
		}
	})

	missing := filepath.Join(t.TempDir(), "no-such-program")
	for name, invocation := range map[string]process.ProcessInvocation{
		"agent cannot start":       lineInvocation(t, process.AgentProgram{Path: missing}),
		"interpreter cannot start": {Argv: []string{missing}},
	} {
		t.Run(name, func(t *testing.T) {
			setup(t, 0)
			line, _ := typedLine(t, invocation, nil)
			if code := startShell(t, shell, line).exitCode(t); code == 0 {
				t.Fatal("a failed start exited 0")
			}
		})
	}

	t.Run("a program path with operators and odd quotes", func(t *testing.T) {
		out := setup(t, 0)
		dir := filepath.Join(t.TempDir(), "agent & (x) ^ !A! %B% O'Brien \u2019 中文")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		program := filepath.Join(dir, filepath.Base(self))
		data, err := os.ReadFile(self)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(program, data, 0o755); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(t.TempDir(), "injected")
		args := []string{
			`one " quote`, `x"&echo.>` + marker + `&"`, `q"|echo.>` + marker, `z^&echo.>` + marker,
			`"`, `%COMSPEC%&echo.>` + marker, `!COMSPEC!&echo.>` + marker, `tail\`, "last",
		}
		line, _ := typedLine(t, lineInvocation(t, process.AgentProgram{Path: program}, args...), nil)
		if code := startShell(t, shell, line).exitCode(t); code != 0 {
			t.Fatalf("exit=%d", code)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("a value ran as a command: %v", err)
		}
		if report := readLineReport(t, out); !slices.Equal(report.Args, args) {
			t.Fatalf("args=%q\nwant %q", report.Args, args)
		}
	})

	// Only Windows has cmd.exe, the second parser of the line; elsewhere there
	// is nothing to test, and a skip here would hide nothing on Windows.
	if runtime.GOOS == "windows" {
		t.Run("cmd script runs nothing", func(t *testing.T) {
			out := setup(t, 0)
			dir := t.TempDir()
			script := filepath.Join(dir, "agent shim.cmd")
			t.Setenv("KANDER_LINE_AGENT_PATH", self)
			if err := os.WriteFile(script, []byte("@\"%KANDER_LINE_AGENT_PATH%\" %*\r\n@exit /b %ERRORLEVEL%\r\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(dir, "injected")
			args := []string{
				`x"&echo.>` + marker + `&"`, `y|echo.>` + marker, `z^&echo.>` + marker,
				`%COMSPEC%&echo.>` + marker, `!COMSPEC!&echo.>` + marker, `q"|echo.>` + marker,
			}
			line, _ := typedLine(t, lineInvocation(t, process.AgentProgram{Path: script, Batch: true}, args...), nil)
			if code := startShell(t, shell, line).exitCode(t); code != 0 {
				t.Fatalf("exit=%d", code)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("a value ran as a command: %v", err)
			}
			readLineReport(t, out)
		})
	}
}
